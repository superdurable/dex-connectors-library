// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package notion

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/notion/go/internal/notionrowsfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

const (
	rowsDataSourceID = "2a4d6f80-1b3c-4d5e-8f70-112233445566"
	rowsDatabaseID   = "2a4d6f80-1b3c-4d5e-8f70-112233445500"
	rowA             = "2a4d6f80-1b3c-4d5e-8f70-0000000000a1"
	rowB             = "2a4d6f80-1b3c-4d5e-8f70-0000000000b2"
	rowC             = "2a4d6f80-1b3c-4d5e-8f70-0000000000c3"
)

var rowsBaseTime = time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)

func minute(offset int) time.Time { return rowsBaseTime.Add(time.Duration(offset) * time.Minute) }

func newRowsTestClient(t *testing.T, fake *notionrowsfake.DataSource, pageSize int) *Client {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "notion", Name: "rows"}
	client, err := New(Config{Endpoint: fake.URL, PollInterval: minimumPollInterval, PollPageSize: int64(pageSize)},
		sdkgo.StaticCredentialProvider[Credentials]{reference: {APIToken: sdkgo.NewSecretString(notionrowsfake.Token)}})
	require.NoError(t, err)
	client.now = func() time.Time { return minute(30) }
	return client
}

func newRowsTestFetcher(t *testing.T, fake *notionrowsfake.DataSource, pageSize int) *databaseRowFetcher {
	t.Helper()
	return newDatabaseRowFetcher(newRowsTestClient(t, fake, pageSize), sdkgo.ConnectionRef{Provider: "notion", Name: "rows"},
		DatabaseRowChangedTriggerConfiguration{DataSourceID: rowsDataSourceID})
}

// fetchAll follows HasMorePages from cursor, as the kit does within one poll.
func fetchAll(t *testing.T, fetcher *databaseRowFetcher, cursor pollingtrigger.Cursor) ([]sdkgo.TriggerEvent[DatabaseRowChangedEvent], pollingtrigger.Cursor) {
	t.Helper()
	var events []sdkgo.TriggerEvent[DatabaseRowChangedEvent]
	for range 20 {
		page, err := fetcher.FetchChanges(context.Background(), cursor)
		require.NoError(t, err)
		require.LessOrEqual(t, len(page.Changes), pollingtrigger.MaxChangesPerPage)
		events, cursor = append(events, page.Changes...), page.Next
		if !page.HasMorePages {
			return events, cursor
		}
	}
	t.Fatal("the fetcher never finished its query sequence")
	return nil, cursor
}

func rowTitles(events []sdkgo.TriggerEvent[DatabaseRowChangedEvent]) []string {
	titles := make([]string, 0, len(events))
	for _, event := range events {
		titles = append(titles, event.Payload.Row.Title+":"+string(event.Payload.ChangeKind))
	}
	return titles
}

func TestDatabaseRowBaselineDeliversNoExistingRowAndThenEachNewVersionOnce(t *testing.T) {
	fake := notionrowsfake.New(t, rowsDataSourceID, rowsDatabaseID)
	fake.Create(rowA, "Existing old", minute(0))
	fake.Create(rowB, "Existing newest", minute(10))
	fetcher := newRowsTestFetcher(t, fake, 100)
	seed, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	requests := fake.Requests()
	require.Len(t, requests, 2)
	require.Equal(t, "descending", requests[0].Body.Sorts[0].Direction)
	require.Equal(t, 1, requests[0].Body.PageSize)
	require.Equal(t, "2026-11-02T09:08:00Z", requests[1].Body.Filter.LastEditedTime.OnOrAfter, "the window starts one lookback before the newest edit")

	events, cursor := fetchAll(t, fetcher, seed)
	require.Empty(t, events, "the baseline delivers no existing row")

	fake.Create(rowC, "New", minute(11))
	fake.Edit(rowA, "Existing edited", minute(12))
	events, next := fetchAll(t, fetcher, cursor)
	require.Equal(t, []string{"New:created", "Existing edited:updated"}, rowTitles(events))
	require.Equal(t, rowsDataSourceID, events[0].Payload.DataSourceID)
	require.Equal(t, minute(11), events[0].OccurredAt)

	refetched, _ := fetchAll(t, fetcher, cursor)
	require.Equal(t, []string{events[0].ID, events[1].ID}, []string{refetched[0].ID, refetched[1].ID}, "event IDs are a pure function of the row version")
	again, _ := fetchAll(t, fetcher, next)
	require.Empty(t, again, "committed versions are not delivered again")
	require.Equal(t, "2026-11-02T09:10:00Z", fake.Requests()[len(fake.Requests())-1].Body.Filter.LastEditedTime.OnOrAfter)
}

