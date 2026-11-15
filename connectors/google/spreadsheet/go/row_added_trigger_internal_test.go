// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package spreadsheet

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/spreadsheet/go/internal/sheetsfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
)

var leadsHeader = []string{"email", "name", "status"}

func newRowAddedTestFetcher(t *testing.T, sheet *sheetsfake.Sheet, config Config) *rowAddedFetcher {
	t.Helper()
	return newRowAddedTestFetcherForTab(t, sheet, config, "Leads 2026")
}

func newRowAddedTestFetcherForTab(t *testing.T, sheet *sheetsfake.Sheet, config Config, sheetName string) *rowAddedFetcher {
	t.Helper()
	config.Endpoint = sheet.URL
	reference := sdkgo.ConnectionRef{Provider: "google", Name: "sheets-leads"}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {AccessToken: sdkgo.NewSecretString("SENTINEL-ACCESS-TOKEN")},
	}, withClock(func() time.Time { return time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC) }))
	require.NoError(t, err)
	binding := sdkgo.TriggerBindingRef{Connection: reference, Trigger: RowAddedTriggerDefinition.Trigger, Name: "new-leads"}
	return client.newRowAddedFetcher(binding, RowAddedTriggerConfiguration{SpreadsheetID: " sheet-1 ", SheetName: sheetName})
}

func requireCursorRow(t *testing.T, cursor pollingtrigger.Cursor, wantLastRow int64, wantCells []string) {
	t.Helper()
	token, err := decodeRowAddedCursorToken(cursor.Token)
	require.NoError(t, err)
	require.Equal(t, rowAddedCursorToken{Version: 1, LastRow: wantLastRow, RowHash: hashRowCells(wantCells)}, token)
}

func rowNumbers(page pollingtrigger.ChangePage[sdkgo.TriggerEvent[RowAddedEvent]]) []int64 {
	numbers := []int64{}
	for _, change := range page.Changes {
		numbers = append(numbers, change.Payload.RowNumber)
	}
	return numbers
}

func TestRowAddedSeedsAfterTheLastRowAndDeliversAppendedRowsInOrder(t *testing.T) {
	sheet := sheetsfake.New(t, "Leads 2026", leadsHeader, []string{"ada@example.test", "Ada", "new"})
	fetcher := newRowAddedTestFetcher(t, sheet, Config{})
	ctx := context.Background()

	seed, err := fetcher.SeedCursor(ctx)
	require.NoError(t, err)
	requireCursorRow(t, seed, 2, []string{"ada@example.test", "Ada", "new"})
	page, err := fetcher.FetchChanges(ctx, seed)
	require.NoError(t, err)
	require.Empty(t, page.Changes, "existing rows are never delivered")
	require.Equal(t, seed, page.Next)

	sheet.AppendRows([]string{"grace@example.test", "Grace"}, []string{}, []string{"", "", "", ""}, []string{"linus@example.test", "Linus", "new", "extra"})
	page, err = fetcher.FetchChanges(ctx, seed)
	require.NoError(t, err)
	require.Equal(t, []int64{3, 6}, rowNumbers(page), "blank rows are skipped")
	require.False(t, page.HasMorePages)
	requireCursorRow(t, page.Next, 6, []string{"linus@example.test", "Linus", "new", "extra"})
	first := page.Changes[0]
	require.Equal(t, RowAddedEvent{
		SpreadsheetID: "sheet-1", SheetName: "Leads 2026", RowNumber: 3,
		Values: map[string]string{"email": "grace@example.test", "name": "Grace", "status": ""},
		Cells:  []string{"grace@example.test", "Grace"},
	}, first.Payload)
	require.Regexp(t, `^row-3-[0-9a-f]{16}$`, first.ID)
	require.Equal(t, time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC), first.OccurredAt)

	refetched, err := fetcher.FetchChanges(ctx, seed)
	require.NoError(t, err)
	require.Equal(t, page.Changes[0].ID, refetched.Changes[0].ID, "a refetch after a crash yields the same event IDs")
	require.Equal(t, page.Changes[1].ID, refetched.Changes[1].ID)

	requests := sheet.Requests()
	require.Equal(t, sheetsfake.Request{
		SpreadsheetID: "sheet-1", Ranges: []string{"'Leads 2026'!1:1", "'Leads 2026'!2:102"}, Authorization: "Bearer SENTINEL-ACCESS-TOKEN",
	}, requests[len(requests)-1])
}

func TestRowAddedSeedsAnEmptyOrHeaderOnlyTabAtTheHeaderRow(t *testing.T) {
	sheet := sheetsfake.New(t, "Leads 2026")
	fetcher := newRowAddedTestFetcher(t, sheet, Config{})
	seed, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	requireCursorRow(t, seed, 1, nil)

	sheet.AppendRows(leadsHeader, []string{"ada@example.test"})
	page, err := fetcher.FetchChanges(context.Background(), seed)
	require.NoError(t, err)
	require.Equal(t, []int64{2}, rowNumbers(page), "a header added after seeding is not delivered as a row")
	require.Equal(t, map[string]string{"email": "ada@example.test", "name": "", "status": ""}, page.Changes[0].Payload.Values)
}

