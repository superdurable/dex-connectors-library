// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package asana

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/asana/go/internal/asanaeventsfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
)

const fetcherProjectID = "1201000000000001"

func newFetcherTestClient(t *testing.T, endpoint string) (*Client, sdkgo.ConnectionRef) {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "asana", Name: "asana-events"}
	client, err := New(Config{Endpoint: endpoint, PollInterval: minimumPollInterval}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {AccessToken: sdkgo.NewSecretString(asanaeventsfake.AccessToken)},
	})
	require.NoError(t, err)
	return client, reference
}

func newTestTaskEventFetcher(t *testing.T, fake *asanaeventsfake.Project) *taskEventFetcher {
	t.Helper()
	client, reference := newFetcherTestClient(t, fake.URL)
	return newTaskEventFetcher(client, reference, TaskChangedTriggerConfiguration{ProjectID: fetcherProjectID})
}

func TestTaskEventFetcherSeedsWithoutBacklogAndDeliversOnlyTaskEvents(t *testing.T) {
	fake := asanaeventsfake.New(t, fetcherProjectID)
	fake.AddTask("1204000000000001", "Existing")
	fetcher := newTestTaskEventFetcher(t, fake)
	ctx := context.Background()

	seed, err := fetcher.SeedCursor(ctx)
	require.NoError(t, err)
	require.JSONEq(t, `{"projectId":"1201000000000001","sync":"sync-1"}`, seed.Token, "the existing task is the baseline")
	require.NotContains(t, fake.Requests()[0].Query, "sync", "the seed sends no sync token and receives 412")

	fake.AddTask("1204000000000002", "Replace badge reader")
	fake.AddStory("1205000000000001")
	fake.CompleteTask("1204000000000001")
	fake.DeleteTask("1204000000000002")
	page, err := fetcher.FetchChanges(ctx, seed)
	require.NoError(t, err)
	require.False(t, page.HasMorePages)
	require.JSONEq(t, `{"projectId":"1201000000000001","sync":"sync-5"}`, page.Next.Token)
	require.Len(t, page.Changes, 3, "the comment story is not a task event")
	added, completed, deleted := page.Changes[0].Payload, page.Changes[1].Payload, page.Changes[2].Payload
	require.Equal(t, TaskChangedEvent{
		ProjectID: fetcherProjectID, Action: TaskChangeAdded,
		Task: TaskReference{ID: "1204000000000002", Name: "Replace badge reader"}, TaskSubtype: "default_task",
		Parent:     &EventParentReference{ID: fetcherProjectID, ResourceType: "project", Name: "Facilities"},
		User:       &UserReference{ID: "1203000000000001", Name: "Ada Lovelace"},
		OccurredAt: "2026-03-02T09:00:01Z",
	}, added)
	require.Equal(t, TaskChangeChanged, completed.Action)
	require.Equal(t, "completed", completed.ChangedField)
	require.Equal(t, "changed", completed.ChangeAction)
	require.Nil(t, completed.Parent)
	require.Equal(t, TaskChangeDeleted, deleted.Action)
	require.Equal(t, time.Date(2026, 3, 2, 9, 0, 1, 0, time.UTC), page.Changes[0].OccurredAt)

	query := fake.Requests()[1].Query
	require.Equal(t, "sync-1", query.Get("sync"))
	require.Equal(t, fetcherProjectID, query.Get("resource"))
	require.Contains(t, query.Get("opt_fields"), "change.field")

	again, err := fetcher.FetchChanges(ctx, seed)
	require.NoError(t, err)
	for index := range page.Changes {
		require.Equal(t, page.Changes[index].ID, again.Changes[index].ID, "a refetch of one sync token yields the same IDs")
	}
	require.NotEqual(t, page.Changes[0].ID, page.Changes[2].ID)
}

func TestTaskEventFetcherFollowsHasMoreInPagesOfOneHundred(t *testing.T) {
	fake := asanaeventsfake.New(t, fetcherProjectID)
	fetcher := newTestTaskEventFetcher(t, fake)
	seed, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	for index := range 150 {
		fake.AddTask(strconv.Itoa(1204000000001000+index), "Bulk task")
	}
	first, err := fetcher.FetchChanges(context.Background(), seed)
	require.NoError(t, err)
	require.Len(t, first.Changes, 100)
	require.True(t, first.HasMorePages)
	second, err := fetcher.FetchChanges(context.Background(), first.Next)
	require.NoError(t, err)
	require.Len(t, second.Changes, 50)
	require.False(t, second.HasMorePages)
}

