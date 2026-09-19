//go:build !windows

package compose

import "os"

func atomicReplace(source, destination string) error { return os.Rename(source, destination) }
