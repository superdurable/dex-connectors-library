// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package zoom

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoom/go/internal/zoommeetingsfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
)

const triggerTestHostID = "zoom-host-1"

func newTriggerTestConnection(t *testing.T, endpoint string) Connection {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "zoom", Name: "zoom-watcher"}
	client, err := New(Config{Endpoint: endpoint, PollInterval: minimumPollInterval}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {AccessToken: sdkgo.NewSecretString(zoommeetingsfake.AccessToken)},
	})
	require.NoError(t, err)
	connection, err := NewConnection(client, reference)
	require.NoError(t, err)
	return connection
}

func newTestMeetingListFetcher(t *testing.T, fake *zoommeetingsfake.Meetings, configuration MeetingChangedTriggerConfiguration) *meetingListFetcher {
	t.Helper()
	connection := newTriggerTestConnection(t, fake.URL)
	return newMeetingListFetcher(connection.client, connection.reference, configuration)
}

func changeKindsAndIDs(page pollingtrigger.ChangePage[sdkgo.TriggerEvent[MeetingChangedEvent]]) map[int64]MeetingChangeKind {
	kinds := map[int64]MeetingChangeKind{}
	for _, change := range page.Changes {
		kinds[change.Payload.Meeting.ID] = change.Payload.ChangeKind
	}
	return kinds
}

func TestMeetingListFetcherSeedsWithoutBacklogThenDeliversEachDifference(t *testing.T) {
	fake := zoommeetingsfake.New(t, triggerTestHostID)
	fake.Schedule(92674392836, "Consultation", 1)
	fake.Schedule(92674392840, "Team review", 2)
	fake.Schedule(92674392841, "Planning", 3)
	fetcher := newTestMeetingListFetcher(t, fake, MeetingChangedTriggerConfiguration{})
	ctx := context.Background()

	seed, err := fetcher.SeedCursor(ctx)
	require.NoError(t, err)
	require.JSONEq(t, `{"version":1,"hostId":"zoom-host-1"}`, seed.Token)
	require.Len(t, seed.Snapshot.Entries, 3)
	require.NotContains(t, string(seed.Snapshot.Entries[0].Value), "join", "the snapshot never keeps the join URL")

	unchanged, err := fetcher.FetchChanges(ctx, seed)
	require.NoError(t, err)
	require.Empty(t, unchanged.Changes, "the baseline is never delivered")
	require.False(t, unchanged.HasMorePages)

	fake.Schedule(92674392850, "Kickoff", 4)
	fake.Reschedule(92674392840, 2)
	fake.Delete(92674392841)
	page, err := fetcher.FetchChanges(ctx, seed)
	require.NoError(t, err)
	require.Equal(t, map[int64]MeetingChangeKind{
		92674392840: MeetingChangeUpdated, 92674392841: MeetingChangeRemoved, 92674392850: MeetingChangeScheduled,
	}, changeKindsAndIDs(page))
	require.Equal(t, []int64{92674392840, 92674392841, 92674392850}, []int64{
		page.Changes[0].Payload.Meeting.ID, page.Changes[1].Payload.Meeting.ID, page.Changes[2].Payload.Meeting.ID,
	}, "changes arrive in meeting ID order")

	updated := page.Changes[0].Payload
	require.NotNil(t, updated.PreviousMeeting)
	require.Equal(t, 2*time.Hour, updated.Meeting.StartTime.Sub(*updated.PreviousMeeting.StartTime))
	require.Equal(t, "https://us05web.zoom.us/j/92674392840", updated.Meeting.JoinURL, "the listed meeting keeps its join URL")
	removed := page.Changes[1].Payload
	require.Equal(t, "Planning", removed.Meeting.Topic, "a removal carries the last listed state")
	require.Nil(t, removed.PreviousMeeting)
	scheduled := page.Changes[2]
	require.Equal(t, *scheduled.Payload.Meeting.CreatedAt, scheduled.OccurredAt, "a scheduled meeting occurs at its creation time")

	refetched, err := fetcher.FetchChanges(ctx, seed)
	require.NoError(t, err)
	for index := range page.Changes {
		require.Equal(t, page.Changes[index].ID, refetched.Changes[index].ID, "a refetch from the same cursor keeps the event IDs")
	}
	after, err := fetcher.FetchChanges(ctx, page.Next)
	require.NoError(t, err)
	require.Empty(t, after.Changes)
}

