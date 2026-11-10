// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//go:build darwin || linux

package projectconfig

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
)

// isCrossProcessRefreshLockSupported reports whether this platform coordinates refresh across processes.
const isCrossProcessRefreshLockSupported = true

// lockFileExclusively polls a non-blocking flock(2), which unlike a blocking one observes ctx, until it holds the lock.
func lockFileExclusively(ctx context.Context, path string) (*os.File, error) {
	// O_NOFOLLOW refuses a planted link; Go opens close-on-exec, so a child never inherits the lock.
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, fmt.Errorf("%w: open the credential refresh lock file: %w", ErrRefreshFailed, err)
	}
	delay := refreshLockFirstPollDelay
	for {
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return file, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			// The descriptor holds no lock, so a failed Close changes nothing.
			_ = file.Close()
			return nil, fmt.Errorf("%w: lock the credential refresh lock file: %w", ErrRefreshFailed, err)
		}
		if err = waitForRefreshLockPoll(ctx, delay); err != nil {
			_ = file.Close()
			return nil, err
		}
		delay = nextRefreshLockPollDelay(delay)
	}
}

// syncDirectory makes a completed rename in directory durable.
func syncDirectory(directory string) error {
	handle, err := os.Open(directory)
	if err != nil {
		return err
	}
	syncErr := handle.Sync()
	return errors.Join(syncErr, handle.Close())
}