func TestTaskEventFetcherMarksExpiredAndForeignCursorsInvalid(t *testing.T) {
	fake := asanaeventsfake.New(t, fetcherProjectID)
	fetcher := newTestTaskEventFetcher(t, fake)
	seed, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	fake.AddTask("1204000000000003", "Lost in the gap")
	fake.ExpireSyncTokens()
	_, err = fetcher.FetchChanges(context.Background(), seed)
	require.True(t, pollingtrigger.IsCursorInvalid(err), "HTTP 412 for a stored token reseeds: %v", err)
	require.ErrorContains(t, err, "412")

	foreign, err := json.Marshal(taskEventCursor{ProjectID: "1201000000000999", SyncToken: "sync-0"})
	require.NoError(t, err)
	_, err = fetcher.FetchChanges(context.Background(), pollingtrigger.Cursor{Token: string(foreign)})
	require.True(t, pollingtrigger.IsCursorInvalid(err), "a changed project reseeds")
	_, err = fetcher.FetchChanges(context.Background(), pollingtrigger.Cursor{Token: "not json"})
	require.True(t, pollingtrigger.IsCursorInvalid(err))
}

func TestTaskEventFetcherReportsRateLimitsWithRetryAfter(t *testing.T) {
	fake := asanaeventsfake.New(t, fetcherProjectID)
	fetcher := newTestTaskEventFetcher(t, fake)
	fake.RateLimitNext(1, "7")
	_, err := fetcher.SeedCursor(context.Background())
	var rateLimited *pollingtrigger.RateLimitedError
	require.True(t, errors.As(err, &rateLimited), "429 pauses the connection: %v", err)
	require.Equal(t, 7*time.Second, rateLimited.RetryAfter)
	require.False(t, pollingtrigger.IsCursorInvalid(err))
}

func TestTaskEventFetcherTreatsOtherFailuresAsRetryablePolls(t *testing.T) {
	fake := asanaeventsfake.New(t, fetcherProjectID)
	client, reference := newFetcherTestClient(t, fake.URL)
	fetcher := newTaskEventFetcher(client, reference, TaskChangedTriggerConfiguration{ProjectID: "1201000000000999"})
	_, err := fetcher.SeedCursor(context.Background())
	require.ErrorContains(t, err, "HTTP 404")
	require.False(t, pollingtrigger.IsCursorInvalid(err))
	var rateLimited *pollingtrigger.RateLimitedError
	require.False(t, errors.As(err, &rateLimited))
}

func TestTaskEventIdentityDistinguishesRepeatsAndIgnoresNames(t *testing.T) {
	fetcher := &taskEventFetcher{client: &Client{now: time.Now}, projectID: fetcherProjectID}
	follower := func(name string) asanaEvent {
		return asanaEvent{
			Action: "changed", CreatedAt: "2026-03-02T09:00:00Z",
			Resource: &asanaEventSubject{GID: "1204000000000001", Name: name, ResourceType: "task"},
			Change:   &asanaEventChange{Field: "followers", Action: "added", AddedValue: json.RawMessage(`{"gid":"1203000000000002","name":"Grace"}`)},
		}
	}
	first, err := fetcher.convertEvents("sync-1", []asanaEvent{follower("Old"), follower("Old")})
	require.NoError(t, err)
	require.NotEqual(t, first[0].ID, first[1].ID, "identical events in one page keep distinct IDs")
	renamed, err := fetcher.convertEvents("sync-1", []asanaEvent{follower("New"), follower("New")})
	require.NoError(t, err)
	require.Equal(t, first[0].ID, renamed[0].ID, "a rename between refetches keeps the ID")
	require.Equal(t, first[1].ID, renamed[1].ID)
	nextPage, err := fetcher.convertEvents("sync-101", []asanaEvent{follower("Old")})
	require.NoError(t, err)
	require.NotEqual(t, first[0].ID, nextPage[0].ID, "an identical event behind a page boundary is a distinct change")

	_, err = fetcher.convertEvents("sync-1", []asanaEvent{{Action: "moved", Resource: &asanaEventSubject{GID: "1", ResourceType: "task"}}})
	require.ErrorContains(t, err, "unknown action")
}

func TestTaskChangedTriggerConfigurationRequiresAProjectGID(t *testing.T) {
	require.NoError(t, TaskChangedTriggerConfiguration{WorkspaceID: "1200000000000001", ProjectID: fetcherProjectID}.Validate())
	require.Error(t, TaskChangedTriggerConfiguration{}.Validate())
	require.Error(t, TaskChangedTriggerConfiguration{ProjectID: "Facilities"}.Validate())
	require.Error(t, TaskChangedTriggerConfiguration{ProjectID: fetcherProjectID, WorkspaceID: "ops"}.Validate())
}