func TestDatabaseRowSharedTimestampsAndLateIndexedRowsAreDeliveredOnce(t *testing.T) {
	fake := notionrowsfake.New(t, rowsDataSourceID, rowsDatabaseID)
	fetcher := newRowsTestFetcher(t, fake, 100)
	seed, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	fake.Create(rowA, "A", minute(31))
	fake.Create(rowB, "B late", minute(31))
	fake.HideFromQueries(rowB)
	events, cursor := fetchAll(t, fetcher, seed)
	require.Equal(t, []string{"A:created"}, rowTitles(events))

	fake.Create(rowC, "C", minute(32))
	events, cursor = fetchAll(t, fetcher, cursor)
	require.Equal(t, []string{"C:created"}, rowTitles(events), "the newest edit moves the high water past B's minute")
	fake.RevealToQueries(rowB)
	events, _ = fetchAll(t, fetcher, cursor)
	require.Equal(t, []string{"B late:created"}, rowTitles(events), "a row indexed late inside the lookback is still delivered")
}

func TestDatabaseRowQueryPagesThroughABurstAndRestartsAfterAStaleStartCursor(t *testing.T) {
	fake := notionrowsfake.New(t, rowsDataSourceID, rowsDatabaseID)
	fetcher := newRowsTestFetcher(t, fake, 1)
	seed, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	fake.Create(rowA, "A", minute(31))
	fake.Create(rowB, "B", minute(31))
	fake.Create(rowC, "C", minute(31))
	first, err := fetcher.FetchChanges(context.Background(), seed)
	require.NoError(t, err)
	require.True(t, first.HasMorePages)
	require.Contains(t, first.Next.Token, `"startCursor":"rows-1"`)

	fake.ExpireStartCursors()
	restarted, err := fetcher.FetchChanges(context.Background(), first.Next)
	require.NoError(t, err)
	require.Empty(t, restarted.Changes)
	require.True(t, restarted.HasMorePages)
	require.NotContains(t, restarted.Next.Token, "startCursor", "the rejected start cursor is dropped, not the high water")

	afterRestart, _ := fetchAll(t, fetcher, restarted.Next)
	titles := rowTitles(afterRestart)
	require.Equal(t, []string{"A:created"}, rowTitles(first.Changes))
	require.ElementsMatch(t, []string{"B:created", "C:created"}, titles, "every row is delivered once across the restart")
}

func TestDatabaseRowTrashedRowsAreNotDelivered(t *testing.T) {
	fake := notionrowsfake.New(t, rowsDataSourceID, rowsDatabaseID)
	fake.Create(rowA, "A", minute(0))
	fetcher := newRowsTestFetcher(t, fake, 100)
	seed, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	fake.Trash(rowA, minute(31))
	events, _ := fetchAll(t, fetcher, seed)
	require.Empty(t, events)
}

func TestDatabaseRowFailuresAreClassifiedForTheKit(t *testing.T) {
	fake := notionrowsfake.New(t, rowsDataSourceID, rowsDatabaseID)
	fetcher := newRowsTestFetcher(t, fake, 100)
	seed, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)

	fake.RateLimitNextRequests(1, "7")
	_, err = fetcher.FetchChanges(context.Background(), seed)
	var rateLimited *pollingtrigger.RateLimitedError
	require.ErrorAs(t, err, &rateLimited)
	require.Equal(t, 7*time.Second, rateLimited.RetryAfter)

	for name, token := range map[string]string{
		"garbage":            "not json",
		"other data source":  `{"v":1,"dataSourceId":"2a4d6f80-1b3c-4d5e-8f70-999999999999","highWater":"2026-11-02T09:00:00Z"}`,
		"unknown version":    `{"v":9,"dataSourceId":"` + rowsDataSourceID + `","highWater":"2026-11-02T09:00:00Z"}`,
		"missing high water": `{"v":1,"dataSourceId":"` + rowsDataSourceID + `"}`,
	} {
		_, err := fetcher.FetchChanges(context.Background(), pollingtrigger.Cursor{Token: token})
		require.True(t, pollingtrigger.IsCursorInvalid(err), name)
	}

	unshared := newDatabaseRowFetcher(newRowsTestClient(t, fake, 100), sdkgo.ConnectionRef{Provider: "notion", Name: "rows"},
		DatabaseRowChangedTriggerConfiguration{DataSourceID: "2a4d6f80-1b3c-4d5e-8f70-999999999999"})
	_, err = unshared.SeedCursor(context.Background())
	require.ErrorContains(t, err, "share its database with the connection")
	require.False(t, pollingtrigger.IsCursorInvalid(err), "an unshared data source backs off instead of reseeding")
	require.False(t, errors.As(err, &rateLimited))
	require.Equal(t, http.StatusNotFound, fake.Requests()[len(fake.Requests())-1].Status)
}

