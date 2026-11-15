// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package monday

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/monday/go/internal/mondayboardfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
)

const fetcherTestBoardID = "1234567890"

var fetcherTestConnection = sdkgo.ConnectionRef{Provider: "monday", Name: "monday-board-watch"}

func newFetcherTestClient(t *testing.T, apiURL string, options ...Option) *Client {
	t.Helper()
	client, err := New(Config{PollInterval: minimumPollInterval}, sdkgo.StaticCredentialProvider[Credentials]{
		fetcherTestConnection: {AuthMethodID: PersonalAPITokenAuthMethodID, APIToken: sdkgo.NewSecretString(mondayboardfake.Token)},
	}, append([]Option{WithAPIURL(apiURL)}, options...)...)
	require.NoError(t, err)
	return client
}

func newTestItemChangeFetcher(t *testing.T, board *mondayboardfake.Board, options ...Option) *itemChangeFetcher {
	t.Helper()
	return newItemChangeFetcher(newFetcherTestClient(t, board.URL, options...), fetcherTestConnection, ItemChangedTriggerConfiguration{BoardID: fetcherTestBoardID})
}

// fetchAll follows HasMorePages from cursor, committing each page as the kit does.
func fetchAll(t *testing.T, fetcher *itemChangeFetcher, cursor pollingtrigger.Cursor) ([]sdkgo.TriggerEvent[ItemChangedEvent], pollingtrigger.Cursor) {
	t.Helper()
	var events []sdkgo.TriggerEvent[ItemChangedEvent]
	for {
		page, err := fetcher.FetchChanges(context.Background(), cursor)
		require.NoError(t, err)
		events, cursor = append(events, page.Changes...), page.Next
		if !page.HasMorePages {
			return events, cursor
		}
	}
}

func TestItemChangeFetcherSeedsWithoutBacklogAndDeliversChangesOldestFirst(t *testing.T) {
	board := mondayboardfake.New(t, fetcherTestBoardID)
	board.Create("5550001", "Existing request")
	board.Create("5550002", "Second existing request")
	fetcher := newTestItemChangeFetcher(t, board)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	require.JSONEq(t, `{"boardId":"1234567890","watermark":"`+board.UpdatedAt("5550002").Format(time.RFC3339)+`"}`, cursor.Token)
	events, cursor := fetchAll(t, fetcher, cursor)
	require.Empty(t, events, "existing items are the baseline")

	board.Create("5550003", "New request")
	board.SetStatus("5550001", "Done")
	events, cursor = fetchAll(t, fetcher, cursor)
	require.Len(t, events, 2)
	require.Equal(t, "5550003", events[0].Payload.Item.ID)
	require.Equal(t, ItemChangeCreated, events[0].Payload.ChangeKind)
	require.Equal(t, "5550001", events[1].Payload.Item.ID)
	require.Equal(t, ItemChangeUpdated, events[1].Payload.ChangeKind)
	require.Equal(t, "Done", events[1].Payload.Item.ColumnValues[0].Text)
	require.Equal(t, board.UpdatedAt("5550001"), events[1].OccurredAt)

	again, _ := fetchAll(t, fetcher, cursor)
	require.Empty(t, again, "the snapshot keeps items inside the lookback window from arriving twice")
	refetched, _ := fetchAll(t, newTestItemChangeFetcher(t, board), pollingtrigger.Cursor{Token: `{"boardId":"1234567890","watermark":"` + board.UpdatedAt("5550002").Format(time.RFC3339) + `"}`})
	refetchedIDs := map[string]string{}
	for _, event := range refetched {
		refetchedIDs[event.Payload.Item.ID] = event.ID
	}
	require.Equal(t, events[0].ID, refetchedIDs["5550003"], "a refetch yields the same Trigger event ID")
	require.Equal(t, events[1].ID, refetchedIDs["5550001"])
}

func TestItemChangeFetcherDeliversASecondChangeWithinTheSameSecond(t *testing.T) {
	board := mondayboardfake.New(t, fetcherTestBoardID)
	board.Create("5550001", "Request")
	fetcher := newTestItemChangeFetcher(t, board)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	board.SetStatus("5550001", "Stuck")
	first, cursor := fetchAll(t, fetcher, cursor)
	require.Len(t, first, 1)
	board.SetClock(board.UpdatedAt("5550001"))
	board.SetStatus("5550001", "Done")
	second, _ := fetchAll(t, fetcher, cursor)
	require.Len(t, second, 1)
	require.Equal(t, "Done", second[0].Payload.Item.ColumnValues[0].Text)
	require.NotEqual(t, first[0].ID, second[0].ID)
}

func TestItemChangeFetcherTreatsEveryItemOfAnEmptyBaselineBoardAsCreated(t *testing.T) {
	board := mondayboardfake.New(t, fetcherTestBoardID)
	fetcher := newTestItemChangeFetcher(t, board)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	require.JSONEq(t, `{"boardId":"1234567890","watermark":"0001-01-01T00:00:00Z"}`, cursor.Token)
	board.Create("5550001", "First request")
	events, _ := fetchAll(t, fetcher, cursor)
	require.Len(t, events, 1)
	require.Equal(t, ItemChangeCreated, events[0].Payload.ChangeKind)
}

