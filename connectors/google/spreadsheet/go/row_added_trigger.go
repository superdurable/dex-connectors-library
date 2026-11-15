// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package spreadsheet

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
)

// maxRowAddedPollPageSize keeps one poll page within pollingtrigger.MaxChangesPerPage.
const maxRowAddedPollPageSize = pollingtrigger.MaxChangesPerPage

// rowAddedCursorVersion versions the stored rowAdded cursor token.
const rowAddedCursorVersion = 1

// rowAddedMaxBackoff caps the wait after consecutive failed polls.
const rowAddedMaxBackoff = 5 * time.Minute

// maxRowAddedScanWindows bounds a seed or realignment scan to this many times maxRows rows.
const maxRowAddedScanWindows = 100

// minRowAddedScanWindow is the smallest window a scan shrinks to when a window's response exceeds
// maxResponseBytes.
const minRowAddedScanWindow = 100

// RowAddedTriggerConfiguration selects the spreadsheet tab a rowAdded binding polls. Dex Web saves it
// from the spreadsheetPicker and sheetTabPicker units.
type RowAddedTriggerConfiguration struct {
	// SpreadsheetID is the Google spreadsheet ID stored by the spreadsheetPicker unit. Required.
	SpreadsheetID string `json:"spreadsheetId"`
	// SheetName is the exact tab title stored by the sheetTabPicker unit, used verbatim, including any
	// leading or trailing spaces. Required. Row 1 of the tab is the header row whose cells name
	// RowAddedEvent.Values.
	SheetName string `json:"sheetName"`
}

// RowAddedEvent is one row appended below the last processed row of the polled tab.
type RowAddedEvent struct {
	// SpreadsheetID is the polled spreadsheet ID.
	SpreadsheetID string `json:"spreadsheetId"`
	// SheetName is the polled tab title.
	SheetName string `json:"sheetName"`
	// RowNumber is the 1-based sheet row number when the row was read; row 1 is the header.
	RowNumber int64 `json:"rowNumber"`
	// Values maps each non-empty, unique header cell to this row's formatted cell, or "" when the row has
	// no value in that column. Columns whose header is empty or repeated appear only in Cells.
	Values map[string]string `json:"values"`
	// Cells are the row's formatted cell values in column order, without trailing empty cells.
	Cells []string `json:"cells"`
}

// rowAddedCursorToken is the JSON position stored in the poll cursor: the last processed row and a
// SHA-256 of its cells, which detects rows deleted or inserted above it.
type rowAddedCursorToken struct {
	Version int    `json:"v"`
	LastRow int64  `json:"lastRow"`
	RowHash string `json:"rowHash"`
}

// Validate checks that the binding names a spreadsheet and a tab.
func (configuration RowAddedTriggerConfiguration) Validate() error {
	if strings.TrimSpace(configuration.SpreadsheetID) == "" || strings.TrimSpace(configuration.SheetName) == "" {
		return errors.New("Google Sheets rowAdded spreadsheetId and sheetName are required")
	}
	return nil
}

// rowAddedTriggerSource serves the generated NewRowAddedTrigger factory. Its cursor lives in process
// memory, so a restart seeds again; project applications use NewProjectRowAddedTriggerRunner instead.
func (client *Client) rowAddedTriggerSource(
	connection sdkgo.ConnectionRef,
	configuration RowAddedTriggerConfiguration,
) sdkgo.TriggerSource[RowAddedEvent] {
	if err := configuration.Validate(); err != nil {
		return failedRowAddedTriggerSource{err: err}
	}
	// The generated factory does not pass its binding name, so log records name the Trigger instead.
	binding := sdkgo.TriggerBindingRef{Connection: connection, Trigger: RowAddedTriggerDefinition.Trigger, Name: RowAddedTriggerDefinition.Trigger.TriggerName}
	source, err := pollingtrigger.NewBindingTriggerSource(pollingtrigger.BindingPollerConfig[RowAddedEvent]{
		Definition: RowAddedTriggerDefinition, Binding: binding, Cursor: pollingtrigger.NewMemoryCursorStore(),
		Fetcher: client.newRowAddedFetcher(binding, configuration),
	}, client.rowAddedPollSchedule())
	if err != nil {
		return failedRowAddedTriggerSource{err: err}
	}
	return source
}

