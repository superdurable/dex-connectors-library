// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package sheetsfake serves one mutable Google Sheets tab through values:batchGet, and values.get for
// example read-backs, for connector tests.
package sheetsfake

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// ScriptedResponse is one failure answered instead of the next batchGet request.
type ScriptedResponse struct {
	// Status is the HTTP status code.
	Status int
	// RetryAfter is the optional Retry-After header value.
	RetryAfter string
}

// Request is one recorded batchGet request.
type Request struct {
	// SpreadsheetID is the spreadsheet ID in the request path.
	SpreadsheetID string
	// Ranges are the requested A1 ranges in order.
	Ranges []string
	// Authorization is the Authorization header.
	Authorization string
}

// Sheet is an in-process Sheets API with one tab. It is safe for concurrent use.
type Sheet struct {
	*httptest.Server
	mutex     sync.Mutex
	tab       string
	rows      [][]string
	scripted  []ScriptedResponse
	requests  []Request
	completed int
	deferred  map[int][][]string
	reads     []string
}

// New serves tab with rows; rows[0] is the header row. The server closes when the test ends.
func New(t testing.TB, tab string, rows ...[]string) *Sheet {
	t.Helper()
	sheet := &Sheet{tab: tab, rows: rows}
	sheet.Server = httptest.NewServer(http.HandlerFunc(sheet.serveHTTP))
	t.Cleanup(sheet.Close)
	return sheet
}

// AppendRows adds rows at the bottom of the tab.
func (sheet *Sheet) AppendRows(rows ...[]string) {
	sheet.mutex.Lock()
	defer sheet.mutex.Unlock()
	sheet.rows = append(sheet.rows, rows...)
}

// AppendRowsAfterRequests appends rows right after the server answers its count-th request, such as
// after a Trigger's seed request and before its first poll.
func (sheet *Sheet) AppendRowsAfterRequests(count int, rows ...[]string) {
	sheet.mutex.Lock()
	defer sheet.mutex.Unlock()
	if sheet.deferred == nil {
		sheet.deferred = map[int][][]string{}
	}
	sheet.deferred[count] = append(sheet.deferred[count], rows...)
}

// DeleteRow removes the 1-based row number, shifting later rows up.
func (sheet *Sheet) DeleteRow(rowNumber int) {
	sheet.mutex.Lock()
	defer sheet.mutex.Unlock()
	sheet.rows = slices.Delete(sheet.rows, rowNumber-1, rowNumber)
}

// ReplaceRow replaces the cells of the 1-based row number.
func (sheet *Sheet) ReplaceRow(rowNumber int, cells []string) {
	sheet.mutex.Lock()
	defer sheet.mutex.Unlock()
	sheet.rows[rowNumber-1] = cells
}

// TruncateRows keeps only the first count rows.
func (sheet *Sheet) TruncateRows(count int) {
	sheet.mutex.Lock()
	defer sheet.mutex.Unlock()
	sheet.rows = sheet.rows[:count]
}

// Script answers the next requests with responses, in order, before serving rows again.
func (sheet *Sheet) Script(responses ...ScriptedResponse) {
	sheet.mutex.Lock()
	defer sheet.mutex.Unlock()
	sheet.scripted = append(sheet.scripted, responses...)
}

// Requests returns every recorded request.
func (sheet *Sheet) Requests() []Request {
	sheet.mutex.Lock()
	defer sheet.mutex.Unlock()
	return slices.Clone(sheet.requests)
}

// ReadRanges returns the ranges of every values.get request in order. Those reads are not batchGet
// requests: they are not counted, recorded by Requests, or answered by Script.
func (sheet *Sheet) ReadRanges() []string {
	sheet.mutex.Lock()
	defer sheet.mutex.Unlock()
	return slices.Clone(sheet.reads)
}

// CompletedRequests counts the batchGet requests answered so far.
func (sheet *Sheet) CompletedRequests() int {
	sheet.mutex.Lock()
	defer sheet.mutex.Unlock()
	return sheet.completed
}

