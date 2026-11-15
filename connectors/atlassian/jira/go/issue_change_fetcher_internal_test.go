// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jira

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/jira/go/internal/jirasearchfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
)

var triggerTestConnection = sdkgo.ConnectionRef{Provider: "atlassian", Name: "jira-trigger-test"}

// testClock is a settable clock shared by the client and the fake site.
type testClock struct {
	mutex sync.Mutex
	now   time.Time
}

func (clock *testClock) read() time.Time {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	return clock.now
}

func (clock *testClock) advance(duration time.Duration) {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	clock.now = clock.now.Add(duration)
}

func newTriggerTestClient(t *testing.T, site *jirasearchfake.Site) (*Client, *testClock) {
	t.Helper()
	clock := &testClock{now: time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)}
	site.SetClock(clock.read)
	client, err := New(Config{CloudID: jirasearchfake.CloudID, Endpoint: site.URL, PollInterval: minimumPollInterval}, sdkgo.StaticCredentialProvider[Credentials]{
		triggerTestConnection: {
			OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"),
			AccessToken: sdkgo.NewSecretString("jira-trigger-token"), RefreshToken: sdkgo.NewSecretString("refresh-token"),
		},
	})
	require.NoError(t, err)
	client.now = clock.read
	return client, clock
}

func newOperationsFetcher(client *Client) *issueChangeFetcher {
	return newIssueChangeFetcher(client, triggerTestConnection, IssueChangedTriggerConfiguration{ProjectID: jirasearchfake.Operations.ID})
}

func decodeTestCursor(t *testing.T, cursor pollingtrigger.Cursor) issueChangeCursor {
	t.Helper()
	var position issueChangeCursor
	require.NoError(t, json.Unmarshal([]byte(cursor.Token), &position))
	return position
}

func changedKeys(page pollingtrigger.ChangePage[sdkgo.TriggerEvent[IssueChangedEvent]]) []string {
	keys := []string{}
	for _, change := range page.Changes {
		keys = append(keys, change.Payload.Issue.Key)
	}
	return keys
}

func TestIssueChangeSeedMarksRecentIssuesSeenAndDeliversNoBaseline(t *testing.T) {
	site := jirasearchfake.New(t)
	client, clock := newTriggerTestClient(t, site)
	site.Create(jirasearchfake.Operations, "Old issue")
	clock.advance(time.Hour)
	recent := site.Create(jirasearchfake.Operations, "Recent issue")
	fetcher := newOperationsFetcher(client)

	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)

	position := decodeTestCursor(t, cursor)
	require.Equal(t, jirasearchfake.CloudID, position.CloudID)
	require.Equal(t, "id:10000", position.Project)
	require.Equal(t, clock.read().UnixMilli(), position.UpdatedThrough)
	require.Equal(t, []string{site.IssueID(recent) + "@" + strconv.FormatInt(clock.read().UnixMilli(), 10)}, position.SeenVersions)
	require.Equal(t, "project = 10000 AND updated >= -4m ORDER BY updated DESC", site.Requests()[0].JQL)

	clock.advance(time.Minute)
	page, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	require.Empty(t, page.Changes, "neither the old nor the recent baseline issue is delivered")
	require.Equal(t, cursor.Token, page.Next.Token, "an empty poll leaves the cursor unchanged, so nothing is written")
}

func TestIssueChangeFetchDeliversCreatedAndUpdatedIssuesOnceWithStableIDs(t *testing.T) {
	site := jirasearchfake.New(t)
	client, clock := newTriggerTestClient(t, site)
	existing := site.Create(jirasearchfake.Operations, "Existing")
	clock.advance(10 * time.Minute)
	fetcher := newOperationsFetcher(client)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)

	clock.advance(time.Second)
	created := site.Create(jirasearchfake.Operations, "Loading dock light out")
	site.Create(jirasearchfake.HumanResources, "Other project")
	clock.advance(time.Second)
	site.Update(existing, "Existing, edited")
	clock.advance(time.Second)

	page, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	require.Equal(t, []string{created, existing}, changedKeys(page), "ascending updated order, other projects excluded")
	require.Equal(t, IssueChangeCreated, page.Changes[0].Payload.ChangeKind)
	require.Equal(t, IssueChangeUpdated, page.Changes[1].Payload.ChangeKind)
	require.Equal(t, "Existing, edited", page.Changes[1].Payload.Issue.Summary)
	require.Equal(t, jirasearchfake.CloudID, page.Changes[0].Payload.CloudID)
	require.False(t, page.HasMorePages)
	search := site.Requests()[len(site.Requests())-1]
	require.Equal(t, "project = 10000 AND updated >= -4m ORDER BY updated ASC", search.JQL)
	require.Equal(t, issueChangePageSize, search.MaxResults)

	refetched, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	require.Equal(t, page.Changes[0].ID, refetched.Changes[0].ID, "a refetch from the same cursor yields the same event IDs")
	require.Equal(t, page.Changes[1].ID, refetched.Changes[1].ID)

	clock.advance(30 * time.Second)
	after, err := fetcher.FetchChanges(context.Background(), page.Next)
	require.NoError(t, err)
	require.Empty(t, after.Changes, "re-read issues within the lookback are not delivered again")

	site.Update(created, "Loading dock light replaced")
	again, err := fetcher.FetchChanges(context.Background(), after.Next)
	require.NoError(t, err)
	require.Equal(t, []string{created}, changedKeys(again))
	require.NotEqual(t, page.Changes[0].ID, again.Changes[0].ID, "a new version has a new event ID")
}

