// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//go:build unix

package callstate

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const helperModeEnvironment = "CALLSTATE_TEST_HELPER_MODE"

func TestMain(m *testing.M) {
	if mode := os.Getenv(helperModeEnvironment); mode != "" {
		runHelperProcess(mode)
		return
	}
	os.Exit(m.Run())
}

// runHelperProcess holds one key lock until it is killed.
func runHelperProcess(mode string) {
	if mode != "hold-lock" {
		fmt.Fprintln(os.Stderr, "unknown helper mode", mode)
		os.Exit(2)
	}
	store, err := Open(os.Getenv("CALLSTATE_TEST_STATE_DIR"), time.Now)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if _, err := store.Lock(context.Background(), os.Getenv("CALLSTATE_TEST_STATE_KEY")); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("locked")
	for {
		time.Sleep(time.Hour)
	}
}

func TestStateKeyAndRequestDigestAreFrozen(t *testing.T) {
	// These vectors pin the v1 format; changing them strands stored state.
	require.Equal(t, "9dd89a627a9e53ca4baacef633e2997fc1e3d2debb688ef5f9251e3dc455f49d", StateKey("flow-1/turn-1/call-1"))
	request := Request{
		ConnectorID:     "slack",
		OperationID:     "postThreadReply",
		ConnectionName:  "support-slack",
		ProjectIdentity: "file:/Users/me/.dex/connectors/connections.json",
		ArgumentsDigest: "44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a",
	}
	require.Equal(t, "88c783d816be8fd3ff17830563c8200eab33735e32e6e9fa37513f1d31808146", request.Digest())
}

func TestValidateIdempotencyKey(t *testing.T) {
	require.NoError(t, ValidateIdempotencyKey("flow/turn/call"))
	require.NoError(t, ValidateIdempotencyKey(strings.Repeat("k", MaximumIdempotencyKeyBytes)))
	require.Error(t, ValidateIdempotencyKey(""))
	require.Error(t, ValidateIdempotencyKey(strings.Repeat("k", MaximumIdempotencyKeyBytes+1)))
	require.Error(t, ValidateIdempotencyKey(string([]byte{0xff, 0xfe})))
}

func TestOpenCreatesPrivateDirectoriesAndFormat(t *testing.T) {
	stateDirectory := filepath.Join(t.TempDir(), "state")
	_, err := Open(stateDirectory, time.Now)
	require.NoError(t, err)
	info, err := os.Stat(stateDirectory)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), info.Mode().Perm())
	contents, err := os.ReadFile(filepath.Join(stateDirectory, "calls", "v1", "FORMAT"))
	require.NoError(t, err)
	require.Equal(t, FormatVersion+"\n", string(contents))
	_, err = Open(stateDirectory, time.Now)
	require.NoError(t, err, "a second open of the same format succeeds")
}

func TestOpenRefusesUnsafeStateDirectories(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	require.NoError(t, os.Mkdir(target, 0o700))
	link := filepath.Join(root, "link")
	require.NoError(t, os.Symlink(target, link))
	_, err := Open(link, time.Now)
	require.ErrorContains(t, err, "symbolic link")

	shared := filepath.Join(root, "shared")
	require.NoError(t, os.Mkdir(shared, 0o700))
	require.NoError(t, os.Chmod(shared, 0o770))
	_, err = Open(shared, time.Now)
	require.ErrorContains(t, err, "writable by group or others")

	_, err = Open("relative/state", time.Now)
	require.ErrorContains(t, err, "absolute")
}

func TestOpenRefusesAnotherFormat(t *testing.T) {
	stateDirectory := filepath.Join(t.TempDir(), "state")
	_, err := Open(stateDirectory, time.Now)
	require.NoError(t, err)
	formatPath := filepath.Join(stateDirectory, "calls", "v1", "FORMAT")
	require.NoError(t, os.WriteFile(formatPath, []byte("dex-connectors-mcp/call-state/v2\n"), 0o600))
	_, err = Open(stateDirectory, time.Now)
	require.ErrorContains(t, err, "call-state/v2")
}