func TestMeetingListFetcherGivesARecoveredMeetingANewScheduledID(t *testing.T) {
	fake := zoommeetingsfake.New(t, triggerTestHostID)
	fetcher := newTestMeetingListFetcher(t, fake, MeetingChangedTriggerConfiguration{})
	ctx := context.Background()
	cursor, err := fetcher.SeedCursor(ctx)
	require.NoError(t, err)
	poll := func() pollingtrigger.ChangePage[sdkgo.TriggerEvent[MeetingChangedEvent]] {
		t.Helper()
		page, err := fetcher.FetchChanges(ctx, cursor)
		require.NoError(t, err)
		cursor = page.Next
		return page
	}

	fake.Schedule(92674392836, "Consultation", 1)
	scheduled := poll()
	require.Len(t, scheduled.Changes, 1)
	require.Empty(t, poll().Changes)
	require.JSONEq(t, `{"version":1,"hostId":"zoom-host-1","sequence":1}`, cursor.Token, "an unchanged poll keeps the token")
	fake.Delete(92674392836)
	require.Equal(t, MeetingChangeRemoved, poll().Changes[0].Payload.ChangeKind)
	fake.Recover(92674392836)
	recovered := poll()
	require.Len(t, recovered.Changes, 1)
	require.Equal(t, MeetingChangeScheduled, recovered.Changes[0].Payload.ChangeKind)
	require.Equal(t, scheduled.Changes[0].Payload.Meeting, recovered.Changes[0].Payload.Meeting, "Zoom restores the same meeting")
	require.NotEqual(t, scheduled.Changes[0].ID, recovered.Changes[0].ID, "a recovered meeting is not deduplicated against its first scheduling")
}

func TestListedMeetingDuplicatesKeepTheEarliestStart(t *testing.T) {
	start := time.Date(2036, 3, 2, 17, 0, 0, 0, time.UTC)
	listed := func(days int, topic string) listedMeeting {
		t.Helper()
		occurrence := start.AddDate(0, 0, days)
		meeting, err := newListedMeeting(MeetingSummary{ID: 92674392836, Topic: topic, Type: 8, StartTime: &occurrence})
		require.NoError(t, err)
		return meeting
	}
	next, later := listed(0, "Weekly sync"), listed(7, "Weekly sync")
	require.True(t, next.listsBefore(later))
	require.False(t, later.listsBefore(next))
	unscheduled, err := newListedMeeting(MeetingSummary{ID: 92674392836, Topic: "Weekly sync", Type: 3})
	require.NoError(t, err)
	require.True(t, later.listsBefore(unscheduled), "a listed start wins over none")
	first, second := listed(0, "A"), listed(0, "B")
	require.NotEqual(t, first.listsBefore(second), second.listsBefore(first), "equal starts order by hash")
}

func TestMeetingListFetcherSkipsUnconfiguredKindsButAdvancesPastThem(t *testing.T) {
	fake := zoommeetingsfake.New(t, triggerTestHostID)
	fake.Schedule(92674392836, "Consultation", 1)
	fetcher := newTestMeetingListFetcher(t, fake, MeetingChangedTriggerConfiguration{ChangeKinds: []MeetingChangeKind{MeetingChangeRemoved}})
	seed, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	fake.Rename(92674392836, "Consultation moved")
	fake.Schedule(92674392850, "Kickoff", 4)
	page, err := fetcher.FetchChanges(context.Background(), seed)
	require.NoError(t, err)
	require.Empty(t, page.Changes)
	require.Len(t, page.Next.Snapshot.Entries, 2, "skipped changes still advance the snapshot")

	fake.Delete(92674392850)
	page, err = fetcher.FetchChanges(context.Background(), page.Next)
	require.NoError(t, err)
	require.Equal(t, map[int64]MeetingChangeKind{92674392850: MeetingChangeRemoved}, changeKindsAndIDs(page))
}

func TestMeetingListFetcherReadsEveryPageAndRestartsAfterARejectedPageToken(t *testing.T) {
	fake := zoommeetingsfake.New(t, triggerTestHostID)
	for index := range 650 {
		fake.Schedule(int64(80000000000+index), "Office hours", index%30)
	}
	fetcher := newTestMeetingListFetcher(t, fake, MeetingChangedTriggerConfiguration{})
	seed, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	require.Len(t, seed.Snapshot.Entries, 650)
	require.Equal(t, 3, fake.ListingRequestCount(), "Zoom's largest page size is used")
	for _, request := range fake.Requests() {
		require.Equal(t, "scheduled", request.Query.Get("type"))
		require.Equal(t, "300", request.Query.Get("page_size"))
	}

	fake.RejectNextPageTokens(1)
	page, err := fetcher.FetchChanges(context.Background(), seed)
	require.NoError(t, err)
	require.Empty(t, page.Changes, "an expired page token restarts the listing instead of reporting removals")
	require.Equal(t, 3+2+3, fake.ListingRequestCount(), "the first page, the rejected page, then a full listing")

	fake.RejectNextPageTokens(2)
	_, err = fetcher.FetchChanges(context.Background(), seed)
	require.ErrorContains(t, err, "page token")
	require.False(t, pollingtrigger.IsCursorInvalid(err), "the stored snapshot stays valid")
}

