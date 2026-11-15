// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package projectconfig_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

var pollScope = projectconfig.Scope{ProjectID: "sample-project", Kind: "preview"}

var pollKey = projectconfig.PollCursorKey{ConnectorID: "sample", ConnectionName: "main-é-流程 😀", TriggerName: "itemCreated", BindingName: "main / west"}

const (
	goldenPollCursorHeadKey  = "projects/sample-project/preview/poll-cursors/sample/bWFpbi3DqS3mtYHnqIsg8J-YgA/itemCreated/bWFpbiAvIHdlc3Q/head"
	goldenPollFeedHeadKey    = "projects/sample-project/preview/poll-feeds/sample/bWFpbi3DqS3mtYHnqIsg8J-YgA/itemChanges/head"
	goldenUnseededDocument   = `{"schemaVersion":"connectors.dex.dev/project-poll-cursor/v1alpha1","state":null}`
	goldenPollCursorDocument = `{"schemaVersion":"connectors.dex.dev/project-poll-cursor/v1alpha1","state":{"token":"row \u003c7\u003e \u0026 \u2028end","snapshot":{"entries":[{"key":"row-1","hash":"9f86d081884c7d65","seen":12,"value":{"Name":"Ada"}},{"key":"row-2","hash":"0123456789abcdef","seen":3}]},"seededAtUnixMs":1763164800000,"committedAtUnixMs":1763165400000,"commitCount":12,"resetCount":0,"lastResetAtUnixMs":null}}`
)

// keyRecordingStore records every key a store writes, and refuses reads of missing keys as S3 does.
type keyRecordingStore struct {
	*testsupport.ObjectStore
	createdKeys      []string
	missingKeyReads  int
	nextCreateResult error
}

func (store *keyRecordingStore) CreateObject(ctx context.Context, key string, contents []byte) (projectconfig.Object, error) {
	store.createdKeys = append(store.createdKeys, key)
	if err := store.nextCreateResult; err != nil {
		store.nextCreateResult = nil
		return projectconfig.Object{}, err
	}
	return store.ObjectStore.CreateObject(ctx, key, contents)
}

func (store *keyRecordingStore) ReadObject(ctx context.Context, key, version string) (projectconfig.Object, error) {
	object, err := store.ObjectStore.ReadObject(ctx, key, version)
	if errors.Is(err, projectconfig.ErrObjectNotFound) {
		store.missingKeyReads++
		return projectconfig.Object{}, errors.New("access denied")
	}
	return object, err
}

func goldenPollState() projectconfig.PollCursorState {
	return projectconfig.PollCursorState{
		Token: "row <7> & \u2028end",
		Snapshot: &projectconfig.KeyedSnapshot{Entries: []projectconfig.KeyedSnapshotEntry{
			{Key: "row-1", Hash: "9f86d081884c7d65", Seen: 12, Value: json.RawMessage(`{ "Name": "Ada" }`)},
			{Key: "row-2", Hash: "0123456789abcdef", Seen: 3},
		}},
		SeededAtUnixMs: 1763164800000, CommittedAtUnixMs: 1763165400000, CommitCount: 12,
	}
}

func TestPollCursorStoreKeysAndDocumentsMatchTheStorageContract(t *testing.T) {
	ctx := context.Background()
	objects := &keyRecordingStore{ObjectStore: testsupport.NewObjectStore()}
	store, err := projectconfig.NewPollCursorStore(objects, pollScope, pollKey)
	require.NoError(t, err)
	record, err := store.ReadPollCursor(ctx)
	require.NoError(t, err)
	require.Nil(t, record.State)
	require.Equal(t, []string{goldenPollCursorHeadKey}, objects.createdKeys)
	head, err := objects.ReadObject(ctx, goldenPollCursorHeadKey, "")
	require.NoError(t, err)
	require.Equal(t, goldenUnseededDocument, string(head.Contents))

	written, err := store.ReplacePollCursor(ctx, record.Revision, goldenPollState())
	require.NoError(t, err)
	head, err = objects.ReadObject(ctx, goldenPollCursorHeadKey, "")
	require.NoError(t, err)
	require.Equal(t, goldenPollCursorDocument, string(head.Contents))
	reread, err := store.ReadPollCursor(ctx)
	require.NoError(t, err)
	require.Equal(t, written.Revision, reread.Revision)
	require.Equal(t, "row <7> & \u2028end", reread.State.Token)
	require.JSONEq(t, `{"Name":"Ada"}`, string(reread.State.Snapshot.Entries[0].Value))
	require.Zero(t, objects.missingKeyReads)

	feed, err := projectconfig.NewPollFeedCursorStore(objects, pollScope, projectconfig.PollFeedKey{
		ConnectorID: "sample", ConnectionName: pollKey.ConnectionName, FeedName: "itemChanges",
	})
	require.NoError(t, err)
	_, err = feed.ReadPollCursor(ctx)
	require.NoError(t, err)
	require.Equal(t, goldenPollFeedHeadKey, objects.createdKeys[len(objects.createdKeys)-1])
}

