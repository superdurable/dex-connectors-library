// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package projectstoretest keeps the Google Sheets connector tests' project Trigger inboxes and poll cursors
// in memory.
package projectstoretest

import (
	"context"
	"slices"
	"strconv"
	"sync"

	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

// scope is the project boundary of every test store.
var scope = projectconfig.Scope{ProjectID: "google-sheets-trigger-tests", Kind: "live"}

// Store is one project's Trigger inbox and poll cursor storage for a test. It outlives the runners that use
// it, so a restarted runner finds what an earlier runner left behind. It is safe for concurrent use.
type Store struct {
	objects *objectStore
	mutex   sync.Mutex
	keys    []projectconfig.TriggerInboxKey
}

// New returns empty project storage.
func New() *Store {
	return &Store{objects: &objectStore{versions: map[string][]projectconfig.Object{}}}
}

// Inbox binds the inbox of one Trigger binding, as projectconfig.LoadedProject.TriggerInbox does.
func (store *Store) Inbox(key projectconfig.TriggerInboxKey) (*projectconfig.TriggerInbox, error) {
	inbox, err := projectconfig.NewTriggerInbox(store.objects, scope, key)
	if err != nil {
		return nil, err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if !slices.Contains(store.keys, key) {
		store.keys = append(store.keys, key)
	}
	return inbox, nil
}

// Cursor binds the poll cursor of one Trigger binding, as projectconfig.LoadedProject.PollCursorStore does.
func (store *Store) Cursor(key projectconfig.PollCursorKey) (*projectconfig.PollCursorStore, error) {
	return projectconfig.NewPollCursorStore(store.objects, scope, key)
}

// PendingEventIDs lists the events pending in every inbox opened so far.
func (store *Store) PendingEventIDs(ctx context.Context) ([]string, error) {
	store.mutex.Lock()
	keys := slices.Clone(store.keys)
	store.mutex.Unlock()
	eventIDs := []string{}
	for _, key := range keys {
		inbox, err := projectconfig.NewTriggerInbox(store.objects, scope, key)
		if err != nil {
			return nil, err
		}
		pending, err := inbox.Pending(ctx)
		if err != nil {
			return nil, err
		}
		for _, event := range pending {
			eventIDs = append(eventIDs, event.ID)
		}
	}
	return eventIDs, nil
}

// objectStore is an in-memory projectconfig.ObjectStore with immutable versions and compare-and-swap.
type objectStore struct {
	mutex    sync.Mutex
	versions map[string][]projectconfig.Object
	next     int
}

// ReadObject reads an exact version, or the current version when version is empty.
func (store *objectStore) ReadObject(_ context.Context, key, version string) (projectconfig.Object, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	versions := store.versions[key]
	if len(versions) == 0 {
		return projectconfig.Object{}, projectconfig.ErrObjectNotFound
	}
	if version == "" {
		return copyObject(versions[len(versions)-1]), nil
	}
	for _, object := range versions {
		if object.Version == version {
			return copyObject(object), nil
		}
	}
	return projectconfig.Object{}, projectconfig.ErrObjectNotFound
}

// CreateObject creates a previously absent key.
func (store *objectStore) CreateObject(_ context.Context, key string, contents []byte) (projectconfig.Object, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if len(store.versions[key]) > 0 {
		return projectconfig.Object{}, projectconfig.ErrConflict
	}
	return store.put(key, contents), nil
}

// CompareAndSwapObject replaces a key only when its current ETag equals expectedETag.
func (store *objectStore) CompareAndSwapObject(_ context.Context, key, expectedETag string, contents []byte) (projectconfig.Object, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	versions := store.versions[key]
	if len(versions) == 0 || versions[len(versions)-1].ETag != expectedETag {
		return projectconfig.Object{}, projectconfig.ErrConflict
	}
	return store.put(key, contents), nil
}

func (store *objectStore) put(key string, contents []byte) projectconfig.Object {
	store.next++
	object := projectconfig.Object{
		Key: key, Version: "v" + strconv.Itoa(store.next), ETag: "e" + strconv.Itoa(store.next), Contents: slices.Clone(contents),
	}
	store.versions[key] = append(store.versions[key], object)
	return copyObject(object)
}

func copyObject(object projectconfig.Object) projectconfig.Object {
	object.Contents = slices.Clone(object.Contents)
	return object
}