func TestWriteThenReadRoundTripsARecord(t *testing.T) {
	store := openTestStore(t)
	stateKey := StateKey("key-1")
	lock, err := store.Lock(context.Background(), stateKey)
	require.NoError(t, err)
	defer func() { require.NoError(t, lock.Release()) }()

	_, isFound, err := lock.Read()
	require.NoError(t, err)
	require.False(t, isFound)

	record := newTestRecord(stateKey)
	record.Checkpoint = json.RawMessage(`{"callId":"call-1","attempt":1,"state":"claimed"}`)
	require.NoError(t, lock.Write(&record))
	read, isFound, err := lock.Read()
	require.NoError(t, err)
	require.True(t, isFound)
	require.Equal(t, record.Request, read.Request)
	require.Equal(t, record.Attempt, read.Attempt)
	require.JSONEq(t, string(record.Checkpoint), string(read.Checkpoint))
	require.False(t, read.UpdatedAt.IsZero())

	info, err := os.Stat(lock.recordPath())
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	lockInfo, err := os.Stat(lock.lockPath())
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), lockInfo.Mode().Perm())
}

func TestWriteRejectsAnInconsistentRecord(t *testing.T) {
	store := openTestStore(t)
	stateKey := StateKey("key-1")
	lock, err := store.Lock(context.Background(), stateKey)
	require.NoError(t, err)
	defer func() { require.NoError(t, lock.Release()) }()
	record := newTestRecord(stateKey)
	record.Status = StatusCompleted
	require.Error(t, lock.Write(&record), "a completed record needs a result")
	_, isFound, err := lock.Read()
	require.NoError(t, err)
	require.False(t, isFound)
}

func TestReadReportsUntrustworthyRecordsAsUnreadable(t *testing.T) {
	cases := map[string]func(record map[string]any){
		"unknown schema version":  func(record map[string]any) { record["schemaVersion"] = "dex-connectors-mcp/call-state/v2" },
		"tampered request":        func(record map[string]any) { record["request"].(map[string]any)["connectionName"] = "other" },
		"foreign state key":       func(record map[string]any) { record["stateKey"] = StateKey("another key") },
		"unknown status":          func(record map[string]any) { record["status"] = "paused" },
		"read effect":             func(record map[string]any) { record["effect"] = "read" },
		"attempt zero":            func(record map[string]any) { record["attempt"] = 0 },
		"completed without result": func(record map[string]any) { record["status"] = "completed" },
	}
	for name, corrupt := range cases {
		t.Run(name, func(t *testing.T) {
			store := openTestStore(t)
			stateKey := StateKey("key-1")
			lock, err := store.Lock(context.Background(), stateKey)
			require.NoError(t, err)
			defer func() { require.NoError(t, lock.Release()) }()
			record := newTestRecord(stateKey)
			require.NoError(t, lock.Write(&record))
			contents, err := os.ReadFile(lock.recordPath())
			require.NoError(t, err)
			var decoded map[string]any
			require.NoError(t, json.Unmarshal(contents, &decoded))
			corrupt(decoded)
			rewritten, err := json.Marshal(decoded)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(lock.recordPath(), rewritten, 0o600))
			_, isFound, err := lock.Read()
			require.True(t, isFound)
			require.True(t, IsRecordUnreadable(err), "error: %v", err)
		})
	}
	t.Run("not JSON", func(t *testing.T) {
		store := openTestStore(t)
		stateKey := StateKey("key-1")
		lock, err := store.Lock(context.Background(), stateKey)
		require.NoError(t, err)
		defer func() { require.NoError(t, lock.Release()) }()
		require.NoError(t, os.WriteFile(lock.recordPath(), []byte(`{"schemaVersion":`), 0o600))
		_, isFound, err := lock.Read()
		require.True(t, isFound)
		require.True(t, IsRecordUnreadable(err))
	})
}

func TestLockSerializesGoroutinesAndHonorsTheContext(t *testing.T) {
	store := openTestStore(t)
	stateKey := StateKey("key-1")
	first, err := store.Lock(context.Background(), stateKey)
	require.NoError(t, err)

	waitContext, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = store.Lock(waitContext, stateKey)
	require.ErrorIs(t, err, ErrLockWaitCanceled)

	acquired := make(chan *KeyLock)
	go func() {
		second, err := store.Lock(context.Background(), stateKey)
		if err != nil {
			panic(err)
		}
		acquired <- second
	}()
	select {
	case <-acquired:
		t.Fatal("a second goroutine acquired a held key")
	case <-time.After(30 * time.Millisecond):
	}
	require.NoError(t, first.Release())
	second := <-acquired
	require.NoError(t, second.Release())
}

