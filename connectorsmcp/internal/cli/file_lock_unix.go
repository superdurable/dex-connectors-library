// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//go:build unix

package cli

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// tryLockFile takes flock(LOCK_EX) on file without waiting and reports whether it did.
func tryLockFile(file *os.File) (bool, error) {
	for {
		err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		switch {
		case err == nil:
			return true, nil
		case errors.Is(err, syscall.EINTR):
			continue
		case errors.Is(err, syscall.EWOULDBLOCK):
			return false, nil
		default:
			return false, fmt.Errorf("lock %s: %w", file.Name(), err)
		}
	}
}

func unlockAndCloseFile(file *os.File) error {
	return errors.Join(syscall.Flock(int(file.Fd()), syscall.LOCK_UN), file.Close())
}