func TestRowAddedSeedScansTheTabInMaxRowsWindows(t *testing.T) {
	rows := [][]string{leadsHeader}
	for index := range 5 {
		rows = append(rows, []string{"row", string(rune('a' + index))})
	}
	sheet := sheetsfake.New(t, "Leads 2026", rows...)
	fetcher := newRowAddedTestFetcher(t, sheet, Config{MaxRows: 2})
	seed, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	requireCursorRow(t, seed, 6, []string{"row", "e"})
	var scanned []string
	for _, request := range sheet.Requests() {
		scanned = append(scanned, request.Ranges...)
	}
	require.Equal(t, []string{"'Leads 2026'!1:2", "'Leads 2026'!3:4", "'Leads 2026'!5:6", "'Leads 2026'!7:8"}, scanned)
}

func TestRowAddedPagesLargeBatchesWithinPollPageSize(t *testing.T) {
	sheet := sheetsfake.New(t, "Leads 2026", leadsHeader)
	fetcher := newRowAddedTestFetcher(t, sheet, Config{PollPageSize: 2})
	seed, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	sheet.AppendRows([]string{"a"}, []string{"b"}, []string{"c"})
	page, err := fetcher.FetchChanges(context.Background(), seed)
	require.NoError(t, err)
	require.Equal(t, []int64{2, 3}, rowNumbers(page))
	require.True(t, page.HasMorePages)
	page, err = fetcher.FetchChanges(context.Background(), page.Next)
	require.NoError(t, err)
	require.Equal(t, []int64{4}, rowNumbers(page))
	require.False(t, page.HasMorePages)
}

func TestRowAddedRealignsAfterRowsAboveTheCursorAreDeletedOrInserted(t *testing.T) {
	sheet := sheetsfake.New(t, "Leads 2026", leadsHeader, []string{"a"}, []string{"b"}, []string{"c"})
	fetcher := newRowAddedTestFetcher(t, sheet, Config{})
	seed, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)

	sheet.DeleteRow(2)
	sheet.AppendRows([]string{"d"})
	page, err := fetcher.FetchChanges(context.Background(), seed)
	require.NoError(t, err)
	require.Equal(t, []int64{4}, rowNumbers(page), "row c moved from 4 to 3, so only d is new")
	require.Equal(t, []string{"d"}, page.Changes[0].Payload.Cells)
	requireCursorRow(t, page.Next, 4, []string{"d"})

	sheet.TruncateRows(1)
	sheet.AppendRows([]string{"new-1"}, []string{"b"}, []string{"c"}, []string{"d"}, []string{"e"})
	page, err = fetcher.FetchChanges(context.Background(), page.Next)
	require.NoError(t, err)
	require.Equal(t, []int64{6}, rowNumbers(page), "row d moved down from 4 to 5 after an insert above it")
}

func TestRowAddedKeepsTheCursorRowWhenItIsEditedInPlace(t *testing.T) {
	sheet := sheetsfake.New(t, "Leads 2026", leadsHeader, []string{"a", "new"})
	fetcher := newRowAddedTestFetcher(t, sheet, Config{})
	seed, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	sheet.ReplaceRow(2, []string{"a", "contacted"})
	sheet.AppendRows([]string{"b", "new"})
	page, err := fetcher.FetchChanges(context.Background(), seed)
	require.NoError(t, err)
	require.Equal(t, []int64{3}, rowNumbers(page))
	requireCursorRow(t, page.Next, 3, []string{"b", "new"})
}

func TestRowAddedReportsAnInvalidCursorWhenTheTabEndsAboveIt(t *testing.T) {
	sheet := sheetsfake.New(t, "Leads 2026", leadsHeader, []string{"a"}, []string{"b"}, []string{"c"})
	fetcher := newRowAddedTestFetcher(t, sheet, Config{})
	seed, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	sheet.TruncateRows(2)
	_, err = fetcher.FetchChanges(context.Background(), seed)
	require.True(t, pollingtrigger.IsCursorInvalid(err), "%v", err)

	for _, token := range []string{"", "{}", `{"v":1,"lastRow":0,"rowHash":""}`, `{"v":2,"lastRow":2,"rowHash":"` + hashRowCells(nil) + `"}`, `{"v":1,"lastRow":2,"rowHash":"zz","extra":1}`} {
		_, err := fetcher.FetchChanges(context.Background(), pollingtrigger.Cursor{Token: token})
		require.True(t, pollingtrigger.IsCursorInvalid(err), "token %q", token)
	}
}

