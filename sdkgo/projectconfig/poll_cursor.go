// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package projectconfig

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
)

const (
	pollCursorSchema = "connectors.dex.dev/project-poll-cursor/v1alpha1"
	// MaxPollCursorTokenBytes bounds PollCursorState.Token.
	MaxPollCursorTokenBytes = 64 << 10
	// MaxKeyedSnapshotEntries is the hard bound on entries in one stored keyed snapshot.
	MaxKeyedSnapshotEntries = 100_000
	// maxKeyedSnapshotKeyBytes bounds one KeyedSnapshotEntry.Key.
	maxKeyedSnapshotKeyBytes = 1 << 10
)

// ErrPollCursorTooLarge reports a poll cursor state that exceeds a storage bound: a token over
// MaxPollCursorTokenBytes, a snapshot over its entry bound, or an encoded document over 8 MiB. No retry
// of the same state can succeed; the connector or its configuration must produce a smaller state.
var ErrPollCursorTooLarge = errors.New("project poll cursor state exceeds its bound")

// ErrPollCursorDocumentInvalid reports a stored poll cursor document that fails strict decoding or has
// another schema version. The store never overwrites such a document; an operator must inspect it.
var ErrPollCursorDocumentInvalid = errors.New("project poll cursor document is invalid")

// PollCursorKey identifies one Trigger binding's poll cursor. It has the same four identity parts as
// TriggerInboxKey, so a binding's inbox key converts with PollCursorKey(inboxKey).
type PollCursorKey TriggerInboxKey

// PollFeedKey identifies one poll cursor shared by several Trigger bindings of one connection, such as
// one provider change feed that serves several Triggers.
type PollFeedKey struct {
	// ConnectorID is the manifest's connector identifier.
	ConnectorID string
	// ConnectionName is the application-declared connection the feed reads.
	ConnectionName string
	// FeedName is the connector's stable lower camel case feed name, such as "driveChanges".
	FeedName string
}

// PollCursorState is a committed provider position. Every field is part of the stored document
// contract "connectors.dex.dev/project-poll-cursor/v1alpha1" and keeps this field order.
type PollCursorState struct {
	// Token is the connector's opaque encoded position, at most MaxPollCursorTokenBytes. Empty is valid.
	Token string `json:"token"`
	// Snapshot is the optional bounded keyed snapshot committed atomically with Token; nil when unused.
	Snapshot *KeyedSnapshot `json:"snapshot"`
	// SeededAtUnixMs is the time of the latest seed, in milliseconds since the Unix epoch.
	SeededAtUnixMs int64 `json:"seededAtUnixMs"`
	// CommittedAtUnixMs is the time of this commit, in milliseconds since the Unix epoch.
	CommittedAtUnixMs int64 `json:"committedAtUnixMs"`
	// CommitCount counts every commit, including seeds. It is diagnostic and makes each commit's bytes
	// unique, so an unknown write outcome can be reconciled by comparing bytes.
	CommitCount int64 `json:"commitCount"`
	// ResetCount counts reseeds after the provider rejected the cursor permanently.
	ResetCount int64 `json:"resetCount"`
	// LastResetAtUnixMs is the time of the latest reseed in milliseconds, or nil before the first.
	LastResetAtUnixMs *int64 `json:"lastResetAtUnixMs"`
}

// KeyedSnapshot is a bounded set of tracked provider items stored with a poll cursor. Entries are
// sorted ascending by Key in byte order and keys are unique.
type KeyedSnapshot struct {
	// Entries are the tracked items, sorted by Key.
	Entries []KeyedSnapshotEntry `json:"entries"`
}

// KeyedSnapshotEntry is one tracked provider item.
type KeyedSnapshotEntry struct {
	// Key is the connector's stable item key, at most 1 KiB.
	Key string `json:"key"`
	// Hash is the item's content hash: lowercase hex, 16 to 64 characters.
	Hash string `json:"hash"`
	// Seen is the CommitCount of the commit that last added, changed, or reported this entry. Zero in a
	// state being committed means "this commit"; the poll Trigger kit replaces it before writing.
	Seen int64 `json:"seen"`
	// Value is the item's optional JSON value, kept only when a poller stores values. Absent, never null.
	Value json.RawMessage `json:"value,omitempty"`
}

// PollCursorRecord is one read of a poll cursor head.
type PollCursorRecord struct {
	// State is the committed position, or nil until the cursor is seeded.
	State *PollCursorState
	// Revision is the opaque compare-and-swap token for ReplacePollCursor.
	Revision string
}

type pollCursorDocument struct {
	SchemaVersion string           `json:"schemaVersion"`
	State         *PollCursorState `json:"state"`
}

