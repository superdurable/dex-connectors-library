// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//go:build darwin || linux

package projectconfig

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// holdFileLock takes the exclusive flock of path through its own open file, as another process would.
func holdFileLock(t *testing.T, path string) *os.File {
	t.Helper()
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	require.NoError(t, err)
	require.NoError(t, syscall.Flock(int(file.Fd()), syscall.LOCK_EX))
	t.Cleanup(func() { _ = file.Close() })
	return file
}

func TestRefreshLockWaitEndsAtContextDeadline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.json.lock")
	holder := holdFileLock(t, path)
	lock := newFileRefreshLock(path, time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := lock.acquire(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorIs(t, err, ErrRefreshFailed)
	require.Less(t, time.Since(started), 5*time.Second)

	// The semaphore was returned, and the lock is acquired once the holder releases it.
	require.NoError(t, holder.Close())
	held, err := lock.acquire(context.Background())
	require.NoError(t, err)
	held.release()
}

func TestRefreshLockWaitEndsWhenContextIsCanceled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.json.lock")
	holdFileLock(t, path)
	lock := newFileRefreshLock(path, time.Minute)

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	_, err := lock.acquire(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, err, ErrRefreshFailed)
}

func TestRefreshLockMaxWaitBoundsContextWithoutDeadline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.json.lock")
	holdFileLock(t, path)
	lock := newFileRefreshLock(path, 100*time.Millisecond)

	started := time.Now()
	_, err := lock.acquire(context.Background())
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Less(t, time.Since(started), 5*time.Second)
}

func TestRefreshLockSerializesGoroutinesOfOneStore(t *testing.T) {
	lock := newFileRefreshLock(filepath.Join(t.TempDir(), "connections.json.lock"), time.Minute)
	first, err := lock.acquire(context.Background())
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = lock.acquire(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	first.release()
	first.release() // A second release does nothing.
	second, err := lock.acquire(context.Background())
	require.NoError(t, err)
	second.release()
}

// Two stores of one file in one process, such as during a reload, exclude each other through flock alone.
func TestRefreshLockExcludesAnotherStoreOfTheSameFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.json.lock")
	first, err := newFileRefreshLock(path, time.Minute).acquire(context.Background())
	require.NoError(t, err)
	other := newFileRefreshLock(path, time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = other.acquire(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	first.release()
	second, err := other.acquire(context.Background())
	require.NoError(t, err)
	second.release()

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestRefreshLockRefusesSymbolicLink(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "elsewhere")
	require.NoError(t, os.WriteFile(target, nil, 0o600))
	path := filepath.Join(directory, "connections.json.lock")
	require.NoError(t, os.Symlink(target, path))

	_, err := newFileRefreshLock(path, time.Minute).acquire(context.Background())
	require.ErrorIs(t, err, ErrRefreshFailed)
	require.ErrorContains(t, err, "open the credential refresh lock file")
}
