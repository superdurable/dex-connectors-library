// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package projectconfig

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// versionedStore is an ObjectStore that keeps every version in memory and, when dir is set, also as private files,
// so a restarted application finds them again. It serves one local application process.
type versionedStore struct {
	mu      sync.Mutex
	dir     string
	objects map[string][]Object
}

// storedObject is the file form of one object version.
type storedObject struct {
	Key      string `json:"key"`
	Version  string `json:"version"`
	ETag     string `json:"etag"`
	Contents []byte `json:"contents"`
}

// newVersionedStore returns an empty in-memory store, or one that persists to and first loads from dir.
func newVersionedStore(dir string) (*versionedStore, error) {
	store := &versionedStore{dir: dir, objects: map[string][]Object{}}
	if dir == "" {
		return store, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create local project storage directory: %w", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read local project storage directory: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		contents, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read local project storage file: %w", err)
		}
		var stored storedObject
		if err := json.Unmarshal(contents, &stored); err != nil {
			return nil, fmt.Errorf("decode local project storage file %q: %w", entry.Name(), err)
		}
		store.objects[stored.Key] = append(store.objects[stored.Key], Object{Key: stored.Key, Version: stored.Version, ETag: stored.ETag, Contents: stored.Contents})
	}
	for key, versions := range store.objects {
		sort.Slice(versions, func(i, j int) bool { return versionNumber(versions[i].Version) < versionNumber(versions[j].Version) })
		if !keepsHistory(key) {
			// A crash between writing a new version and deleting the old one leaves both.
			for _, stale := range versions[:len(versions)-1] {
				store.remove(stale)
			}
			store.objects[key] = versions[len(versions)-1:]
		}
	}
	return store, nil
}

// keepsHistory reports whether readers address earlier versions of key. Only the pinned configuration snapshot is
// read by exact version after it is superseded; every other key is read at its current version.
func keepsHistory(key string) bool {
	return strings.HasSuffix(key, "/configuration/head")
}

func versionNumber(version string) int {
	number, _ := strconv.Atoi(version)
	return number
}

// ReadObject reads an exact version, or the current version when version is empty.
func (store *versionedStore) ReadObject(_ context.Context, key, version string) (Object, error) {
	if err := validateObjectKey(key); err != nil {
		return Object{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	versions := store.objects[key]
	if len(versions) == 0 {
		return Object{}, ErrObjectNotFound
	}
	if version == "" {
		return copyObject(versions[len(versions)-1]), nil
	}
	for _, object := range versions {
		if object.Version == version {
			return copyObject(object), nil
		}
	}
	return Object{}, ErrObjectNotFound
}

// CreateObject creates a previously absent key and returns ErrConflict if it exists.
func (store *versionedStore) CreateObject(_ context.Context, key string, contents []byte) (Object, error) {
	if err := validateObjectKey(key); err != nil {
		return Object{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.objects[key]) != 0 {
		return Object{}, ErrConflict
	}
	return store.appendVersion(key, contents)
}

// CompareAndSwapObject replaces a key only when its current ETag equals expectedETag.
func (store *versionedStore) CompareAndSwapObject(_ context.Context, key, expectedETag string, contents []byte) (Object, error) {
	if err := validateObjectKey(key); err != nil {
		return Object{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	versions := store.objects[key]
	if len(versions) == 0 {
		return Object{}, ErrObjectNotFound
	}
	if versions[len(versions)-1].ETag != expectedETag {
		return Object{}, ErrConflict
	}
	return store.appendVersion(key, contents)
}

// appendVersion records the next version of key. The caller holds the lock.
func (store *versionedStore) appendVersion(key string, contents []byte) (Object, error) {
	previous := store.objects[key]
	version := "1"
	if len(previous) > 0 {
		version = strconv.Itoa(versionNumber(previous[len(previous)-1].Version) + 1)
	}
	object := Object{Key: key, Version: version, ETag: digestBytes(contents) + "-" + version, Contents: append([]byte(nil), contents...)}
	if store.dir != "" {
		if err := store.persist(object); err != nil {
			return Object{}, err
		}
	}
	if keepsHistory(key) {
		store.objects[key] = append(previous, object)
		return copyObject(object), nil
	}
	// The new version is durable before the old file goes, so a crash never loses the key.
	for _, superseded := range previous {
		store.remove(superseded)
	}
	store.objects[key] = []Object{object}
	return copyObject(object), nil
}

// remove deletes the file of one superseded version. A failure leaves a stale file that the next load discards.
func (store *versionedStore) remove(object Object) {
	if store.dir != "" {
		_ = os.Remove(store.file(object))
	}
}

// persist writes one version through a same-directory temporary file, so a reader never sees a partial file.
func (store *versionedStore) persist(object Object) error {
	contents, err := json.Marshal(storedObject{Key: object.Key, Version: object.Version, ETag: object.ETag, Contents: object.Contents})
	if err != nil {
		return errors.New("local project storage encoding failed")
	}
	return writePrivateFile(store.file(object), contents)
}

func (store *versionedStore) file(object Object) string {
	sum := sha256.Sum256([]byte(object.Key))
	return filepath.Join(store.dir, hex.EncodeToString(sum[:])+"-"+object.Version+".json")
}

func copyObject(object Object) Object {
	object.Contents = append([]byte(nil), object.Contents...)
	return object
}

// verifyFileReplacement creates and removes the temporary file that writePrivateFile would create beside path.
func verifyFileReplacement(path string) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".write-*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	closeErr := temporary.Close()
	return errors.Join(closeErr, os.Remove(temporary.Name()))
}

// writePrivateFile atomically replaces path with a regular file readable only by its owner.
// Synchronizing the contents and the rename leaves the prior or new file complete after a crash.
func writePrivateFile(path string, contents []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".write-*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err = temporary.Chmod(0o600); err == nil {
		_, err = temporary.Write(contents)
	}
	if err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write temporary file: %w", err)
	}
	if err = os.Rename(name, path); err != nil {
		return fmt.Errorf("replace file: %w", err)
	}
	if err = syncDirectory(filepath.Dir(path)); err != nil {
		return fmt.Errorf("synchronize directory: %w", err)
	}
	return nil
}