// failedRowAddedTriggerSource reports an invalid binding when its Trigger runs.
type failedRowAddedTriggerSource struct{ err error }

// Run returns the configuration error without polling.
func (source failedRowAddedTriggerSource) Run(context.Context, sdkgo.TriggerTarget[RowAddedEvent]) error {
	return source.err
}

// rowAddedPollSchedule polls at the configured interval and backs off to five minutes.
func (client *Client) rowAddedPollSchedule() pollingtrigger.PollSchedule {
	return pollingtrigger.PollSchedule{Interval: client.pollInterval, MaxBackoff: max(rowAddedMaxBackoff, client.pollInterval)}
}

// rowAddedFetcher reads one tab's rows after the cursor row through values:batchGet.
type rowAddedFetcher struct {
	client        *Client
	binding       sdkgo.TriggerBindingRef
	call          sdkgo.Call
	spreadsheetID string
	sheetName     string
}

func (client *Client) newRowAddedFetcher(binding sdkgo.TriggerBindingRef, configuration RowAddedTriggerConfiguration) *rowAddedFetcher {
	return &rowAddedFetcher{
		client: client, binding: binding, call: pollingtrigger.NewTriggerCredentialCall(binding.Connection, binding.Trigger),
		spreadsheetID: strings.TrimSpace(configuration.SpreadsheetID), sheetName: configuration.SheetName,
	}
}

// SeedCursor positions the cursor after the tab's current last row, so existing rows are not delivered.
func (fetcher *rowAddedFetcher) SeedCursor(ctx context.Context) (pollingtrigger.Cursor, error) {
	scan, err := fetcher.scanTab(ctx, nil)
	if err != nil {
		return pollingtrigger.Cursor{}, err
	}
	return fetcher.encodeCursor(rowAddedCursorToken{Version: rowAddedCursorVersion, LastRow: scan.lastRow, RowHash: hashRowCells(scan.lastRowCells)})
}

// FetchChanges reads the header row and up to pollPageSize rows after the cursor row. When the cursor
// row no longer holds the hashed cells, it first realigns the cursor with realignCursor.
func (fetcher *rowAddedFetcher) FetchChanges(
	ctx context.Context,
	cursor pollingtrigger.Cursor,
) (pollingtrigger.ChangePage[sdkgo.TriggerEvent[RowAddedEvent]], error) {
	var page pollingtrigger.ChangePage[sdkgo.TriggerEvent[RowAddedEvent]]
	token, err := decodeRowAddedCursorToken(cursor.Token)
	if err != nil {
		return page, pollingtrigger.MarkCursorInvalid(err)
	}
	header, window, err := fetcher.readWindow(ctx, token.LastRow)
	if err != nil {
		return page, err
	}
	if !windowStartsAtCursorRow(window, token) {
		if token, err = fetcher.realignCursor(ctx, token); err != nil {
			return page, err
		}
		if header, window, err = fetcher.readWindow(ctx, token.LastRow); err != nil {
			return page, err
		}
		if !windowStartsAtCursorRow(window, token) {
			return page, errors.New("Google Sheets rows moved during the rowAdded poll")
		}
	}
	occurredAt := fetcher.client.now().UTC()
	for offset := 1; offset < len(window); offset++ {
		cells := withoutTrailingEmptyCells(window[offset])
		if len(cells) == 0 {
			continue
		}
		rowNumber := token.LastRow + int64(offset)
		page.Changes = append(page.Changes, sdkgo.TriggerEvent[RowAddedEvent]{
			ID: fetcher.eventID(rowNumber, cells), OccurredAt: occurredAt,
			Payload: RowAddedEvent{
				SpreadsheetID: fetcher.spreadsheetID, SheetName: fetcher.sheetName, RowNumber: rowNumber,
				Values: headerKeyedValues(header, cells), Cells: cells,
			},
		})
	}
	if len(window) > 1 {
		token.LastRow += int64(len(window) - 1)
		token.RowHash = hashRowCells(window[len(window)-1])
	}
	page.HasMorePages = len(window) == fetcher.client.pollPageSize+1
	page.Next, err = fetcher.encodeCursor(token)
	return page, err
}