func TestPollCursorStoreSharesAnExistingHeadAndDetectsConflicts(t *testing.T) {
	ctx := context.Background()
	objects := testsupport.NewObjectStore()
	first, err := projectconfig.NewPollCursorStore(objects, pollScope, pollKey)
	require.NoError(t, err)
	second, err := projectconfig.NewPollCursorStore(objects, pollScope, pollKey)
	require.NoError(t, err)
	firstRead, err := first.ReadPollCursor(ctx)
	require.NoError(t, err)
	secondRead, err := second.ReadPollCursor(ctx)
	require.NoError(t, err)
	require.Equal(t, firstRead.Revision, secondRead.Revision)

	_, err = first.ReplacePollCursor(ctx, firstRead.Revision, projectconfig.PollCursorState{Token: "a", CommitCount: 1})
	require.NoError(t, err)
	_, err = second.ReplacePollCursor(ctx, secondRead.Revision, projectconfig.PollCursorState{Token: "b", CommitCount: 1})
	require.ErrorIs(t, err, projectconfig.ErrConflict)
	current, err := second.ReadPollCursor(ctx)
	require.NoError(t, err)
	require.Equal(t, "a", current.State.Token)
}

// outcomeUnknownStore applies or drops one compare-and-swap and then reports an unknown outcome.
type outcomeUnknownStore struct {
	*testsupport.ObjectStore
	shouldApply bool
}

func (store *outcomeUnknownStore) CompareAndSwapObject(ctx context.Context, key, expectedETag string, contents []byte) (projectconfig.Object, error) {
	if store.shouldApply {
		if _, err := store.ObjectStore.CompareAndSwapObject(ctx, key, expectedETag, contents); err != nil {
			return projectconfig.Object{}, err
		}
	}
	return projectconfig.Object{}, projectconfig.ErrOutcomeUnknown
}

func TestPollCursorStoreReconcilesUnknownWriteOutcomes(t *testing.T) {
	ctx := context.Background()
	for _, scenario := range []struct {
		name        string
		shouldApply bool
		expectedErr error
	}{{"applied write succeeds", true, nil}, {"lost write is a conflict", false, projectconfig.ErrConflict}} {
		t.Run(scenario.name, func(t *testing.T) {
			objects := &outcomeUnknownStore{ObjectStore: testsupport.NewObjectStore(), shouldApply: scenario.shouldApply}
			store, err := projectconfig.NewPollCursorStore(objects, pollScope, pollKey)
			require.NoError(t, err)
			record, err := store.ReadPollCursor(ctx)
			require.NoError(t, err)
			written, err := store.ReplacePollCursor(ctx, record.Revision, projectconfig.PollCursorState{Token: "x", CommitCount: 1})
			if scenario.expectedErr != nil {
				require.ErrorIs(t, err, scenario.expectedErr)
				return
			}
			require.NoError(t, err)
			reread, err := store.ReadPollCursor(ctx)
			require.NoError(t, err)
			require.Equal(t, written.Revision, reread.Revision)
		})
	}
}