func TestRowAddedClassifiesProviderFailuresWithoutLeakingSecrets(t *testing.T) {
	sheet := sheetsfake.New(t, "Leads 2026", leadsHeader)
	fetcher := newRowAddedTestFetcher(t, sheet, Config{})
	seed, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)

	sheet.Script(sheetsfake.ScriptedResponse{Status: http.StatusTooManyRequests, RetryAfter: "7"})
	_, err = fetcher.FetchChanges(context.Background(), seed)
	var rateLimited *pollingtrigger.RateLimitedError
	require.True(t, errors.As(err, &rateLimited))
	require.Equal(t, 7*time.Second, rateLimited.RetryAfter)

	sheet.Script(sheetsfake.ScriptedResponse{Status: http.StatusTooManyRequests, RetryAfter: "soon"})
	_, err = fetcher.FetchChanges(context.Background(), seed)
	require.True(t, errors.As(err, &rateLimited))
	require.Zero(t, rateLimited.RetryAfter, "an unreadable Retry-After falls back to exponential backoff")

	for _, status := range []int{http.StatusNotFound, http.StatusForbidden, http.StatusInternalServerError} {
		sheet.Script(sheetsfake.ScriptedResponse{Status: status})
		_, err = fetcher.FetchChanges(context.Background(), seed)
		require.Error(t, err)
		require.False(t, pollingtrigger.IsCursorInvalid(err), "HTTP %d keeps the cursor", status)
		require.False(t, errors.As(err, &rateLimited))
		require.NotContains(t, err.Error(), "SENTINEL")
		require.NotContains(t, err.Error(), "Leads")
	}
}

func TestRowAddedConfigurationAndPollSettingsAreValidated(t *testing.T) {
	require.Error(t, RowAddedTriggerConfiguration{SpreadsheetID: "sheet-1"}.Validate())
	require.Error(t, RowAddedTriggerConfiguration{SheetName: "Leads"}.Validate())
	require.NoError(t, RowAddedTriggerConfiguration{SpreadsheetID: "sheet-1", SheetName: "Leads"}.Validate())
	credentials := sdkgo.StaticCredentialProvider[Credentials]{}
	for _, config := range []Config{{PollInterval: 500 * time.Millisecond}, {PollPageSize: 501}, {PollPageSize: -1}} {
		_, err := New(config, credentials)
		require.Error(t, err, "%+v", config)
	}
	client, err := New(Config{}, credentials)
	require.NoError(t, err)
	require.Equal(t, 30*time.Second, client.pollInterval)
	require.Equal(t, 100, client.pollPageSize)
	require.Equal(t, pollingtrigger.PollSchedule{Interval: 30 * time.Second, MaxBackoff: 5 * time.Minute}, client.rowAddedPollSchedule())
}

func TestRowAddedUsesTheExactTabTitleIncludingOuterSpaces(t *testing.T) {
	sheet := sheetsfake.New(t, " Leads 2026 ", leadsHeader, []string{"a"})
	fetcher := newRowAddedTestFetcherForTab(t, sheet, Config{}, " Leads 2026 ")
	seed, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	sheet.AppendRows([]string{"b"})
	page, err := fetcher.FetchChanges(context.Background(), seed)
	require.NoError(t, err)
	require.Equal(t, []int64{3}, rowNumbers(page))
	require.Equal(t, " Leads 2026 ", page.Changes[0].Payload.SheetName)
	requests := sheet.Requests()
	require.Equal(t, []string{"' Leads 2026 '!1:1", "' Leads 2026 '!2:102"}, requests[len(requests)-1].Ranges)
}

func TestRowAddedSeedHalvesScanWindowsWhoseResponseIsTooLarge(t *testing.T) {
	rows := [][]string{leadsHeader}
	for index := range 300 {
		rows = append(rows, []string{fmt.Sprintf("row-%03d-padding-padding", index)})
	}
	sheet := sheetsfake.New(t, "Leads 2026", rows...)
	// A 400-row window returns all 301 rows, about 10 KB; a 200-row window stays under 7 KB.
	fetcher := newRowAddedTestFetcher(t, sheet, Config{MaxRows: 400, MaxResponseBytes: 7000})
	seed, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	requireCursorRow(t, seed, 301, rows[300])
	var scanned []string
	for _, request := range sheet.Requests() {
		scanned = append(scanned, request.Ranges...)
	}
	require.Equal(t, []string{"'Leads 2026'!1:400", "'Leads 2026'!1:200", "'Leads 2026'!201:400"}, scanned)

	tooSmall := newRowAddedTestFetcher(t, sheet, Config{MaxRows: 400, MaxResponseBytes: 1000})
	_, err = tooSmall.SeedCursor(context.Background())
	require.ErrorContains(t, err, "provider response exceeds configured limit", "a window at the minimum size still fails")
}