// realignCursor finds the row that holds the cursor row's cells after rows above it were deleted or
// inserted, choosing the match nearest the old row number. Without a match it assumes the cursor row was
// edited in place and keeps its number; when the tab now ends above that row it reports the cursor invalid,
// so the poller reseeds and logs the gap.
func (fetcher *rowAddedFetcher) realignCursor(ctx context.Context, token rowAddedCursorToken) (rowAddedCursorToken, error) {
	scan, err := fetcher.scanTab(ctx, &token)
	if err != nil {
		return token, err
	}
	nearest := int64(0)
	for _, rowNumber := range scan.matchingRows {
		if nearest == 0 || absoluteRowDistance(rowNumber, token.LastRow) < absoluteRowDistance(nearest, token.LastRow) {
			nearest = rowNumber
		}
	}
	switch {
	case nearest != 0:
		fetcher.log(ctx, slog.LevelInfo, "sheet rows moved; cursor realigned", slog.Int64("from_row", token.LastRow), slog.Int64("to_row", nearest))
		token.LastRow = nearest
	case token.LastRow <= scan.lastRow:
		fetcher.log(ctx, slog.LevelInfo, "last processed row was edited; keeping its position", slog.Int64("row", token.LastRow))
		token.RowHash = hashRowCells(scan.cursorRowCells)
	default:
		return token, pollingtrigger.MarkCursorInvalid(fmt.Errorf("Google Sheets tab now ends at row %d, above the last processed row %d", scan.lastRow, token.LastRow))
	}
	return token, nil
}

// tabScan is the result of reading a whole tab in maxRows windows.
type tabScan struct {
	lastRow        int64
	lastRowCells   []string
	matchingRows   []int64
	cursorRowCells []string
}

// scanTab reads the tab from row 1 in maxRows windows until a window ends early. It records the last row,
// and, for a non-nil cursor, the rows below the header that hold its hashed cells and its row's cells. A
// window whose response exceeds maxResponseBytes is halved, down to minRowAddedScanWindow rows, and read
// again, so a tab with long rows still seeds.
func (fetcher *rowAddedFetcher) scanTab(ctx context.Context, cursor *rowAddedCursorToken) (tabScan, error) {
	scan := tabScan{lastRow: 1}
	rowHash, cursorRow := "", int64(0)
	if cursor != nil {
		rowHash, cursorRow = cursor.RowHash, cursor.LastRow
	}
	windowSize := int64(fetcher.client.maxRows)
	minWindowSize := min(windowSize, minRowAddedScanWindow)
	maxScanRows := maxRowAddedScanWindows * windowSize
	for firstRow := int64(1); firstRow <= maxScanRows; {
		ranges, err := fetcher.readRanges(ctx, fetcher.rowRange(firstRow, firstRow+windowSize-1))
		if isResponseTooLarge(err) && windowSize > minWindowSize {
			windowSize = max(windowSize/2, minWindowSize)
			fetcher.log(ctx, slog.LevelDebug, "sheet scan window exceeds maxResponseBytes; halving it", slog.Int64("window_rows", windowSize))
			continue
		}
		if err != nil {
			return tabScan{}, err
		}
		rows := ranges[0]
		for offset, row := range rows {
			rowNumber := firstRow + int64(offset)
			if rowHash != "" && rowNumber > 1 && hashRowCells(row) == rowHash {
				scan.matchingRows = append(scan.matchingRows, rowNumber)
			}
			if rowNumber == cursorRow {
				scan.cursorRowCells = row
			}
		}
		if len(rows) > 0 {
			scan.lastRow, scan.lastRowCells = firstRow+int64(len(rows)-1), rows[len(rows)-1]
		}
		if int64(len(rows)) < windowSize {
			return scan, nil
		}
		firstRow += windowSize
	}
	return tabScan{}, fmt.Errorf("Google Sheets tab has more than %d rows to scan", maxScanRows)
}