func (sheet *Sheet) serveHTTP(response http.ResponseWriter, request *http.Request) {
	sheet.mutex.Lock()
	defer sheet.mutex.Unlock()
	path := strings.TrimPrefix(request.URL.Path, "/spreadsheets/")
	if _, valueRange, isGet := strings.Cut(path, "/values/"); request.Method == http.MethodGet && isGet {
		sheet.serveValuesGet(response, valueRange)
		return
	}
	defer sheet.completeRequest()
	spreadsheetID, isBatchGet := strings.CutSuffix(path, "/values:batchGet")
	if request.Method != http.MethodGet || !isBatchGet {
		http.NotFound(response, request)
		return
	}
	ranges := request.URL.Query()["ranges"]
	sheet.requests = append(sheet.requests, Request{SpreadsheetID: spreadsheetID, Ranges: ranges, Authorization: request.Header.Get("Authorization")})
	if len(sheet.scripted) > 0 {
		scripted := sheet.scripted[0]
		sheet.scripted = sheet.scripted[1:]
		if scripted.RetryAfter != "" {
			response.Header().Set("Retry-After", scripted.RetryAfter)
		}
		http.Error(response, `{"error":{"code":`+strconv.Itoa(scripted.Status)+`}}`, scripted.Status)
		return
	}
	valueRanges := make([]map[string]any, 0, len(ranges))
	for _, valueRange := range ranges {
		firstRow, lastRow, ok := sheet.parseRowRange(valueRange)
		if !ok {
			http.Error(response, `{"error":{"code":400}}`, http.StatusBadRequest)
			return
		}
		served := map[string]any{"range": valueRange, "majorDimension": "ROWS"}
		// Like Sheets, a range without values omits the values field.
		if rows := sheet.rowsIn(firstRow, lastRow); len(rows) > 0 {
			served["values"] = rows
		}
		valueRanges = append(valueRanges, served)
	}
	response.Header().Set("Content-Type", "application/json")
	// Encoding a map of strings cannot fail; a broken connection only fails this test request.
	_ = json.NewEncoder(response).Encode(map[string]any{"spreadsheetId": spreadsheetID, "valueRanges": valueRanges})
}

// serveValuesGet answers one values.get read of a whole-row range, such as 'Leads'!7:7.
func (sheet *Sheet) serveValuesGet(response http.ResponseWriter, valueRange string) {
	sheet.reads = append(sheet.reads, valueRange)
	firstRow, lastRow, ok := sheet.parseRowRange(valueRange)
	if !ok {
		http.Error(response, `{"error":{"code":400}}`, http.StatusBadRequest)
		return
	}
	served := map[string]any{"range": valueRange, "majorDimension": "ROWS"}
	if rows := sheet.rowsIn(firstRow, lastRow); len(rows) > 0 {
		served["values"] = rows
	}
	response.Header().Set("Content-Type", "application/json")
	// Encoding a map of strings cannot fail; a broken connection only fails this test request.
	_ = json.NewEncoder(response).Encode(served)
}

// completeRequest counts an answered request and applies rows deferred until it.
func (sheet *Sheet) completeRequest() {
	sheet.completed++
	sheet.rows = append(sheet.rows, sheet.deferred[sheet.completed]...)
	delete(sheet.deferred, sheet.completed)
}

// parseRowRange accepts only the tab's quoted whole-row range, such as 'Leads'!2:11.
func (sheet *Sheet) parseRowRange(valueRange string) (int, int, bool) {
	quotedTab := "'" + strings.ReplaceAll(sheet.tab, "'", "''") + "'!"
	rows, ok := strings.CutPrefix(valueRange, quotedTab)
	if !ok {
		return 0, 0, false
	}
	first, last, ok := strings.Cut(rows, ":")
	firstRow, firstErr := strconv.Atoi(first)
	lastRow, lastErr := strconv.Atoi(last)
	return firstRow, lastRow, ok && firstErr == nil && lastErr == nil && firstRow >= 1 && lastRow >= firstRow
}

// rowsIn returns rows firstRow through lastRow without trailing empty rows or trailing empty cells, as
// Sheets does; an empty row before a non-empty one is served as [].
func (sheet *Sheet) rowsIn(firstRow int, lastRow int) [][]string {
	end := min(lastRow, len(sheet.rows))
	for end >= firstRow && len(trimTrailingEmptyCells(sheet.rows[end-1])) == 0 {
		end--
	}
	rows := [][]string{}
	for rowNumber := firstRow; rowNumber <= end; rowNumber++ {
		rows = append(rows, trimTrailingEmptyCells(sheet.rows[rowNumber-1]))
	}
	return rows
}

func trimTrailingEmptyCells(cells []string) []string {
	end := len(cells)
	for end > 0 && cells[end-1] == "" {
		end--
	}
	return append([]string{}, cells[:end]...)
}