func TestLockExcludesAnotherProcessUntilItIsKilled(t *testing.T) {
	stateDirectory := filepath.Join(t.TempDir(), "state")
	store, err := Open(stateDirectory, time.Now)
	require.NoError(t, err)
	stateKey := StateKey("key-1")
	helper := exec.Command(os.Args[0], "-test.run=^$")
	helper.Env = append(os.Environ(), helperModeEnvironment+"=hold-lock", "CALLSTATE_TEST_STATE_DIR="+stateDirectory, "CALLSTATE_TEST_STATE_KEY="+stateKey)
	stdout, err := helper.StdoutPipe()
	require.NoError(t, err)
	helper.Stderr = os.Stderr
	require.NoError(t, helper.Start())
	t.Cleanup(func() { _ = helper.Process.Kill(); _ = helper.Wait() })
	line, err := bufio.NewReader(stdout).ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "locked\n", line)

	waitContext, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = store.Lock(waitContext, stateKey)
	require.ErrorIs(t, err, ErrLockWaitCanceled, "another process holds the key")

	require.NoError(t, helper.Process.Signal(syscall.SIGKILL))
	_ = helper.Wait()
	acquireContext, cancelAcquire := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelAcquire()
	lock, err := store.Lock(acquireContext, stateKey)
	require.NoError(t, err, "the kernel releases flock when the holder dies")
	require.NoError(t, lock.Release())
}

func TestLockReverifiesTheLockFileAfterThePrunerDeletesIt(t *testing.T) {
	store := openTestStore(t)
	stateKey := StateKey("key-1")
	pruning, err := store.Lock(context.Background(), stateKey)
	require.NoError(t, err)
	lockPath := pruning.lockPath()
	// A waiter that opened the lock file before the pruner deleted it.
	staleWaiter, err := os.OpenFile(lockPath, os.O_RDWR, 0o600)
	require.NoError(t, err)
	defer staleWaiter.Close()
	require.NoError(t, pruning.removeRecordAndLockFile())
	require.NoError(t, pruning.Release())

	isLocked, err := tryLockFile(staleWaiter)
	require.NoError(t, err)
	require.True(t, isLocked, "the deleted inode is free")
	isCurrent, err := isFileAtPath(staleWaiter, lockPath)
	require.NoError(t, err)
	require.False(t, isCurrent, "the waiter must notice its inode was deleted")

	// lockKeyFile retries on the path, so it and a second locker exclude each other.
	current, err := lockKeyFile(context.Background(), lockPath, time.Millisecond)
	require.NoError(t, err)
	waitContext, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err = lockKeyFile(waitContext, lockPath, time.Millisecond)
	require.ErrorIs(t, err, ErrLockWaitCanceled)
	require.NoError(t, unlockAndCloseKeyFile(current))
}

func TestConcurrentLockersNeverHoldOneKeyTogetherWhileItIsPruned(t *testing.T) {
	store := openTestStore(t)
	stateKey := StateKey("key-1")
	lockPath := filepath.Join(store.callsDirectory, stateKey[:2], stateKey+lockFileSuffix)
	_, err := store.ensureShardDirectory(stateKey)
	require.NoError(t, err)
	var holders sync.Mutex
	held := 0
	var wait sync.WaitGroup
	for worker := range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for iteration := range 40 {
				// lockKeyFile skips the in-process mutex, as another process would.
				file, err := lockKeyFile(context.Background(), lockPath, time.Millisecond)
				if err != nil {
					panic(err)
				}
				holders.Lock()
				held++
				if held != 1 {
					panic(fmt.Sprintf("%d lockers hold one key", held))
				}
				holders.Unlock()
				holders.Lock()
				held--
				holders.Unlock()
				if (worker+iteration)%3 == 0 {
					// Behave as the pruner: its last act under the lock deletes the lock file,
					// so a new locker may start on a new file before this one is released.
					if err := os.Remove(lockPath); err != nil && !errors.Is(err, os.ErrNotExist) {
						panic(err)
					}
				}
				if err := unlockAndCloseKeyFile(file); err != nil {
					panic(err)
				}
			}
		}()
	}
	wait.Wait()
}

func TestReadOnlyStateDirectoryFailsWritesWithoutLosingTheRecord(t *testing.T) {
	store := openTestStore(t)
	stateKey := StateKey("key-1")
	lock, err := store.Lock(context.Background(), stateKey)
	require.NoError(t, err)
	record := newTestRecord(stateKey)
	require.NoError(t, lock.Write(&record))
	shardDirectory := filepath.Dir(lock.recordPath())
	require.NoError(t, os.Chmod(shardDirectory, 0o500))
	t.Cleanup(func() { _ = os.Chmod(shardDirectory, 0o700) })

	record.Attempt = 2
	require.Error(t, lock.Write(&record))
	read, isFound, err := lock.Read()
	require.NoError(t, err)
	require.True(t, isFound)
	require.Equal(t, int32(1), read.Attempt, "a failed write leaves the previous record")
	require.NoError(t, lock.Release())

	otherKey := StateKey("key-2")
	otherShard := filepath.Join(store.callsDirectory, otherKey[:2])
	if otherShard == shardDirectory {
		t.Skip("both keys share a shard")
	}
	require.NoError(t, os.Chmod(store.callsDirectory, 0o500))
	t.Cleanup(func() { _ = os.Chmod(store.callsDirectory, 0o700) })
	_, err = store.Lock(context.Background(), otherKey)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrLockWaitCanceled, "a read-only store is a failed file operation")
}