func TestIssueChangeFetchDeliversAnUpdateIndexedLateWithinTheLookback(t *testing.T) {
	site := jirasearchfake.New(t)
	client, clock := newTriggerTestClient(t, site)
	fetcher := newOperationsFetcher(client)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	clock.advance(time.Second)
	lagging := site.Create(jirasearchfake.Operations, "Indexed late")
	site.SetIndexed(lagging, false)
	clock.advance(time.Second)
	prompt := site.Create(jirasearchfake.Operations, "Indexed at once")

	first, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	require.Equal(t, []string{prompt}, changedKeys(first))

	site.SetIndexed(lagging, true)
	clock.advance(30 * time.Second)
	second, err := fetcher.FetchChanges(context.Background(), first.Next)
	require.NoError(t, err)
	require.Equal(t, []string{lagging}, changedKeys(second), "an update older than the newest one read is still delivered")
}

func TestIssueChangeFetchPagesAndRestartsAfterARejectedPageToken(t *testing.T) {
	site := jirasearchfake.New(t)
	client, clock := newTriggerTestClient(t, site)
	fetcher := newOperationsFetcher(client)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	for index := range 150 {
		clock.advance(time.Millisecond)
		site.Create(jirasearchfake.Operations, "Bulk "+strconv.Itoa(index))
	}

	first, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	require.Len(t, first.Changes, 100)
	require.True(t, first.HasMorePages)
	require.NotEmpty(t, decodeTestCursor(t, first.Next).PageToken)

	site.RejectPageTokens(true)
	second, err := fetcher.FetchChanges(context.Background(), first.Next)
	require.NoError(t, err)
	require.Empty(t, second.Changes, "the restarted search re-reads the 100 issues already delivered and skips them")
	require.True(t, second.HasMorePages)
	requests := site.Requests()
	require.NotEmpty(t, requests[len(requests)-2].NextPageToken)
	require.Equal(t, 400, requests[len(requests)-2].Status)
	require.Empty(t, requests[len(requests)-1].NextPageToken)

	site.RejectPageTokens(false)
	third, err := fetcher.FetchChanges(context.Background(), second.Next)
	require.NoError(t, err)
	require.Len(t, third.Changes, 50, "nothing is lost across the restart")
	require.False(t, third.HasMorePages)
}

func TestIssueChangeFetchRestartsAPageSequenceOlderThanTheLookback(t *testing.T) {
	site := jirasearchfake.New(t)
	client, clock := newTriggerTestClient(t, site)
	fetcher := newOperationsFetcher(client)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	for index := range 120 {
		clock.advance(time.Millisecond)
		site.Create(jirasearchfake.Operations, "Bulk "+strconv.Itoa(index))
	}
	first, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	require.True(t, first.HasMorePages)

	clock.advance(issueChangeLookback + time.Second)
	second, err := fetcher.FetchChanges(context.Background(), first.Next)
	require.NoError(t, err)
	last := site.Requests()[len(site.Requests())-1]
	require.Empty(t, last.NextPageToken, "a stale page token is not reused")
	require.Equal(t, "project = 10000 AND updated >= -6m ORDER BY updated ASC", last.JQL)
	require.Empty(t, second.Changes)
	third, err := fetcher.FetchChanges(context.Background(), second.Next)
	require.NoError(t, err)
	require.Len(t, third.Changes, 20)
}

func TestIssueChangeFetchPausesTheConnectionOnRateLimit(t *testing.T) {
	site := jirasearchfake.New(t)
	client, _ := newTriggerTestClient(t, site)
	fetcher := newOperationsFetcher(client)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	site.RateLimitNextRequests(1, "7")

	_, err = fetcher.FetchChanges(context.Background(), cursor)

	var rateLimited *pollingtrigger.RateLimitedError
	require.ErrorAs(t, err, &rateLimited)
	require.Equal(t, 7*time.Second, rateLimited.RetryAfter)
	require.False(t, pollingtrigger.IsCursorInvalid(err))
}

