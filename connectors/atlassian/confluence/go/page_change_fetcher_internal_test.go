// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package confluence

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/confluence/go/internal/confluencepagefake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
)

const fakeSiteCloudID = "11223344-a1b2-4b33-8c44-def123456789"

var triggerTestConnection = sdkgo.ConnectionRef{Provider: "atlassian", Name: "confluence-trigger-test"}

type pageChangeFetcherFixture struct {
	site    *confluencepagefake.Site
	client  *Client
	fetcher *pageChangeFetcher
}

func newPageChangeFetcherFixture(t *testing.T, configuredCloudID string, spaceKey string) *pageChangeFetcherFixture {
	t.Helper()
	site := confluencepagefake.New(t, fakeSiteCloudID)
	client, err := New(Config{Endpoint: site.URL, CloudID: configuredCloudID}, sdkgo.StaticCredentialProvider[Credentials]{triggerTestConnection: {
		OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"),
		AccessToken: sdkgo.NewSecretString(confluencepagefake.AccessToken), RefreshToken: sdkgo.NewSecretString("refresh-token"),
	}})
	require.NoError(t, err)
	configuration := PageChangedTriggerConfiguration{SpaceKey: spaceKey}
	return &pageChangeFetcherFixture{site: site, client: client, fetcher: newPageChangeFetcher(client, triggerTestConnection, configuration)}
}

func (fixture *pageChangeFetcherFixture) seed(t *testing.T) pollingtrigger.Cursor {
	t.Helper()
	cursor, err := fixture.fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	return cursor
}

func (fixture *pageChangeFetcherFixture) fetch(t *testing.T, cursor pollingtrigger.Cursor) pollingtrigger.ChangePage[sdkgo.TriggerEvent[PageChangedEvent]] {
	t.Helper()
	page, err := fixture.fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	return page
}

func TestPageChangesSkipTheBaselineAndDeliverEachNewVersionOnce(t *testing.T) {
	fixture := newPageChangeFetcherFixture(t, "", "")
	existing := fixture.site.Publish("OPS", "Existing policy", "Before the Trigger started")
	cursor := fixture.seed(t)
	require.Len(t, cursor.Snapshot.Entries, 1, "a page edited inside the lookback window is baselined")

	page := fixture.fetch(t, cursor)
	require.Empty(t, page.Changes, "the baseline is never delivered")
	require.False(t, page.HasMorePages)
	cursor = page.Next

	created := fixture.site.Publish("OPS", "Remote work policy", "Two days a week")
	fixture.site.Edit(existing, "Existing policy", "Revised")
	page = fixture.fetch(t, cursor)
	require.Len(t, page.Changes, 2)
	byPage := map[string]sdkgo.TriggerEvent[PageChangedEvent]{}
	for _, change := range page.Changes {
		require.Equal(t, fakeSiteCloudID, change.Payload.CloudID, "the site is found through accessible resources")
		byPage[change.Payload.Page.ID] = change
	}
	require.Equal(t, PageChangeCreated, byPage[created].Payload.ChangeKind)
	require.Equal(t, PageChangeUpdated, byPage[existing].Payload.ChangeKind)
	require.Equal(t, 2, byPage[existing].Payload.Page.VersionNumber)
	require.Equal(t, "https://ops.atlassian.net/wiki/spaces/OPS/pages/"+created, byPage[created].Payload.Page.WebURL)
	require.False(t, page.Changes[0].OccurredAt.After(page.Changes[1].OccurredAt), "changes are delivered oldest first")

	refetched := fixture.fetch(t, cursor)
	require.Equal(t, eventIDs(page.Changes), eventIDs(refetched.Changes), "a refetch from the same cursor yields the same event IDs")
	require.Empty(t, fixture.fetch(t, page.Next).Changes, "a committed version is not delivered again")
}

func TestPageChangesFollowSearchCursorsAndRestartAnExpiredOne(t *testing.T) {
	fixture := newPageChangeFetcherFixture(t, fakeSiteCloudID, "OPS")
	cursor := fixture.fetch(t, fixture.seed(t)).Next
	for index := range 150 {
		fixture.site.Publish("OPS", "Runbook "+strconv.Itoa(index), "Step one")
	}
	fixture.site.Publish("HR", "Other space", "Not watched")

	first := fixture.fetch(t, cursor)
	require.Len(t, first.Changes, pageChangeSearchPageSize)
	require.True(t, first.HasMorePages)

	fixture.site.ExpireSearchCursors()
	restarted := fixture.fetch(t, first.Next)
	require.Empty(t, restarted.Changes)
	require.True(t, restarted.HasMorePages, "an expired search cursor restarts the scan instead of failing")

	again := fixture.fetch(t, restarted.Next)
	require.Empty(t, again.Changes, "the restarted first page holds only delivered versions")
	last := fixture.fetch(t, again.Next)
	require.Len(t, last.Changes, 50)
	require.False(t, last.HasMorePages)
	delivered := append(eventIDs(first.Changes), eventIDs(last.Changes)...)
	require.Len(t, delivered, 150)
	for _, query := range fixture.site.CQLQueries() {
		require.Contains(t, query, `type = page AND space = "OPS" AND lastmodified >= "`)
		require.Contains(t, query, `ORDER BY lastmodified DESC`)
	}
}