// PollCursorStore reads and conditionally replaces one poll cursor head in project storage, beside the
// Trigger inboxes. It is safe for concurrent use; concurrent replicas are ordered by compare-and-swap.
type PollCursorStore struct {
	objects ObjectStore
	key     string
	// headExists records that the head was created or found, so no later read asks for a missing key.
	headExists atomic.Bool
}

// NewPollCursorStore binds one Trigger binding's poll cursor in scope without reading or writing objects.
// The key is "<scope prefix>/poll-cursors/<connectorId>/<b64url(connection)>/<trigger>/<b64url(binding)>/head".
func NewPollCursorStore(objects ObjectStore, scope Scope, key PollCursorKey) (*PollCursorStore, error) {
	prefix, err := pollCursorPrefix(objects, scope, key.ConnectorID, key.TriggerName, key.ConnectionName, key.BindingName)
	if err != nil {
		return nil, err
	}
	encode := base64.RawURLEncoding.EncodeToString
	path := prefix + "/poll-cursors/" + key.ConnectorID + "/" + encode([]byte(key.ConnectionName)) + "/" +
		key.TriggerName + "/" + encode([]byte(key.BindingName)) + "/head"
	return &PollCursorStore{objects: objects, key: path}, nil
}

// NewPollFeedCursorStore binds one shared feed's poll cursor in scope without reading or writing objects.
// The key is "<scope prefix>/poll-feeds/<connectorId>/<b64url(connection)>/<feed>/head".
func NewPollFeedCursorStore(objects ObjectStore, scope Scope, key PollFeedKey) (*PollCursorStore, error) {
	prefix, err := pollCursorPrefix(objects, scope, key.ConnectorID, key.FeedName, key.ConnectionName)
	if err != nil {
		return nil, err
	}
	path := prefix + "/poll-feeds/" + key.ConnectorID + "/" +
		base64.RawURLEncoding.EncodeToString([]byte(key.ConnectionName)) + "/" + key.FeedName + "/head"
	return &PollCursorStore{objects: objects, key: path}, nil
}

func pollCursorPrefix(objects ObjectStore, scope Scope, connectorID, triggerOrFeedName string, names ...string) (string, error) {
	if objects == nil {
		return "", errors.New("poll cursor object store is required")
	}
	prefix, err := scope.Prefix()
	if err != nil {
		return "", err
	}
	if !regexpConnectorID(connectorID) || !triggerNamePattern.MatchString(triggerOrFeedName) {
		return "", errors.New("poll cursor connector, trigger, or feed identity is invalid")
	}
	for _, name := range names {
		if strings.TrimSpace(name) != name || name == "" || len(name) > 256 || strings.ContainsAny(name, "\x00\r\n") {
			return "", errors.New("poll cursor connection or binding name is invalid")
		}
	}
	return prefix, nil
}

// ReadPollCursor returns the current cursor record. Before its first read the store creates the unseeded
// head {"schemaVersion":...,"state":null} with a create-only write, so it never reads a missing key and
// a restart learns whether the cursor was seeded without a provider call. It returns an error wrapping
// ErrPollCursorDocumentInvalid when the stored document fails strict decoding.
func (store *PollCursorStore) ReadPollCursor(ctx context.Context) (PollCursorRecord, error) {
	if err := store.createHead(ctx); err != nil {
		return PollCursorRecord{}, err
	}
	object, err := store.objects.ReadObject(ctx, store.key, "")
	if errors.Is(err, ErrObjectNotFound) {
		return PollCursorRecord{}, nil
	}
	if err != nil {
		return PollCursorRecord{}, err
	}
	state, err := decodePollCursorDocument(object.Contents)
	if err != nil {
		return PollCursorRecord{}, err
	}
	if object.ETag == "" {
		return PollCursorRecord{}, fmt.Errorf("%w: missing revision", ErrPollCursorDocumentInvalid)
	}
	store.headExists.Store(true)
	return PollCursorRecord{State: state, Revision: object.ETag}, nil
}

// ReplacePollCursor writes state only when the head is still at expectedRevision, the Revision of the
// read the caller's decision was based on. It returns ErrConflict when another writer changed the head
// first, and an error wrapping ErrPollCursorTooLarge for a state over a storage bound. An unknown write
// outcome is reconciled by reading the head back and comparing bytes; a mismatch is ErrConflict. It
// never retries with another precondition: the caller re-reads and decides again.
func (store *PollCursorStore) ReplacePollCursor(ctx context.Context, expectedRevision string, state PollCursorState) (PollCursorRecord, error) {
	contents, err := encodePollCursorDocument(&state)
	if err != nil {
		return PollCursorRecord{}, err
	}
	var written Object
	if expectedRevision == "" {
		written, err = store.objects.CreateObject(ctx, store.key, contents)
	} else {
		written, err = store.objects.CompareAndSwapObject(ctx, store.key, expectedRevision, contents)
	}
	if errors.Is(err, ErrOutcomeUnknown) {
		observed, readErr := store.objects.ReadObject(ctx, store.key, "")
		if readErr != nil {
			return PollCursorRecord{}, err
		}
		if !bytes.Equal(observed.Contents, contents) {
			return PollCursorRecord{}, ErrConflict
		}
		written, err = observed, nil
	}
	if err != nil {
		return PollCursorRecord{}, err
	}
	store.headExists.Store(true)
	return PollCursorRecord{State: &state, Revision: written.ETag}, nil
}

