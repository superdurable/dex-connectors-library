// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pollingtrigger

import (
	"context"
	"strconv"
	"sync"

	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

// CursorStore persists one poller's cursor with compare-and-swap. *projectconfig.PollCursorStore, from
// LoadedProject.PollCursorStore or LoadedProject.PollFeedCursorStore, implements it.
type CursorStore interface {
	// ReadPollCursor returns the current record; its State is nil until the cursor is seeded.
	ReadPollCursor(ctx context.Context) (projectconfig.PollCursorRecord, error)
	// ReplacePollCursor writes state only while the head is at expectedRevision, returning
	// projectconfig.ErrConflict otherwise.
	ReplacePollCursor(ctx context.Context, expectedRevision string, state projectconfig.PollCursorState) (projectconfig.PollCursorRecord, error)
}

// NewMemoryCursorStore keeps one cursor in process memory with the same encoding, bounds, and
// compare-and-swap rules as project storage. Use it in tests and in applications without project
// storage. A restart reseeds, so changes made while the process was stopped are not delivered.
func NewMemoryCursorStore() CursorStore {
	store, err := projectconfig.NewPollFeedCursorStore(&memoryObjectStore{objects: map[string]projectconfig.Object{}},
		projectconfig.Scope{ProjectID: "memory", Kind: "live"},
		projectconfig.PollFeedKey{ConnectorID: "memory", ConnectionName: "memory", FeedName: "memoryCursor"})
	if err != nil {
		panic("memory poll cursor identity is invalid: " + err.Error())
	}
	return store
}

// memoryObjectStore is the smallest projectconfig.ObjectStore that keeps only current versions.
type memoryObjectStore struct {
	mutex    sync.Mutex
	objects  map[string]projectconfig.Object
	revision int
}

// ReadObject reads the current object.
func (store *memoryObjectStore) ReadObject(_ context.Context, key, _ string) (projectconfig.Object, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	object, ok := store.objects[key]
	if !ok {
		return projectconfig.Object{}, projectconfig.ErrObjectNotFound
	}
	object.Contents = append([]byte(nil), object.Contents...)
	return object, nil
}

// CreateObject creates an absent object.
func (store *memoryObjectStore) CreateObject(_ context.Context, key string, contents []byte) (projectconfig.Object, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if _, ok := store.objects[key]; ok {
		return projectconfig.Object{}, projectconfig.ErrConflict
	}
	return store.put(key, contents), nil
}

// CompareAndSwapObject replaces an object at expectedETag.
func (store *memoryObjectStore) CompareAndSwapObject(_ context.Context, key, expectedETag string, contents []byte) (projectconfig.Object, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if current, ok := store.objects[key]; !ok || current.ETag != expectedETag {
		return projectconfig.Object{}, projectconfig.ErrConflict
	}
	return store.put(key, contents), nil
}

func (store *memoryObjectStore) put(key string, contents []byte) projectconfig.Object {
	store.revision++
	revision := strconv.Itoa(store.revision)
	object := projectconfig.Object{Key: key, Version: revision, ETag: revision, Contents: append([]byte(nil), contents...)}
	store.objects[key] = object
	return object
}
