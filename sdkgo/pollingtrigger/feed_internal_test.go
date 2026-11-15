// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pollingtrigger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

// fileChange is a delta-feed change that the feed snapshot tracks by file ID.
type fileChange struct {
	FileID  string
	Version int
}

func (change fileChange) SnapshotKey() string            { return change.FileID }
func (change fileChange) SnapshotHash() string           { return fmt.Sprintf("%016x", change.Version) }
func (change fileChange) SnapshotValue() json.RawMessage { return nil }

// fileFeedFetcher returns scripted pages and keeps the snapshot with ApplyKeyedSnapshotChanges.
type fileFeedFetcher struct {
	log   *operationLog
	pages [][]fileChange
}

func (fetcher *fileFeedFetcher) SeedCursor(context.Context) (Cursor, error) {
	return Cursor{Token: "0", Snapshot: &projectconfig.KeyedSnapshot{}}, nil
}

func (fetcher *fileFeedFetcher) FetchChanges(_ context.Context, cursor Cursor) (ChangePage[fileChange], error) {
	fetcher.log.add("fetch after %s", cursor.Token)
	index, err := strconv.Atoi(cursor.Token)
	if err != nil || index >= len(fetcher.pages) {
		return ChangePage[fileChange]{Next: cursor}, err
	}
	items := make([]KeyedItem, 0, len(fetcher.pages[index]))
	for _, change := range fetcher.pages[index] {
		items = append(items, change)
	}
	snapshot, err := ApplyKeyedSnapshotChanges(cursor.Snapshot, items, nil, KeyedSnapshotOptions{})
	if err != nil {
		return ChangePage[fileChange]{}, err
	}
	return ChangePage[fileChange]{Changes: fetcher.pages[index], Next: Cursor{Token: strconv.Itoa(index + 1), Snapshot: snapshot}}, nil
}

func selectFileEvents(kind string) FeedEventSelector[fileChange, item] {
	return func(change fileChange, previous *projectconfig.KeyedSnapshotEntry) []sdkgo.TriggerEvent[item] {
		isCreated := previous == nil
		if (kind == "created") != isCreated {
			return nil
		}
		return []sdkgo.TriggerEvent[item]{{ID: change.FileID + "@" + strconv.Itoa(change.Version), Payload: item{Sequence: change.Version}}}
	}
}

func TestFeedPollerSharesOneFetchAndSkipsRecordedMembersOnRetry(t *testing.T) {
	clock := installFakeClock(t, 0.5)
	log := &operationLog{}
	fetcher := &fileFeedFetcher{log: log, pages: [][]fileChange{{{"f1", 1}}, {{"f1", 2}, {"f2", 1}}}}
	created := &durableTarget{recordingTarget: recordingTarget{log: log, name: "created:"}}
	updated := &durableTarget{recordingTarget: recordingTarget{log: log, name: "updated:"},
		prepareErrors: map[string][]error{"f1@2": {errors.New("inbox write failed")}}}
	createdMember, err := NewFeedMember(testDefinition("fileCreated"), testBinding("fileCreated", "created"), selectFileEvents("created"), sdkgo.TriggerTarget[item](created))
	require.NoError(t, err)
	updatedMember, err := NewFeedMember(testDefinition("fileUpdated"), testBinding("fileUpdated", "updated"), selectFileEvents("updated"), sdkgo.TriggerTarget[item](updated))
	require.NoError(t, err)
	store := &loggedCursorStore{CursorStore: NewMemoryCursorStore(), log: log}
	poller, err := NewFeedPoller(FeedPollerConfig[fileChange]{
		Name: "fileChanges", Cursor: store, Fetcher: fetcher, Members: []FeedMember[fileChange]{createdMember, updatedMember},
		Snapshot: &KeyedSnapshotOptions{},
	})
	require.NoError(t, err)
	runner := newTestRunner(t, PollSchedule{}, poller)
	require.ErrorIs(t, runUntil(t, clock, runner, func() bool { return log.count("fetch after 2") == 1 }), context.Canceled)
	require.Equal(t, []string{
		"read", "replay created:", "replay updated:", "read", "replace 0",
		"read", "fetch after 0", "prepare created:f1@1", "replace 1", "handle created:f1@1",
		"read", "fetch after 1", "prepare created:f2@1", "prepare updated:f1@2",
		"read", "fetch after 1", "prepare updated:f1@2", "replace 2", "handle created:f2@1", "handle updated:f1@2",
		"read", "fetch after 2",
	}, log.snapshot())
	state := store.state(t)
	require.Equal(t, []projectconfig.KeyedSnapshotEntry{
		{Key: "f1", Hash: "0000000000000002", Seen: 3}, {Key: "f2", Hash: "0000000000000001", Seen: 3},
	}, state.Snapshot.Entries)
}

type rowItem struct{ key, hash, value string }

func (row rowItem) SnapshotKey() string            { return row.key }
func (row rowItem) SnapshotHash() string           { return row.hash }
func (row rowItem) SnapshotValue() json.RawMessage { return json.RawMessage(row.value) }