// createHead creates the unseeded head before the first read; a conflict means it already exists.
func (store *PollCursorStore) createHead(ctx context.Context) error {
	if store.headExists.Load() {
		return nil
	}
	contents, err := encodePollCursorDocument(nil)
	if err != nil {
		return err
	}
	_, err = store.objects.CreateObject(ctx, store.key, contents)
	switch {
	case err == nil, errors.Is(err, ErrConflict):
		store.headExists.Store(true)
		return nil
	case errors.Is(err, ErrOutcomeUnknown):
		return nil
	default:
		return err
	}
}

func encodePollCursorDocument(state *PollCursorState) ([]byte, error) {
	if state != nil {
		if err := ValidatePollCursorState(*state); err != nil {
			return nil, err
		}
		if state.Snapshot != nil && state.Snapshot.Entries == nil {
			encoded := *state
			encoded.Snapshot = &KeyedSnapshot{Entries: []KeyedSnapshotEntry{}}
			state = &encoded
		}
	}
	contents, err := json.Marshal(pollCursorDocument{SchemaVersion: pollCursorSchema, State: state})
	if err != nil {
		return nil, errors.New("poll cursor encoding failed")
	}
	if len(contents) > maxTriggerInboxBytes {
		return nil, fmt.Errorf("%w: encoded document is %d bytes", ErrPollCursorTooLarge, len(contents))
	}
	return contents, nil
}

func decodePollCursorDocument(contents []byte) (*PollCursorState, error) {
	var document pollCursorDocument
	if strictJSON(contents, &document) != nil || document.SchemaVersion != pollCursorSchema {
		return nil, ErrPollCursorDocumentInvalid
	}
	if document.State != nil {
		if err := ValidatePollCursorState(*document.State); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrPollCursorDocumentInvalid, err)
		}
	}
	return document.State, nil
}

// ValidatePollCursorState checks the storage bounds and snapshot ordering of a state. It returns an
// error wrapping ErrPollCursorTooLarge for an oversize token or snapshot, and another error for an
// unsorted or duplicate snapshot key, a malformed hash, a null value, or a negative counter.
func ValidatePollCursorState(state PollCursorState) error {
	if len(state.Token) > MaxPollCursorTokenBytes {
		return fmt.Errorf("%w: token is %d bytes", ErrPollCursorTooLarge, len(state.Token))
	}
	if state.CommitCount < 0 || state.ResetCount < 0 {
		return errors.New("poll cursor counters must not be negative")
	}
	if state.Snapshot == nil {
		return nil
	}
	return ValidateKeyedSnapshot(*state.Snapshot)
}

// ValidateKeyedSnapshot checks that entries are sorted by unique key, keys are at most 1 KiB, hashes are
// lowercase hex of 16 to 64 characters, values are JSON other than null, and at most
// MaxKeyedSnapshotEntries entries exist (ErrPollCursorTooLarge otherwise).
func ValidateKeyedSnapshot(snapshot KeyedSnapshot) error {
	if len(snapshot.Entries) > MaxKeyedSnapshotEntries {
		return fmt.Errorf("%w: snapshot has %d entries", ErrPollCursorTooLarge, len(snapshot.Entries))
	}
	for index, entry := range snapshot.Entries {
		if entry.Key == "" || len(entry.Key) > maxKeyedSnapshotKeyBytes {
			return errors.New("keyed snapshot key must be 1 byte to 1 KiB")
		}
		if index > 0 && snapshot.Entries[index-1].Key >= entry.Key {
			return errors.New("keyed snapshot entries must be sorted by unique key")
		}
		if !isKeyedSnapshotHash(entry.Hash) {
			return errors.New("keyed snapshot hash must be lowercase hex of 16 to 64 characters")
		}
		if entry.Seen < 0 {
			return errors.New("keyed snapshot seen counter must not be negative")
		}
		if entry.Value != nil && (!json.Valid(entry.Value) || string(bytes.TrimSpace(entry.Value)) == "null") {
			return errors.New("keyed snapshot value must be JSON other than null")
		}
	}
	return nil
}

func isKeyedSnapshotHash(hash string) bool {
	if len(hash) < 16 || len(hash) > 64 {
		return false
	}
	return strings.Trim(hash, "0123456789abcdef") == ""
}