// isResponseTooLarge reports a provider response that exceeded maxResponseBytes.
func isResponseTooLarge(err error) bool {
	var requestErr *providerRequestError
	return errors.As(err, &requestErr) && requestErr.kind == sdkgo.FailureResponseTooLarge
}

// readWindow returns the header row and the rows from lastRow through lastRow+pollPageSize.
func (fetcher *rowAddedFetcher) readWindow(ctx context.Context, lastRow int64) ([]string, [][]string, error) {
	ranges, err := fetcher.readRanges(ctx, fetcher.rowRange(1, 1), fetcher.rowRange(lastRow, lastRow+int64(fetcher.client.pollPageSize)))
	if err != nil {
		return nil, nil, err
	}
	var header []string
	if len(ranges[0]) > 0 {
		header = ranges[0][0]
	}
	return header, ranges[1], nil
}

// readRanges makes one values:batchGet request for whole-row ranges and returns each range's rows. Sheets
// omits trailing empty rows and columns, so a range may return fewer rows than it spans.
func (fetcher *rowAddedFetcher) readRanges(ctx context.Context, ranges ...string) ([][][]string, error) {
	if err := pollingtrigger.WaitForPollRequest(ctx); err != nil {
		return nil, err
	}
	credential, failure, _ := fetcher.client.resolveCredential(ctx, fetcher.call, "rowAdded")
	if failure != nil {
		return nil, errors.New("Google Sheets rowAdded poll " + failure.Message)
	}
	query := url.Values{"ranges": ranges, "majorDimension": {"ROWS"}, "valueRenderOption": {"FORMATTED_VALUE"}}
	result, _, err := fetcher.client.request(ctx, fetcher.call, credential, http.MethodGet, fetcher.spreadsheetID, "/values:batchGet", query, nil)
	if err != nil {
		return nil, fmt.Errorf("Google Sheets rowAdded poll %w", err)
	}
	if result.status == http.StatusTooManyRequests {
		// An unreadable Retry-After falls back to the runner's exponential backoff.
		delay, _ := retryAfterDelay(result.header)
		return nil, pollingtrigger.MarkRateLimited(errors.New("Google Sheets rate limited the rowAdded poll with HTTP 429"), delay)
	}
	if result.status < 200 || result.status >= 300 {
		return nil, fmt.Errorf("Google Sheets rejected the rowAdded poll with HTTP %d", result.status)
	}
	var response struct {
		ValueRanges []valuesResponse `json:"valueRanges"`
	}
	if err := json.Unmarshal(result.body, &response); err != nil || len(response.ValueRanges) != len(ranges) {
		return nil, errors.New("Google Sheets returned an invalid rowAdded poll response")
	}
	rows := make([][][]string, len(ranges))
	for index, valueRange := range response.ValueRanges {
		if len(valueRange.Values) > max(fetcher.client.maxRows, fetcher.client.pollPageSize+1) {
			return nil, errors.New("Google Sheets returned more rows than the rowAdded poll requested")
		}
		rows[index] = convertValues(valueRange).Values
	}
	return rows, nil
}

// rowRange returns the A1 whole-row range firstRow:lastRow of the polled tab, such as 'Leads'!5:105.
func (fetcher *rowAddedFetcher) rowRange(firstRow int64, lastRow int64) string {
	return quoteSheet(fetcher.sheetName) + "!" + strconv.FormatInt(firstRow, 10) + ":" + strconv.FormatInt(lastRow, 10)
}

