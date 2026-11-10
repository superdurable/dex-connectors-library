// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package callstate keeps keyed write call state (dex-connectors-mcp/call-state/v1);
// connectorsmcp/README.md documents the protocol.
package callstate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	formatFileName      = "FORMAT"
	recordFileSuffix    = ".json"
	lockFileSuffix      = ".lock"
	temporaryFileMarker = ".tmp-"
	// defaultLockPollInterval is how often a waiter retries a held key lock.
	defaultLockPollInterval = 10 * time.Millisecond
)

// callsDirectoryPath is the versioned subtree; a later format sits beside it.
var callsDirectoryPath = filepath.Join("calls", "v1")

var stateKeyPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ErrLockWaitCanceled means ctx ended before the key's lock was acquired.
var ErrLockWaitCanceled = errors.New("call state lock wait ended before the lock was acquired")

// Store reads and writes the records of one state directory. It is safe for
// concurrent use.
type Store struct {
	callsDirectory   string
	now              func() time.Time
	lockPollInterval time.Duration
	keyMutexes       *keyMutexRegistry
}

// Open prepares stateDirectory, writes or checks FORMAT, and returns its Store;
// now stamps UpdatedAt.
func Open(stateDirectory string, now func() time.Time) (*Store, error) {
	if now == nil {
		return nil, errors.New("call state store clock is required")
	}
	if err := PrepareStateDirectory(stateDirectory); err != nil {
		return nil, err
	}
	callsDirectory := filepath.Join(stateDirectory, callsDirectoryPath)
	if err := os.MkdirAll(callsDirectory, 0o700); err != nil {
		return nil, fmt.Errorf("create call state directory: %w", err)
	}
	if err := syncDirectory(filepath.Dir(callsDirectory)); err != nil {
		return nil, err
	}
	if err := ensureFormatFile(callsDirectory); err != nil {
		return nil, err
	}
	return &Store{
		callsDirectory:   callsDirectory,
		now:              now,
		lockPollInterval: defaultLockPollInterval,
		keyMutexes:       newKeyMutexRegistry(),
	}, nil
}

// PrepareStateDirectory creates stateDirectory 0700 and refuses symlinks,
// non-directories, and group- or world-writable modes.
func PrepareStateDirectory(stateDirectory string) error {
	if !filepath.IsAbs(stateDirectory) {
		return fmt.Errorf("state directory %q must be an absolute path", stateDirectory)
	}
	info, err := os.Lstat(stateDirectory)
	if errors.Is(err, fs.ErrNotExist) {
		if err := os.MkdirAll(stateDirectory, 0o700); err != nil {
			return fmt.Errorf("create state directory: %w", err)
		}
		info, err = os.Lstat(stateDirectory)
	}
	if err != nil {
		return fmt.Errorf("inspect state directory: %w", err)
	}
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		return fmt.Errorf("state directory %s is a symbolic link; give its target instead", stateDirectory)
	case !info.IsDir():
		return fmt.Errorf("state directory %s is not a directory", stateDirectory)
	case info.Mode().Perm()&0o022 != 0:
		return fmt.Errorf("state directory %s is writable by group or others; run chmod 700 on it", stateDirectory)
	}
	return nil
}

func ensureFormatFile(callsDirectory string) error {
	path := filepath.Join(callsDirectory, formatFileName)
	contents, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return writeFileDurably(callsDirectory, formatFileName, []byte(FormatVersion+"\n"))
	}
	if err != nil {
		return fmt.Errorf("read call state FORMAT: %w", err)
	}
	if found := strings.TrimSpace(string(contents)); found != FormatVersion {
		return fmt.Errorf("call state directory %s has format %q; this server reads only %q (a newer server wrote it?)", callsDirectory, found, FormatVersion)
	}
	return nil
}

// KeyLock is one held key lock, owned by the call that took it.
type KeyLock struct {
	store           *Store
	stateKey        string
	file            *os.File
	releaseKeyMutex func()
}

// Lock waits for stateKey's lock; an ended ctx wraps ErrLockWaitCanceled, other
// errors are file failures.
func (store *Store) Lock(ctx context.Context, stateKey string) (*KeyLock, error) {
	if !stateKeyPattern.MatchString(stateKey) {
		return nil, fmt.Errorf("call state key %q is not a lowercase SHA-256", stateKey)
	}
	releaseKeyMutex, err := store.keyMutexes.acquire(ctx, stateKey)
	if err != nil {
		return nil, err
	}
	shardDirectory, err := store.ensureShardDirectory(stateKey)
	if err != nil {
		releaseKeyMutex()
		return nil, err
	}
	file, err := lockKeyFile(ctx, filepath.Join(shardDirectory, stateKey+lockFileSuffix), store.lockPollInterval)
	if err != nil {
		releaseKeyMutex()
		return nil, err
	}
	return &KeyLock{store: store, stateKey: stateKey, file: file, releaseKeyMutex: releaseKeyMutex}, nil
}