func TestItemChangeFetcherSplitsABurstIntoBatchesFromOneScan(t *testing.T) {
	board := mondayboardfake.New(t, fetcherTestBoardID)
	board.Create("5550000", "Existing")
	fetcher := newTestItemChangeFetcher(t, board)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	for index := 1; index <= 150; index++ {
		board.Create(strconv.Itoa(5550000+index), "Imported "+strconv.Itoa(index))
	}
	requestsBefore := len(board.Requests())
	page, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	require.Len(t, page.Changes, maximumItemChangesPerPage)
	require.True(t, page.HasMorePages)
	require.Equal(t, "5550001", page.Changes[0].Payload.Item.ID)
	scanRequests := len(board.Requests()) - requestsBefore
	require.Equal(t, 2, scanRequests, "two pages reach the item before the cursor")
	rest, err := fetcher.FetchChanges(context.Background(), page.Next)
	require.NoError(t, err)
	require.Len(t, rest.Changes, 50)
	require.False(t, rest.HasMorePages)
	require.Equal(t, "5550150", rest.Changes[49].Payload.Item.ID)
	require.Len(t, board.Requests(), requestsBefore+scanRequests, "the rest of the burst comes from the same scan")
}

func TestItemChangeFetcherPausesTheConnectionForRateAndDailyLimits(t *testing.T) {
	board := mondayboardfake.New(t, fetcherTestBoardID)
	now := time.Date(2026, 3, 2, 21, 30, 0, 0, time.UTC)
	fetcher := newTestItemChangeFetcher(t, board, WithClock(func() time.Time { return now }))
	board.RateLimitNextRequests(1, 7)
	_, err := fetcher.SeedCursor(context.Background())
	var rateLimited *pollingtrigger.RateLimitedError
	require.ErrorAs(t, err, &rateLimited)
	require.Equal(t, 7*time.Second, rateLimited.RetryAfter)

	board.ExhaustDailyLimitForNextRequests(1)
	_, err = fetcher.FetchChanges(context.Background(), pollingtrigger.Cursor{Token: `{"boardId":"1234567890","watermark":"2026-03-02T09:00:00Z"}`})
	require.ErrorAs(t, err, &rateLimited)
	require.Equal(t, 150*time.Minute, rateLimited.RetryAfter, "the daily call limit resets at midnight UTC")
}

func TestItemChangeFetcherMarksForeignAndOversizedCursorsInvalid(t *testing.T) {
	board := mondayboardfake.New(t, fetcherTestBoardID)
	fetcher := newTestItemChangeFetcher(t, board)
	for _, token := range []string{"not json", `{"boardId":"999","watermark":"2026-03-02T09:00:00Z"}`} {
		_, err := fetcher.FetchChanges(context.Background(), pollingtrigger.Cursor{Token: token})
		require.True(t, pollingtrigger.IsCursorInvalid(err), token)
	}

	for index := 1; index <= maximumItemScanPages*itemPollPageSize+1; index++ {
		board.Create(strconv.Itoa(6000000+index), "Bulk import")
	}
	_, err := fetcher.FetchChanges(context.Background(), pollingtrigger.Cursor{Token: `{"boardId":"1234567890","watermark":"2026-03-02T09:00:00Z"}`})
	require.True(t, pollingtrigger.IsCursorInvalid(err), "more changes than one scan reads record a new baseline")
	require.ErrorIs(t, err, errItemScanExceeded)
}

func TestItemChangedTriggerConfigurationRequiresANumericBoardID(t *testing.T) {
	require.NoError(t, ItemChangedTriggerConfiguration{BoardID: "1234567890"}.Validate())
	for _, boardID := range []string{"", " 1234567890", "board", "0123"} {
		require.Error(t, ItemChangedTriggerConfiguration{BoardID: boardID}.Validate(), boardID)
	}
}

func TestItemChangeBatchesStopAtTheCountOrByteBound(t *testing.T) {
	changes := func(count int, encodedBytes int) []itemChange {
		batch := make([]itemChange, count)
		for index := range batch {
			batch[index].polled.encodedBytes = encodedBytes
		}
		return batch
	}
	require.Equal(t, 40, itemChangeBatchSize(changes(40, 1024)))
	require.Equal(t, maximumItemChangesPerPage, itemChangeBatchSize(changes(150, 1024)))
	require.Equal(t, 4, itemChangeBatchSize(changes(10, maximumItemChangeBatchBytes/4)), "a raised response limit cannot overfill the inbox")
	require.Equal(t, 1, itemChangeBatchSize(changes(3, maximumItemChangeBatchBytes+1)), "an oversized item still moves alone")
}