func TestPruneRemovesExpiredRecordsOrphanLocksAndTemporaryFiles(t *testing.T) {
	now := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	clock := &testClock{now: now}
	stateDirectory := filepath.Join(t.TempDir(), "state")
	store, err := Open(stateDirectory, clock.Now)
	require.NoError(t, err)

	expiredKey := StateKey("expired")
	writeTestRecord(t, store, expiredKey)
	clock.now = now.Add(31 * 24 * time.Hour)
	freshKey := StateKey("fresh")
	writeTestRecord(t, store, freshKey)
	heldExpiredKey := StateKey("held")
	clock.now = now
	writeTestRecord(t, store, heldExpiredKey)
	clock.now = now.Add(31 * 24 * time.Hour)

	orphanKey := StateKey("orphan")
	orphanShard, err := store.ensureShardDirectory(orphanKey)
	require.NoError(t, err)
	orphanLock := filepath.Join(orphanShard, orphanKey+lockFileSuffix)
	require.NoError(t, os.WriteFile(orphanLock, nil, 0o600))
	temporaryFile := filepath.Join(orphanShard, "."+orphanKey+".json"+temporaryFileMarker+"123")
	require.NoError(t, os.WriteFile(temporaryFile, []byte("{"), 0o600))
	old := clock.now.Add(-2 * time.Hour)
	require.NoError(t, os.Chtimes(orphanLock, old, old))
	require.NoError(t, os.Chtimes(temporaryFile, old, old))

	held, err := store.Lock(context.Background(), heldExpiredKey)
	require.NoError(t, err)
	result, err := store.Prune(context.Background(), 30*24*time.Hour)
	require.NoError(t, err)
	require.Equal(t, PruneResult{RemovedRecords: 1, RemovedLockFiles: 1, RemovedTemporaryFiles: 1}, result)
	require.NoError(t, held.Release())

	requireRecordExists(t, store, expiredKey, false)
	requireRecordExists(t, store, freshKey, true)
	requireRecordExists(t, store, heldExpiredKey, true)
	_, err = os.Stat(orphanLock)
	require.ErrorIs(t, err, os.ErrNotExist)

	result, err = store.Prune(context.Background(), 30*24*time.Hour)
	require.NoError(t, err)
	require.Equal(t, 1, result.RemovedRecords, "the key released since the first prune is removed now")
}

func TestDirectorySyncWorksOnThisFileSystem(t *testing.T) {
	require.NoError(t, syncDirectory(t.TempDir()))
}

type testClock struct{ now time.Time }

func (clock *testClock) Now() time.Time { return clock.now }

func openTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "state"), time.Now)
	require.NoError(t, err)
	return store
}

func newTestRecord(stateKey string) Record {
	request := Request{ConnectorID: "example-chat", OperationID: "postReply", ConnectionName: "work", ProjectIdentity: "file:/tmp/connections.json", ArgumentsDigest: strings.Repeat("a", 64)}
	first := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	return Record{
		SchemaVersion:     FormatVersion,
		StateKey:          stateKey,
		Request:           request,
		RequestDigest:     request.Digest(),
		Effect:            sdkgo.AgentToolEffectNonIdempotentWrite,
		CallID:            "call-1",
		FirstAttemptAt:    first,
		RetryWindowEndsAt: first.Add(time.Hour),
		Attempt:           1,
		Status:            StatusRunning,
		ServerVersion:     "test",
	}
}

func writeTestRecord(t *testing.T, store *Store, stateKey string) {
	t.Helper()
	lock, err := store.Lock(context.Background(), stateKey)
	require.NoError(t, err)
	record := newTestRecord(stateKey)
	require.NoError(t, lock.Write(&record))
	require.NoError(t, lock.Release())
}

func requireRecordExists(t *testing.T, store *Store, stateKey string, isExpected bool) {
	t.Helper()
	lock, err := store.Lock(context.Background(), stateKey)
	require.NoError(t, err)
	defer func() { require.NoError(t, lock.Release()) }()
	_, isFound, err := lock.Read()
	require.NoError(t, err)
	require.Equal(t, isExpected, isFound)
}
