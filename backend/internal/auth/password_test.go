package auth

import (
	"strings"
	"testing"
)

func TestPasswordHashRoundTrip(t *testing.T) {
	password := "correct horse battery staple"
	encoded, err := hashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(encoded, password) {
		t.Fatal("encoded password contains plaintext")
	}
	valid, err := verifyPassword(encoded, password)
	if err != nil || !valid {
		t.Fatalf("expected password to verify: valid=%t err=%v", valid, err)
	}
	valid, err = verifyPassword(encoded, "wrong password")
	if err != nil {
		t.Fatal(err)
	}
	if valid {
		t.Fatal("wrong password verified")
	}
}

func TestRejectMalformedPasswordHash(t *testing.T) {
	if valid, err := verifyPassword("$argon2id$invalid", "password"); err == nil || valid {
		t.Fatalf("expected malformed hash rejection: valid=%t err=%v", valid, err)
	}
}