// eventID is a pure function of the binding's tab, the row number, and its cells, so a refetch after a
// crash yields the same ID and a reused row number with new cells yields a new one.
func (fetcher *rowAddedFetcher) eventID(rowNumber int64, cells []string) string {
	encodedCells, _ := json.Marshal(cells) // A []string always encodes.
	digest := sha256.Sum256([]byte(strings.Join([]string{fetcher.spreadsheetID, fetcher.sheetName, strconv.FormatInt(rowNumber, 10), string(encodedCells)}, "\x00")))
	return "row-" + strconv.FormatInt(rowNumber, 10) + "-" + hex.EncodeToString(digest[:8])
}

func (fetcher *rowAddedFetcher) encodeCursor(token rowAddedCursorToken) (pollingtrigger.Cursor, error) {
	encoded, err := json.Marshal(token)
	if err != nil {
		return pollingtrigger.Cursor{}, fmt.Errorf("encode Google Sheets rowAdded cursor: %w", err)
	}
	return pollingtrigger.Cursor{Token: string(encoded)}, nil
}

// log writes one record with the binding's identity; it never carries cell values or the sheet title.
func (fetcher *rowAddedFetcher) log(ctx context.Context, level slog.Level, message string, attrs ...slog.Attr) {
	logger := fetcher.client.triggerLogger()
	attrs = append([]slog.Attr{
		slog.String("connector", ConnectorID), slog.String("connection", fetcher.binding.Connection.Name),
		slog.String("trigger", fetcher.binding.Trigger.TriggerName), slog.String("binding", fetcher.binding.Name),
	}, attrs...)
	logger.LogAttrs(ctx, level, message, attrs...)
}

// triggerLogger returns the WithLogger logger or, as of the call, slog.Default().
func (client *Client) triggerLogger() *slog.Logger {
	if client.logger != nil {
		return client.logger
	}
	return slog.Default()
}

func decodeRowAddedCursorToken(encoded string) (rowAddedCursorToken, error) {
	var token rowAddedCursorToken
	decoder := json.NewDecoder(strings.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&token); err != nil {
		return token, errors.New("Google Sheets rowAdded cursor is not valid JSON")
	}
	if token.Version != rowAddedCursorVersion || token.LastRow < 1 || len(token.RowHash) != sha256.Size*2 {
		return token, errors.New("Google Sheets rowAdded cursor has an unsupported shape")
	}
	if _, err := hex.DecodeString(token.RowHash); err != nil {
		return token, errors.New("Google Sheets rowAdded cursor hash is not hexadecimal")
	}
	return token, nil
}

// windowStartsAtCursorRow reports whether the window's first row still holds the cursor row's cells.
// The header row position never moves, so a cursor on row 1 always matches.
func windowStartsAtCursorRow(window [][]string, token rowAddedCursorToken) bool {
	if token.LastRow == 1 {
		return true
	}
	var firstRow []string
	if len(window) > 0 {
		firstRow = window[0]
	}
	return hashRowCells(firstRow) == token.RowHash
}

// hashRowCells returns the hex SHA-256 of the JSON cells without trailing empty cells.
func hashRowCells(cells []string) string {
	encodedCells, _ := json.Marshal(withoutTrailingEmptyCells(cells)) // A []string always encodes.
	digest := sha256.Sum256(encodedCells)
	return hex.EncodeToString(digest[:])
}

func withoutTrailingEmptyCells(cells []string) []string {
	end := len(cells)
	for end > 0 && cells[end-1] == "" {
		end--
	}
	trimmed := make([]string, end)
	copy(trimmed, cells[:end])
	return trimmed
}

// headerKeyedValues maps each non-empty, unique header to its cell, "" when the row is shorter.
func headerKeyedValues(header []string, cells []string) map[string]string {
	counts := make(map[string]int, len(header))
	for _, name := range header {
		counts[name]++
	}
	values := make(map[string]string, len(header))
	for column, name := range header {
		if name == "" || counts[name] > 1 {
			continue
		}
		if column < len(cells) {
			values[name] = cells[column]
		} else {
			values[name] = ""
		}
	}
	return values
}

func absoluteRowDistance(first int64, second int64) int64 {
	if first > second {
		return first - second
	}
	return second - first
}
