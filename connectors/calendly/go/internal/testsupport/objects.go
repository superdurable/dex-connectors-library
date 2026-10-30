// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package testsupport

import (
	"context"
	"strconv"
	"sync"

	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

// ObjectStore is an in-memory projectconfig.ObjectStore with immutable versions and compare-and-swap, as
// the Connector SDK's own Trigger delivery tests use. It serves tests whose subject is above storage, such
// as durable Trigger inboxes; the SDK's S3 integration suite covers real storage semantics.
type ObjectStore struct {
	mutex    sync.Mutex
	versions map[string][]projectconfig.Object
	next     int
}

// NewObjectStore returns an empty store that is safe for concurrent use.
func NewObjectStore() *ObjectStore {
	return &ObjectStore{versions: map[string][]projectconfig.Object{}}
}

// ReadObject reads an exact version, or the current version when version is empty.
func (store *ObjectStore) ReadObject(_ context.Context, key, version string) (projectconfig.Object, error) {
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
func (store *ObjectStore) CreateObject(_ context.Context, key string, contents []byte) (projectconfig.Object, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if len(store.versions[key]) > 0 {
		return projectconfig.Object{}, projectconfig.ErrConflict
	}
	return store.put(key, contents), nil
}

// CompareAndSwapObject replaces a key only when its current ETag equals expectedETag.
func (store *ObjectStore) CompareAndSwapObject(_ context.Context, key, expectedETag string, contents []byte) (projectconfig.Object, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	versions := store.versions[key]
	if len(versions) == 0 || versions[len(versions)-1].ETag != expectedETag {
		return projectconfig.Object{}, projectconfig.ErrConflict
	}
	return store.put(key, contents), nil
}

func (store *ObjectStore) put(key string, contents []byte) projectconfig.Object {
	store.next++
	object := projectconfig.Object{Key: key, Version: "v" + strconv.Itoa(store.next), ETag: "e" + strconv.Itoa(store.next), Contents: append([]byte(nil), contents...)}
	store.versions[key] = append(store.versions[key], object)
	return copyObject(object)
}

func copyObject(object projectconfig.Object) projectconfig.Object {
	object.Contents = append([]byte(nil), object.Contents...)
	return object
}