// Read returns the key's record, if any; IsRecordUnreadable marks untrustworthy ones.
func (lock *KeyLock) Read() (Record, bool, error) {
	contents, err := os.ReadFile(lock.recordPath())
	if errors.Is(err, fs.ErrNotExist) {
		return Record{}, false, nil
	}
	if err != nil {
		return Record{}, false, fmt.Errorf("read call state record: %w", err)
	}
	var record Record
	if err := json.Unmarshal(contents, &record); err != nil {
		return Record{}, true, fmt.Errorf("%w: %s", errRecordUnreadable, "it is not a JSON record")
	}
	if err := record.validate(lock.stateKey); err != nil {
		return Record{}, true, fmt.Errorf("%w: %s", errRecordUnreadable, err.Error())
	}
	return record, true, nil
}

// Write stores record durably; after an error either version may be stored.
func (lock *KeyLock) Write(record *Record) error {
	record.UpdatedAt = lock.store.now().UTC()
	if err := record.validate(lock.stateKey); err != nil {
		return fmt.Errorf("call state record is invalid: %w", err)
	}
	contents, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode call state record: %w", err)
	}
	return writeFileDurably(filepath.Dir(lock.recordPath()), lock.stateKey+recordFileSuffix, contents)
}

// Release releases the lock. Closing the file releases flock even when the
// explicit unlock fails.
func (lock *KeyLock) Release() error {
	err := unlockAndCloseKeyFile(lock.file)
	lock.releaseKeyMutex()
	return err
}

// removeRecordAndLockFile must be the last act under the lock: new
// callers may lock a new file at once.
func (lock *KeyLock) removeRecordAndLockFile() error {
	if err := os.Remove(lock.recordPath()); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.Remove(lock.lockPath()); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return syncDirectory(filepath.Dir(lock.recordPath()))
}

func (lock *KeyLock) recordPath() string {
	return filepath.Join(lock.store.callsDirectory, lock.stateKey[:2], lock.stateKey+recordFileSuffix)
}

func (lock *KeyLock) lockPath() string {
	return filepath.Join(lock.store.callsDirectory, lock.stateKey[:2], lock.stateKey+lockFileSuffix)
}

// HasRecordFile is false only when the key's record is provably absent.
func (store *Store) HasRecordFile(stateKey string) bool {
	if !stateKeyPattern.MatchString(stateKey) {
		return true
	}
	_, err := os.Stat(filepath.Join(store.callsDirectory, stateKey[:2], stateKey+recordFileSuffix))
	return !errors.Is(err, fs.ErrNotExist)
}

func (store *Store) ensureShardDirectory(stateKey string) (string, error) {
	shardDirectory := filepath.Join(store.callsDirectory, stateKey[:2])
	if _, err := os.Stat(shardDirectory); err == nil {
		return shardDirectory, nil
	}
	if err := os.Mkdir(shardDirectory, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return "", fmt.Errorf("create call state shard directory: %w", err)
	}
	if err := syncDirectory(store.callsDirectory); err != nil {
		return "", err
	}
	return shardDirectory, nil
}

// writeFileDurably replaces directory/fileName with contents: write a
// temporary file, sync it, rename it, then sync the directory.
func writeFileDurably(directory string, fileName string, contents []byte) (err error) {
	temporary, err := os.CreateTemp(directory, "."+fileName+temporaryFileMarker+"*")
	if err != nil {
		return fmt.Errorf("create temporary call state file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		if err != nil {
			// Best effort: the pruner removes temporary files left behind.
			_ = os.Remove(temporaryPath)
		}
	}()
	if _, err = temporary.Write(contents); err != nil {
		return errors.Join(fmt.Errorf("write temporary call state file: %w", err), temporary.Close())
	}
	if err = temporary.Sync(); err != nil {
		return errors.Join(fmt.Errorf("sync temporary call state file: %w", err), temporary.Close())
	}
	if err = temporary.Close(); err != nil {
		return fmt.Errorf("close temporary call state file: %w", err)
	}
	if err = os.Rename(temporaryPath, filepath.Join(directory, fileName)); err != nil {
		return fmt.Errorf("rename call state file: %w", err)
	}
	return syncDirectory(directory)
}

func syncDirectory(directory string) error {
	handle, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("open directory for sync: %w", err)
	}
	syncErr := handle.Sync()
	closeErr := handle.Close()
	if syncErr != nil {
		return fmt.Errorf("sync directory: %w", syncErr)
	}
	return closeErr
}