func TestDiffKeyedSnapshotReportsAddedChangedAndRemovedKeys(t *testing.T) {
	previous := &projectconfig.KeyedSnapshot{Entries: []projectconfig.KeyedSnapshotEntry{
		{Key: "a", Hash: "000000000000000a", Seen: 3, Value: json.RawMessage(`"old a"`)},
		{Key: "b", Hash: "000000000000000b", Seen: 4},
		{Key: "c", Hash: "000000000000000c", Seen: 5},
	}}
	diff, err := DiffKeyedSnapshot(previous, []KeyedItem{
		rowItem{"d", "000000000000000d", `"d"`}, rowItem{"a", "00000000000000a2", `"new a"`}, rowItem{"b", "000000000000000b", `"b"`},
	}, KeyedSnapshotOptions{KeepValues: true})
	require.NoError(t, err)
	require.Equal(t, []string{"d"}, entryKeys(diff.Added))
	require.Len(t, diff.Changed, 1)
	require.Equal(t, `"old a"`, string(diff.Changed[0].Previous.Value))
	require.Equal(t, `"new a"`, string(diff.Changed[0].Current.Value))
	require.Equal(t, []string{"c"}, entryKeys(diff.Removed))
	require.Equal(t, []string{"a", "b", "d"}, entryKeys(diff.Next.Entries))
	require.Equal(t, []int64{0, 4, 0}, []int64{diff.Next.Entries[0].Seen, diff.Next.Entries[1].Seen, diff.Next.Entries[2].Seen})

	_, err = DiffKeyedSnapshot(nil, []KeyedItem{rowItem{"a", "000000000000000a", ""}, rowItem{"a", "000000000000000a", ""}}, KeyedSnapshotOptions{})
	require.Error(t, err)
	_, err = DiffKeyedSnapshot(nil, []KeyedItem{rowItem{"a", "000000000000000a", ""}, rowItem{"b", "000000000000000b", ""}}, KeyedSnapshotOptions{MaxEntries: 1})
	require.ErrorIs(t, err, projectconfig.ErrPollCursorTooLarge)
	_, err = DiffKeyedSnapshot(nil, nil, KeyedSnapshotOptions{EvictLeastRecentlySeen: true})
	require.Error(t, err)
}

func TestApplyKeyedSnapshotChangesEvictsTheLeastRecentlySeen(t *testing.T) {
	previous := &projectconfig.KeyedSnapshot{Entries: []projectconfig.KeyedSnapshotEntry{
		{Key: "a", Hash: "000000000000000a", Seen: 2}, {Key: "b", Hash: "000000000000000b", Seen: 1}, {Key: "c", Hash: "000000000000000c", Seen: 3},
	}}
	upserted := []KeyedItem{rowItem{"d", "000000000000000d", ""}}
	_, err := ApplyKeyedSnapshotChanges(previous, upserted, nil, KeyedSnapshotOptions{MaxEntries: 3})
	require.ErrorIs(t, err, projectconfig.ErrPollCursorTooLarge)
	next, err := ApplyKeyedSnapshotChanges(previous, upserted, []string{"c"}, KeyedSnapshotOptions{MaxEntries: 2, EvictLeastRecentlySeen: true})
	require.NoError(t, err)
	require.Equal(t, []string{"a", "d"}, entryKeys(next.Entries))
	require.Len(t, previous.Entries, 3)
}

func entryKeys(entries []projectconfig.KeyedSnapshotEntry) []string {
	keys := []string{}
	for _, entry := range entries {
		keys = append(keys, entry.Key)
	}
	return keys
}

func TestPollerRejectsASnapshotOverItsBound(t *testing.T) {
	clock := installFakeClock(t, 0.5)
	log := &operationLog{}
	fetcher := &fileFeedFetcher{log: log, pages: [][]fileChange{{{"f1", 1}, {"f2", 1}}}}
	member, err := NewFeedMember(testDefinition("fileCreated"), testBinding("fileCreated", "created"), selectFileEvents("created"),
		sdkgo.TriggerTarget[item](&recordingTarget{log: log}))
	require.NoError(t, err)
	poller, err := NewFeedPoller(FeedPollerConfig[fileChange]{Name: "fileChanges", Cursor: NewMemoryCursorStore(), Fetcher: fetcher,
		Members: []FeedMember[fileChange]{member}, Snapshot: &KeyedSnapshotOptions{MaxEntries: 1}})
	require.NoError(t, err)
	runner := newTestRunner(t, PollSchedule{Interval: time.Minute, MaxBackoff: 7 * time.Minute}, poller)
	require.ErrorIs(t, runUntil(t, clock, runner, func() bool { return log.count("fetch after 0") == 2 }), context.Canceled)
	require.Equal(t, 7*time.Minute, clock.recordedWaits()[2])
	require.Zero(t, log.count("handle f1@1"))
}
