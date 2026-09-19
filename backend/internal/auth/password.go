package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	passwordMemory      = 64 * 1024
	passwordIterations  = 3
	passwordParallelism = 2
	passwordSaltLength  = 16
	passwordKeyLength   = 32
)

var ErrInvalidPasswordHash = errors.New("invalid password hash")

func hashPassword(password string) (string, error) {
	salt := make([]byte, passwordSaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, passwordIterations, passwordMemory, passwordParallelism, passwordKeyLength)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, passwordMemory, passwordIterations, passwordParallelism, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

func verifyPassword(encoded, password string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, ErrInvalidPasswordHash
	}
	version, err := parseValue(parts[2], "v")
	if err != nil || version != argon2.Version {
		return false, ErrInvalidPasswordHash
	}
	params := strings.Split(parts[3], ",")
	if len(params) != 3 {
		return false, ErrInvalidPasswordHash
	}
	memory, err := parseValue(params[0], "m")
	if err != nil || memory < 8*1024 || memory > 256*1024 {
		return false, ErrInvalidPasswordHash
	}
	iterations, err := parseValue(params[1], "t")
	if err != nil || iterations < 1 || iterations > 10 {
		return false, ErrInvalidPasswordHash
	}
	parallelism, err := parseValue(params[2], "p")
	if err != nil || parallelism < 1 || parallelism > 16 {
		return false, ErrInvalidPasswordHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 8 || len(salt) > 64 {
		return false, ErrInvalidPasswordHash
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expected) < 16 || len(expected) > 64 {
		return false, ErrInvalidPasswordHash
	}
	actual := argon2.IDKey([]byte(password), salt, uint32(iterations), uint32(memory), uint8(parallelism), uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}

func parseValue(value, key string) (int, error) {
	prefix := key + "="
	if !strings.HasPrefix(value, prefix) {
		return 0, ErrInvalidPasswordHash
	}
	parsed, err := strconv.Atoi(strings.TrimPrefix(value, prefix))
	if err != nil {
		return 0, ErrInvalidPasswordHash
	}
	return parsed, nil
}
