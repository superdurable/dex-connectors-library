// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//go:build unix

package callstate

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"
	"time"
)

// lockKeyFile opens path and takes flock(LOCK_EX) on it, polling so ctx can end
// the wait. After locking, the open file must still be the file at path; the
// pruner deletes lock files while holding them, so a mismatch reopens path.
func lockKeyFile(ctx context.Context, path string, pollInterval time.Duration) (*os.File, error) {
	for {
		file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
		if err != nil {
			return nil, fmt.Errorf("open call state lock file: %w", err)
		}
		if err := waitForExclusiveFileLock(ctx, file, pollInterval); err != nil {
			return nil, errors.Join(err, file.Close())
		}
		isCurrentFile, err := isFileAtPath(file, path)
		if err != nil {
			return nil, errors.Join(err, unlockAndCloseKeyFile(file))
		}
		if isCurrentFile {
			return file, nil
		}
		if err := unlockAndCloseKeyFile(file); err != nil {
			return nil, err
		}
	}
}

// tryLockFile takes flock(LOCK_EX) on file without waiting and reports whether
// it did. Any error other than contention is returned.
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

func waitForExclusiveFileLock(ctx context.Context, file *os.File, pollInterval time.Duration) error {
	for {
		isLocked, err := tryLockFile(file)
		if err != nil {
			return err
		}
		if isLocked {
			return nil
		}
		timer := time.NewTimer(pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("%w: %w", ErrLockWaitCanceled, context.Cause(ctx))
		case <-timer.C:
		}
	}
}

func isFileAtPath(file *os.File, path string) (bool, error) {
	openedInfo, err := file.Stat()
	if err != nil {
		return false, fmt.Errorf("inspect locked call state lock file: %w", err)
	}
	pathInfo, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect call state lock path: %w", err)
	}
	return os.SameFile(openedInfo, pathInfo), nil
}

func unlockAndCloseKeyFile(file *os.File) error {
	unlockErr := syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	return errors.Join(unlockErr, file.Close())
}