func TestMeetingListFetcherSplitsLargeDifferencesAcrossCommits(t *testing.T) {
	fake := zoommeetingsfake.New(t, triggerTestHostID)
	fetcher := newTestMeetingListFetcher(t, fake, MeetingChangedTriggerConfiguration{})
	seed, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	require.JSONEq(t, `{"version":1}`, seed.Token, "an empty list names no host")
	for index := range 250 {
		fake.Schedule(int64(80000000000+index), "Office hours", 1)
	}

	first, err := fetcher.FetchChanges(context.Background(), seed)
	require.NoError(t, err)
	require.Len(t, first.Changes, meetingChangesPerPage)
	require.True(t, first.HasMorePages)
	require.Len(t, first.Next.Snapshot.Entries, meetingChangesPerPage, "the snapshot advances only past delivered changes")
	require.JSONEq(t, `{"version":1,"hostId":"zoom-host-1","sequence":1}`, first.Next.Token)
	delivered := map[string]bool{}
	cursor := first.Next
	for _, change := range first.Changes {
		delivered[change.ID] = true
	}
	for range 2 {
		page, err := fetcher.FetchChanges(context.Background(), cursor)
		require.NoError(t, err)
		for _, change := range page.Changes {
			require.False(t, delivered[change.ID], "no change is repeated")
			delivered[change.ID] = true
		}
		cursor = page.Next
	}
	require.Len(t, delivered, 250)
}

func TestMeetingListFetcherMapsRateLimitsAndInvalidCursors(t *testing.T) {
	fake := zoommeetingsfake.New(t, triggerTestHostID)
	fake.Schedule(92674392836, "Consultation", 1)
	fetcher := newTestMeetingListFetcher(t, fake, MeetingChangedTriggerConfiguration{})
	seed, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)

	for _, isDaily := range []bool{false, true} {
		fake.RateLimitNext(1, isDaily, "7")
		_, err = fetcher.FetchChanges(context.Background(), seed)
		var rateLimited *pollingtrigger.RateLimitedError
		require.True(t, errors.As(err, &rateLimited), "daily=%t", isDaily)
		require.Equal(t, 7*time.Second, rateLimited.RetryAfter)
	}
	fake.RateLimitNext(1, true, "")
	_, err = fetcher.FetchChanges(context.Background(), seed)
	var rateLimited *pollingtrigger.RateLimitedError
	require.True(t, errors.As(err, &rateLimited))
	require.Zero(t, rateLimited.RetryAfter, "without Retry-After the runner backs off exponentially")

	for name, cursor := range map[string]pollingtrigger.Cursor{
		"undecodable": {Token: "sync-1", Snapshot: seed.Snapshot},
		"version":     {Token: `{"version":2}`, Snapshot: seed.Snapshot},
		"no snapshot": {Token: seed.Token},
		"other user":  {Token: `{"version":1,"hostId":"another-host"}`, Snapshot: seed.Snapshot},
	} {
		_, err := fetcher.FetchChanges(context.Background(), cursor)
		require.True(t, pollingtrigger.IsCursorInvalid(err), name)
	}
}

func TestMeetingChangedTriggerConfigurationValidate(t *testing.T) {
	require.NoError(t, MeetingChangedTriggerConfiguration{}.Validate())
	require.NoError(t, MeetingChangedTriggerConfiguration{ChangeKinds: []MeetingChangeKind{MeetingChangeScheduled, MeetingChangeRemoved}}.Validate())
	require.ErrorContains(t, MeetingChangedTriggerConfiguration{ChangeKinds: []MeetingChangeKind{"ended"}}.Validate(), "must be scheduled")
	require.ErrorContains(t, MeetingChangedTriggerConfiguration{ChangeKinds: []MeetingChangeKind{MeetingChangeUpdated, MeetingChangeUpdated}}.Validate(), "repeats")
}

func TestNewRejectsAPollIntervalUnderTheMinimum(t *testing.T) {
	_, err := New(Config{PollInterval: 10 * time.Second}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.ErrorContains(t, err, "pollInterval must be at least 30s")
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.NoError(t, err)
	require.Equal(t, 2*time.Minute, client.pollInterval, "the manifest default applies")
}