func TestPollCursorStoreRejectsOversizeAndMalformedState(t *testing.T) {
	ctx := context.Background()
	store, err := projectconfig.NewPollCursorStore(testsupport.NewObjectStore(), pollScope, pollKey)
	require.NoError(t, err)
	record, err := store.ReadPollCursor(ctx)
	require.NoError(t, err)
	entries := func(keys ...string) *projectconfig.KeyedSnapshot {
		snapshot := &projectconfig.KeyedSnapshot{}
		for _, key := range keys {
			snapshot.Entries = append(snapshot.Entries, projectconfig.KeyedSnapshotEntry{Key: key, Hash: "0123456789abcdef"})
		}
		return snapshot
	}
	tooLarge := []projectconfig.PollCursorState{
		{Token: strings.Repeat("t", projectconfig.MaxPollCursorTokenBytes+1)},
		{Snapshot: &projectconfig.KeyedSnapshot{Entries: make([]projectconfig.KeyedSnapshotEntry, projectconfig.MaxKeyedSnapshotEntries+1)}},
		{Snapshot: &projectconfig.KeyedSnapshot{Entries: []projectconfig.KeyedSnapshotEntry{
			{Key: "a", Hash: "0123456789abcdef", Value: json.RawMessage(`"` + strings.Repeat("v", 9<<20) + `"`)},
		}}},
	}
	for _, state := range tooLarge {
		_, err := store.ReplacePollCursor(ctx, record.Revision, state)
		require.ErrorIs(t, err, projectconfig.ErrPollCursorTooLarge)
	}
	malformed := []projectconfig.PollCursorState{
		{Snapshot: entries("b", "a")},
		{Snapshot: entries("a", "a")},
		{Snapshot: &projectconfig.KeyedSnapshot{Entries: []projectconfig.KeyedSnapshotEntry{{Key: "a", Hash: "ABCDEF0123456789"}}}},
		{Snapshot: &projectconfig.KeyedSnapshot{Entries: []projectconfig.KeyedSnapshotEntry{{Key: "a", Hash: "0123456789abcdef", Value: json.RawMessage(`null`)}}}},
		{CommitCount: -1},
	}
	for _, state := range malformed {
		_, err := store.ReplacePollCursor(ctx, record.Revision, state)
		require.Error(t, err)
		require.NotErrorIs(t, err, projectconfig.ErrPollCursorTooLarge)
	}
	written, err := store.ReplacePollCursor(ctx, record.Revision, projectconfig.PollCursorState{Snapshot: &projectconfig.KeyedSnapshot{}})
	require.NoError(t, err)
	require.NotEmpty(t, written.Revision)
}

func TestPollCursorStoreRejectsInvalidStoredDocuments(t *testing.T) {
	ctx := context.Background()
	for _, contents := range []string{
		`{"schemaVersion":"connectors.dex.dev/project-poll-cursor/v1alpha1","state":null,"extra":1}`,
		`{"schemaVersion":"connectors.dex.dev/project-poll-cursor/v2","state":null}`,
		`{"schemaVersion":"connectors.dex.dev/project-poll-cursor/v1alpha1","state":null} {}`,
		`{"schemaVersion":"connectors.dex.dev/project-poll-cursor/v1alpha1","state":{"token":"","snapshot":{"entries":[{"key":"b","hash":"0123456789abcdef","seen":0},{"key":"a","hash":"0123456789abcdef","seen":0}]},"seededAtUnixMs":0,"committedAtUnixMs":0,"commitCount":0,"resetCount":0,"lastResetAtUnixMs":null}}`,
	} {
		objects := testsupport.NewObjectStore()
		_, err := objects.CreateObject(ctx, goldenPollCursorHeadKey, []byte(contents))
		require.NoError(t, err)
		store, err := projectconfig.NewPollCursorStore(objects, pollScope, pollKey)
		require.NoError(t, err)
		_, err = store.ReadPollCursor(ctx)
		require.ErrorIs(t, err, projectconfig.ErrPollCursorDocumentInvalid, contents)
	}
}

func TestPollCursorStoreRejectsInvalidIdentity(t *testing.T) {
	objects := testsupport.NewObjectStore()
	for _, key := range []projectconfig.PollCursorKey{
		{ConnectorID: "Sample", ConnectionName: "main", TriggerName: "itemCreated", BindingName: "main"},
		{ConnectorID: "sample", ConnectionName: " main", TriggerName: "itemCreated", BindingName: "main"},
		{ConnectorID: "sample", ConnectionName: "main", TriggerName: "item-created", BindingName: "main"},
		{ConnectorID: "sample", ConnectionName: "main", TriggerName: "itemCreated", BindingName: "line\nbreak"},
	} {
		_, err := projectconfig.NewPollCursorStore(objects, pollScope, key)
		require.Error(t, err, key)
	}
	_, err := projectconfig.NewPollFeedCursorStore(objects, pollScope, projectconfig.PollFeedKey{ConnectorID: "sample", ConnectionName: "main", FeedName: "Feed"})
	require.Error(t, err)
	_, err = projectconfig.NewPollCursorStore(nil, pollScope, pollKey)
	require.Error(t, err)
}
