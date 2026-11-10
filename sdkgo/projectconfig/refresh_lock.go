// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package projectconfig

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"
)

const (
	// refreshLockFileSuffix names the lock file beside a connections file: connections.json.lock.
	refreshLockFileSuffix = ".lock"
	// refreshLockMaxWait bounds a lock wait, so a stopped holder process cannot block a call forever.
	refreshLockMaxWait = 2 * time.Minute
	// refreshLockFirstPollDelay and refreshLockMaxPollDelay bound the polling of a non-blocking file lock.
	refreshLockFirstPollDelay = 5 * time.Millisecond
	refreshLockMaxPollDelay   = 100 * time.Millisecond
)

// credentialRefreshLocker is an ObjectStore whose conditional writes coordinate one process only, so refresh also takes its lock.
type credentialRefreshLocker interface {
	// lockCredentialRefresh waits for the exclusive refresh lock until ctx ends.
	lockCredentialRefresh(ctx context.Context) (*heldRefreshLock, error)
	// verifyCredentialWriteBack proves, under the lock, that a refreshed credential can be written back.
	verifyCredentialWriteBack() error
}

// fileRefreshLock orders one store's goroutines with a semaphore, and processes with flock(2) on a never-replaced lock file.
type fileRefreshLock struct {
	path      string
	maxWait   time.Duration
	semaphore chan struct{}
}

// heldRefreshLock is one acquisition of a fileRefreshLock. Its file is nil on a platform without file locks.
type heldRefreshLock struct {
	owner       *fileRefreshLock
	file        *os.File
	releaseOnce sync.Once
}

// newFileRefreshLock returns the lock that path, the lock file, implements. It neither creates nor opens the file.
func newFileRefreshLock(path string, maxWait time.Duration) *fileRefreshLock {
	return &fileRefreshLock{path: path, maxWait: maxWait, semaphore: make(chan struct{}, 1)}
}

// acquire waits for the semaphore, then the file lock, until ctx ends or maxWait passes.
func (lock *fileRefreshLock) acquire(ctx context.Context) (*heldRefreshLock, error) {
	waitContext, cancel := context.WithTimeout(ctx, lock.maxWait)
	defer cancel()
	select {
	case lock.semaphore <- struct{}{}:
	case <-waitContext.Done():
		return nil, refreshLockWaitError(waitContext.Err())
	}
	file, err := lockFileExclusively(waitContext, lock.path)
	if err != nil {
		<-lock.semaphore
		return nil, err
	}
	return &heldRefreshLock{owner: lock, file: file}, nil
}

// release unlocks the file, then the semaphore. Later calls do nothing.
func (held *heldRefreshLock) release() {
	held.releaseOnce.Do(held.releaseNow)
}

func (held *heldRefreshLock) releaseNow() {
	if held.file != nil {
		// Closing the only descriptor of the open file releases its lock even when Close reports an error.
		_ = held.file.Close()
	}
	<-held.owner.semaphore
}

// waitForRefreshLockPoll sleeps for one polling delay unless ctx ends first.
func waitForRefreshLockPoll(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return refreshLockWaitError(ctx.Err())
	case <-timer.C:
		return nil
	}
}

func refreshLockWaitError(cause error) error {
	return fmt.Errorf("%w: wait for the credential refresh lock: %w", ErrRefreshFailed, cause)
}

func nextRefreshLockPollDelay(delay time.Duration) time.Duration {
	return min(2*delay, refreshLockMaxPollDelay)
}
