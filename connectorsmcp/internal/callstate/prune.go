// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package callstate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// orphanFileAge is how old a temporary file, or a lock file without a
	// record, must be before the pruner removes it.
	orphanFileAge = time.Hour
	// pruneLockWait bounds the pruner's wait for a key in use; it skips the key.
	pruneLockWait = 50 * time.Millisecond
)

// PruneResult counts what one Prune removed.
type PruneResult struct {
	// RemovedRecords counts records older than the retention.
	RemovedRecords int
	// RemovedLockFiles counts lock files without a record.
	RemovedLockFiles int
	// RemovedTemporaryFiles counts temporary files left by interrupted writes.
	RemovedTemporaryFiles int
}

// Prune removes records whose UpdatedAt is older than retention, lock files
// without a record that are older than an hour, and temporary files older
// than an hour. It deletes a key's files only while holding the key's lock,
// re-reading the record first, and skips a key whose lock stays busy. A
// record that cannot be read ages by its file modification time. Errors on
// single files are joined into the returned error after the walk.
func (store *Store) Prune(ctx context.Context, retention time.Duration) (PruneResult, error) {
	var result PruneResult
	shards, err := os.ReadDir(store.callsDirectory)
	if err != nil {
		return result, fmt.Errorf("list call state directory: %w", err)
	}
	var errs []error
	for _, shard := range shards {
		if !shard.IsDir() || len(shard.Name()) != 2 {
			continue
		}
		if err := store.pruneShard(ctx, filepath.Join(store.callsDirectory, shard.Name()), retention, &result); err != nil {
			errs = append(errs, err)
		}
		if ctx.Err() != nil {
			break
		}
	}
	return result, errors.Join(errs...)
}

func (store *Store) pruneShard(ctx context.Context, shardDirectory string, retention time.Duration, result *PruneResult) error {
	entries, err := os.ReadDir(shardDirectory)
	if err != nil {
		return fmt.Errorf("list call state shard: %w", err)
	}
	now := store.now()
	names := map[string]bool{}
	for _, entry := range entries {
		names[entry.Name()] = true
	}
	var errs []error
	for _, entry := range entries {
		name := entry.Name()
		info, err := entry.Info()
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		path := filepath.Join(shardDirectory, name)
		switch {
		case strings.Contains(name, temporaryFileMarker):
			if now.Sub(info.ModTime()) > orphanFileAge {
				if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
					errs = append(errs, err)
				} else {
					result.RemovedTemporaryFiles++
				}
			}
		case strings.HasSuffix(name, recordFileSuffix):
			stateKey := strings.TrimSuffix(name, recordFileSuffix)
			if !stateKeyPattern.MatchString(stateKey) || now.Sub(recordAge(path, info)) <= retention {
				continue
			}
			isRemoved, err := store.pruneKey(ctx, stateKey, func(lock *KeyLock) bool {
				currentInfo, err := os.Stat(lock.recordPath())
				return err == nil && store.now().Sub(recordAge(lock.recordPath(), currentInfo)) > retention
			})
			if err != nil {
				errs = append(errs, err)
			} else if isRemoved {
				result.RemovedRecords++
			}
		case strings.HasSuffix(name, lockFileSuffix):
			stateKey := strings.TrimSuffix(name, lockFileSuffix)
			if !stateKeyPattern.MatchString(stateKey) || names[stateKey+recordFileSuffix] || now.Sub(info.ModTime()) <= orphanFileAge {
				continue
			}
			isRemoved, err := store.pruneKey(ctx, stateKey, func(lock *KeyLock) bool {
				_, err := os.Stat(lock.recordPath())
				return errors.Is(err, fs.ErrNotExist)
			})
			if err != nil {
				errs = append(errs, err)
			} else if isRemoved {
				result.RemovedLockFiles++
			}
		}
	}
	return errors.Join(errs...)
}

// pruneKey removes the key's files under its lock when isStillPrunable agrees.
func (store *Store) pruneKey(ctx context.Context, stateKey string, isStillPrunable func(*KeyLock) bool) (bool, error) {
	waitContext, cancel := context.WithTimeout(ctx, pruneLockWait)
	defer cancel()
	lock, err := store.Lock(waitContext, stateKey)
	if errors.Is(err, ErrLockWaitCanceled) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	isRemoved := false
	var removeErr error
	if isStillPrunable(lock) {
		removeErr = lock.removeRecordAndLockFile()
		isRemoved = removeErr == nil
	}
	return isRemoved, errors.Join(removeErr, lock.Release())
}

// recordAge returns the record's UpdatedAt, or the file time when it cannot be read.
func recordAge(path string, info fs.FileInfo) time.Time {
	contents, err := os.ReadFile(path)
	if err != nil {
		return info.ModTime()
	}
	var stamp struct {
		UpdatedAt time.Time `json:"updatedAt"`
	}
	if json.Unmarshal(contents, &stamp) != nil || stamp.UpdatedAt.IsZero() {
		return info.ModTime()
	}
	return stamp.UpdatedAt
}