func TestIssueChangeFetchMarksForeignOrUndecodableCursorsInvalid(t *testing.T) {
	site := jirasearchfake.New(t)
	client, _ := newTriggerTestClient(t, site)
	cursor, err := newOperationsFetcher(client).SeedCursor(context.Background())
	require.NoError(t, err)

	hrFetcher := newIssueChangeFetcher(client, triggerTestConnection, IssueChangedTriggerConfiguration{ProjectKey: "HR"})
	_, err = hrFetcher.FetchChanges(context.Background(), cursor)
	require.True(t, pollingtrigger.IsCursorInvalid(err), "a changed project records a new baseline")

	position := decodeTestCursor(t, cursor)
	position.CloudID = "99999999-0000-4000-8000-000000000000"
	foreign, err := encodeIssueChangeCursor(position)
	require.NoError(t, err)
	_, err = newOperationsFetcher(client).FetchChanges(context.Background(), foreign)
	require.True(t, pollingtrigger.IsCursorInvalid(err), "a changed site records a new baseline")

	_, err = newOperationsFetcher(client).FetchChanges(context.Background(), pollingtrigger.Cursor{Token: "not json"})
	require.True(t, pollingtrigger.IsCursorInvalid(err))
}

func TestIssueChangedConfigurationAndSeenVersionBounds(t *testing.T) {
	require.NoError(t, IssueChangedTriggerConfiguration{}.Validate())
	require.ErrorContains(t, IssueChangedTriggerConfiguration{ProjectID: "OPS"}.Validate(), "numeric")
	require.ErrorContains(t, IssueChangedTriggerConfiguration{ProjectKey: "ops"}.Validate(), "uppercase")
	clause, identity := IssueChangedTriggerConfiguration{ProjectKey: "OPS"}.watchedProject()
	require.Equal(t, `"OPS"`, clause)
	require.Equal(t, "key:OPS", identity)
	clause, identity = IssueChangedTriggerConfiguration{}.watchedProject()
	require.Empty(t, clause+identity, "blank watches every browsable project")

	var versions []issueVersion
	for index := range maximumSeenIssueVersions + 5 {
		versions = append(versions, issueVersion{issueID: strconv.Itoa(10000 + index), updatedMillis: int64(1_000_000 + index)})
	}
	kept := recentSeenVersions([]string{"1@1", "malformed"}, versions, 1_000_000)
	require.Len(t, kept, maximumSeenIssueVersions)
	require.NotContains(t, kept, "1@1", "versions before the lookback are pruned")
	require.NotContains(t, kept, "10000@1000000", "the oldest versions go first when the bound is reached")
}

func TestIssueChangeFetchLosesNoIssueWhenAnUpdateShiftsAPagedBacklog(t *testing.T) {
	site := jirasearchfake.New(t)
	client, clock := newTriggerTestClient(t, site)
	fetcher := newOperationsFetcher(client)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	var keys []string
	for index := range 150 {
		clock.advance(12 * time.Second)
		keys = append(keys, site.Create(jirasearchfake.Operations, "Backlog "+strconv.Itoa(index)))
	}

	first, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	require.Len(t, first.Changes, 100)
	require.True(t, first.HasMorePages)
	// An offset page token would now skip the issue that slid into the first page.
	site.Update(keys[10], "Backlog 10, edited")

	delivered := map[string]bool{}
	for _, key := range changedKeys(first) {
		delivered[key] = true
	}
	next, isEditDelivered := first, false
	for next.HasMorePages {
		next, err = fetcher.FetchChanges(context.Background(), next.Next)
		require.NoError(t, err)
		for _, change := range next.Changes {
			delivered[change.Payload.Issue.Key] = true
			isEditDelivered = isEditDelivered || change.Payload.Issue.Summary == "Backlog 10, edited"
		}
	}
	for _, key := range keys {
		require.True(t, delivered[key], "issue %s is delivered", key)
	}
	require.True(t, isEditDelivered, "the edit is delivered as a new version")
	require.Empty(t, decodeTestCursor(t, first.Next).PageToken, "a page that moves the floor restarts the search")
}

func TestIssueChangeFetchUsesJiraClockWhenTheLocalClockRunsAhead(t *testing.T) {
	site := jirasearchfake.New(t)
	client, clock := newTriggerTestClient(t, site)
	client.now = func() time.Time { return clock.read().Add(10 * time.Minute) }
	fetcher := newOperationsFetcher(client)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	require.Equal(t, clock.read().UnixMilli(), decodeTestCursor(t, cursor).UpdatedThrough, "the baseline is on Jira's clock")

	clock.advance(time.Second)
	created := site.Create(jirasearchfake.Operations, "Created right after the baseline")
	page, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	require.Equal(t, []string{created}, changedKeys(page), "a skewed worker clock does not hide a new issue")
}

func TestIssueChangeFetchWaitsForRetryAfterOnAnOverloadedSearch(t *testing.T) {
	site := jirasearchfake.New(t)
	client, _ := newTriggerTestClient(t, site)
	fetcher := newOperationsFetcher(client)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	site.OverloadNextRequests(1, "11")

	_, err = fetcher.FetchChanges(context.Background(), cursor)

	var rateLimited *pollingtrigger.RateLimitedError
	require.ErrorAs(t, err, &rateLimited)
	require.Equal(t, 11*time.Second, rateLimited.RetryAfter)
}