func TestPageChangesPruneVersionsOlderThanTheNextWindow(t *testing.T) {
	fixture := newPageChangeFetcherFixture(t, fakeSiteCloudID, "")
	fixture.site.Publish("OPS", "Existing policy", "Before")
	cursor := fixture.seed(t)
	require.Len(t, cursor.Snapshot.Entries, 1)
	fixture.client.now = func() time.Time { return time.Now().Add(time.Hour) }
	page := fixture.fetch(t, cursor)
	require.Empty(t, page.Changes)
	page = fixture.fetch(t, page.Next)
	require.Empty(t, page.Next.Snapshot.Entries, "a version older than the next window cannot be returned again")
}

// TestFullSnapshotFitsProjectObjectStorage: at its entry bound the snapshot still encodes well under the
// 4 MiB project object limit, so a burst of edits evicts entries instead of pausing the Trigger.
func TestFullSnapshotFitsProjectObjectStorage(t *testing.T) {
	items := make([]pollingtrigger.KeyedItem, 0, pageChangeSnapshotOptions.MaxEntries+1)
	modifiedAt := time.Date(2026, 11, 15, 10, 20, 30, 123000000, time.UTC)
	for index := range pageChangeSnapshotOptions.MaxEntries + 1 {
		items = append(items, newSeenPageItem(PageSummary{ID: strconv.Itoa(9_000_000_000 + index), VersionNumber: 1234, LastModifiedAt: modifiedAt}))
	}
	snapshot, err := pollingtrigger.ApplyKeyedSnapshotChanges(nil, items, nil, pageChangeSnapshotOptions)
	require.NoError(t, err)
	require.Len(t, snapshot.Entries, pageChangeSnapshotOptions.MaxEntries, "the least recently seen entry is evicted")
	encoded, err := json.Marshal(snapshot)
	require.NoError(t, err)
	require.Less(t, len(encoded), 2<<20)
}

func TestPageChangesReportRateLimitsAndInvalidCursors(t *testing.T) {
	fixture := newPageChangeFetcherFixture(t, fakeSiteCloudID, "OPS")
	cursor := fixture.seed(t)
	fixture.site.ThrottleSearches(1, 7)
	_, err := fixture.fetcher.FetchChanges(context.Background(), cursor)
	var rateLimited *pollingtrigger.RateLimitedError
	require.True(t, errors.As(err, &rateLimited))
	require.Equal(t, 7*time.Second, rateLimited.RetryAfter)
	require.NotContains(t, err.Error(), confluencepagefake.AccessToken)

	_, err = fixture.fetcher.FetchChanges(context.Background(), pollingtrigger.Cursor{Token: "not json"})
	require.True(t, pollingtrigger.IsCursorInvalid(err))
	otherSpace := newPageChangeFetcher(fixture.client, triggerTestConnection, PageChangedTriggerConfiguration{SpaceKey: "HR"})
	_, err = otherSpace.FetchChanges(context.Background(), cursor)
	require.True(t, pollingtrigger.IsCursorInvalid(err), "a changed space records a new baseline")
}

func TestStaleSearchResultsNeverRepeatADeliveredVersion(t *testing.T) {
	snapshot, err := pollingtrigger.ApplyKeyedSnapshotChanges(nil, []pollingtrigger.KeyedItem{
		newSeenPageItem(PageSummary{ID: "600001", VersionNumber: 3, LastModifiedAt: time.Now()}),
	}, nil, pageChangeSnapshotOptions)
	require.NoError(t, err)
	require.True(t, isSeenPageVersion(snapshot, PageSummary{ID: "600001", VersionNumber: 2}), "a lagging index returning an older version")
	require.True(t, isSeenPageVersion(snapshot, PageSummary{ID: "600001", VersionNumber: 3}))
	require.False(t, isSeenPageVersion(snapshot, PageSummary{ID: "600001", VersionNumber: 4}))
	require.False(t, isSeenPageVersion(snapshot, PageSummary{ID: "600002", VersionNumber: 1}))
}

func TestPageChangedConfigurationAndCQL(t *testing.T) {
	require.NoError(t, PageChangedTriggerConfiguration{}.Validate())
	require.NoError(t, PageChangedTriggerConfiguration{SpaceKey: "~5b10ac8d82e05b22cc7d4ef5"}.Validate())
	require.Error(t, PageChangedTriggerConfiguration{SpaceKey: " OPS"}.Validate())
	require.Error(t, PageChangedTriggerConfiguration{SpaceKey: `OPS" OR space = "HR`}.Validate())
	windowStart := time.Date(2026, 11, 15, 10, 30, 45, 0, time.UTC)
	require.Equal(t, `type = page AND space = "OPS" AND lastmodified >= "2026-11-14 22:30" ORDER BY lastmodified DESC`, buildPageChangeCQL("OPS", windowStart))
	require.Equal(t, `type = page AND lastmodified >= "2026-11-14 22:30" ORDER BY lastmodified DESC`, buildPageChangeCQL("", windowStart))
	_, err := New(Config{PollInterval: 9 * time.Second}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.ErrorContains(t, err, "pollInterval must be at least 10s")
}

func eventIDs(events []sdkgo.TriggerEvent[PageChangedEvent]) []string {
	ids := make([]string, 0, len(events))
	for _, event := range events {
		ids = append(ids, event.ID)
	}
	return ids
}
