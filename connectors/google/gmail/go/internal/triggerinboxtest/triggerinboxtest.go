// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package triggerinboxtest keeps the Gmail connector tests' project Trigger inboxes in memory.
package triggerinboxtest

import (
	"context"
	"slices"
	"strconv"
	"sync"

	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

// scope is the project boundary of every test inbox.
var scope = projectconfig.Scope{ProjectID: "gmail-trigger-tests", Kind: "live"}

// Inboxes is one project's Trigger inbox storage for a test. It outlives the runners that use it, so a
// restarted runner finds the events an earlier runner left pending. It is safe for concurrent use.
type Inboxes struct {
	objects *objectStore
	mutex   sync.Mutex
	keys    []projectconfig.TriggerInboxKey
}

// New returns empty inbox storage.
func New() *Inboxes {
	return &Inboxes{objects: &objectStore{versions: map[string][]projectconfig.Object{}}}
}

// Open binds the inbox of one Trigger binding, as projectconfig.LoadedProject.TriggerInbox does.
func (inboxes *Inboxes) Open(key projectconfig.TriggerInboxKey) (*projectconfig.TriggerInbox, error) {
	inbox, err := projectconfig.NewTriggerInbox(inboxes.objects, scope, key)
	if err != nil {
		return nil, err
	}
	inboxes.mutex.Lock()
	defer inboxes.mutex.Unlock()
	if !slices.Contains(inboxes.keys, key) {
		inboxes.keys = append(inboxes.keys, key)
	}
	return inbox, nil
}

// PendingEventIDs lists the events pending in every inbox opened so far.
func (inboxes *Inboxes) PendingEventIDs(ctx context.Context) ([]string, error) {
	inboxes.mutex.Lock()
	keys := slices.Clone(inboxes.keys)
	inboxes.mutex.Unlock()
	eventIDs := []string{}
	for _, key := range keys {
		inbox, err := projectconfig.NewTriggerInbox(inboxes.objects, scope, key)
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