func TestDatabaseRowSeenRowsStayBoundedInsideTheTokenLimit(t *testing.T) {
	seen := newSeenRows(nil)
	for index := range MaximumSeenDatabaseRows + 100 {
		seen.add(minute(0).Add(time.Duration(index%90)*time.Second), "row-"+time.Duration(index).String())
	}
	seen.add(minute(-5), "too-old")
	groups := seen.groupsWithin(minute(1))
	count := 0
	for _, group := range groups {
		count += len(group.RowKeys)
		require.False(t, group.LastEditedTime.Before(minute(-1)))
	}
	require.Equal(t, MaximumSeenDatabaseRows, count)
	cursor, err := encodeDatabaseRowCursor(databaseRowCursor{Version: 1, DataSourceID: rowsDataSourceID, HighWater: minute(1), Seen: groups})
	require.NoError(t, err)
	require.Less(t, len(cursor.Token), projectconfig.MaxPollCursorTokenBytes)
}

func TestDatabaseRowConfigurationAndPollSettingsAreValidated(t *testing.T) {
	require.ErrorContains(t, DatabaseRowChangedTriggerConfiguration{}.Validate(), "dataSourceId is required")
	require.Error(t, DatabaseRowChangedTriggerConfiguration{DataSourceID: "not-an-id"}.Validate())
	require.NoError(t, DatabaseRowChangedTriggerConfiguration{DataSourceID: "https://www.notion.so/2a4d6f801b3c4d5e8f70112233445566"}.Validate())
	credentials := sdkgo.StaticCredentialProvider[Credentials]{}
	_, err := New(Config{PollInterval: time.Second}, credentials)
	require.ErrorContains(t, err, "pollInterval must be at least 5s")
	_, err = New(Config{PollPageSize: 101}, credentials)
	require.ErrorContains(t, err, "pollPageSize must be between 1 and 100")
}

func TestDatabaseRowSeenRowsWithDistinctMillisecondTimesStayInsideTheTokenLimit(t *testing.T) {
	seen := newSeenRows(nil)
	for index := range MaximumSeenDatabaseRows {
		seen.add(minute(0).Add(time.Duration(index)*50*time.Millisecond), "row-"+time.Duration(index).String())
	}
	groups := seen.groupsWithin(minute(2))
	cursor, err := encodeDatabaseRowCursor(databaseRowCursor{Version: 1, DataSourceID: rowsDataSourceID, HighWater: minute(2), Seen: groups})
	require.NoError(t, err)
	require.Less(t, len(cursor.Token), projectconfig.MaxPollCursorTokenBytes, "one group per row must not overflow the kit's token bound")
	require.Equal(t, minute(0).Add(time.Duration(MaximumSeenDatabaseRows-1)*50*time.Millisecond), groups[len(groups)-1].LastEditedTime,
		"the newest row versions are the ones kept")
}

func TestDatabaseRowEmptyDataSourceBaselineDoesNotDependOnTheLocalClock(t *testing.T) {
	fake := notionrowsfake.New(t, rowsDataSourceID, rowsDatabaseID)
	fetcher := newRowsTestFetcher(t, fake, 100)
	fetcher.client.now = func() time.Time { return minute(60) }
	seed, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	fake.Create(rowA, "First row", minute(31))
	events, _ := fetchAll(t, fetcher, seed)
	require.Equal(t, []string{"First row:created"}, rowTitles(events), "a host clock 29 minutes ahead of Notion loses no row")
}
