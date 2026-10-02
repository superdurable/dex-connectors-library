// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package fakeexcel is a stateful, credential-checking fake of the Microsoft
// Graph workbook API subset and the Microsoft identity token endpoint the
// connector uses. It stores typed input the way Excel does: a leading
// apostrophe keeps text literal, text that looks like a number or Boolean is
// converted, and text that starts with = is a formula. Tests can delay, hold,
// refuse, or answer an append ambiguously after applying it.
package fakeexcel

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Counter names reported by Count.
const (
	CountColumnReads          = "tables.columns.get"
	CountTableDataReads       = "tables.dataBodyRange.get"
	CountKeyColumnReads       = "tables.columns.dataBodyRange.get"
	CountAppendsReceived      = "tables.rows.add.received"
	CountAppendsApplied       = "tables.rows.add.applied"
	CountRowsAppended         = "tables.rows.add.rows"
	CountRangeReads           = "worksheets.range.get"
	CountRangeUpdatesReceived = "worksheets.range.patch.received"
	CountRangeUpdatesApplied  = "worksheets.range.patch.applied"
	CountFormulasWritten      = "cells.formulas"
	CountTokenRefreshes       = "token.refresh"
	CountUnauthorized         = "unauthorized"
)

var (
	workbookPathPattern = regexp.MustCompile(`^/v1\.0/drives/([^/]+)/items/([^/]+)/workbook/(.+)$`)
	columnsPattern      = regexp.MustCompile(`^tables/([^/]+)/columns$`)
	tableDataPattern    = regexp.MustCompile(`^tables/([^/]+)/dataBodyRange$`)
	columnDataPattern   = regexp.MustCompile(`^tables/([^/]+)/columns/([^/]+)/dataBodyRange$`)
	appendPattern       = regexp.MustCompile(`^tables/([^/]+)/rows/add$`)
	rangePattern        = regexp.MustCompile(`^worksheets/([^/]+)/range\(address='([A-Z]{1,3}[0-9]{1,7}(?::[A-Z]{1,3}[0-9]{1,7})?)'\)$`)
	cellPattern         = regexp.MustCompile(`^([A-Z]{1,3})([0-9]{1,7})$`)
)

// Table is one Excel table: its column names and data rows of raw values.
type Table struct {
	// ID is the opaque table ID, such as {6D182180-0000-4000-8000-000000000001}.
	ID string
	// Name is the table name, such as ApprovalPolicy.
	Name string
	// Columns lists the column names in table order.
	Columns []string
	// Rows holds the data rows; each value is a string, float64, or bool.
	Rows [][]any
}

// AppendBehavior changes how the fake answers one received append.
type AppendBehavior struct {
	// DelayBeforeApplying waits before the rows are applied, like a request queued on Excel's servers.
	DelayBeforeApplying time.Duration
	// DelayAfterApplying waits after the rows are applied and before the response.
	DelayAfterApplying time.Duration
	// Hold blocks the request before applying it until the channel is closed.
	Hold chan struct{}
	// StatusAfterApplying answers this status, such as 504, after the rows are applied.
	StatusAfterApplying int
	// StatusWithoutApplying answers this status, such as 429 or 503, without applying the rows.
	StatusWithoutApplying int
	// RetryAfterSeconds sets Retry-After on a StatusWithoutApplying response.
	RetryAfterSeconds int
	// IsHiddenFromKeyReads keeps the applied rows out of later key column reads, like a lagging copy.
	IsHiddenFromKeyReads bool
}

// ReadFailure answers one later GET with an error status without reading anything.
type ReadFailure struct {
	// Status is the HTTP status, such as 429.
	Status int
	// RetryAfterSeconds sets Retry-After when positive.
	RetryAfterSeconds int
	// SecondLevelCode is Excel's innerError code, such as tooManyRequestsUncategorized.
	SecondLevelCode string
}

type cellKey struct {
	row    int
	column int
}

type worksheet struct {
	id    string
	name  string
	cells map[cellKey]any
}

type table struct {
	Table
	hiddenRowIndexes map[int]bool
}

type workbook struct {
	worksheets []*worksheet
	tables     []*table
}

// Server is the fake. Its methods are safe for concurrent use.
type Server struct {
	// URL is the base URL of the test server started by New; empty for NewHandler.
	URL string

	mutex             sync.Mutex
	accessToken       string
	refreshToken      string
	workbooks         map[string]*workbook
	counts            map[string]int
	appendBehaviors   []AppendBehavior
	readFailures      []ReadFailure
	rangeUpdateDelays []time.Duration
	tokenGeneration   int
	delayed           sync.WaitGroup
}

// New starts a fake on a loopback test server that accepts accessToken and refreshToken.
func New(t testing.TB, accessToken string, refreshToken string) *Server {
	t.Helper()
	server := NewHandler(accessToken, refreshToken)
	httpServer := httptest.NewServer(server)
	t.Cleanup(httpServer.Close)
	server.URL = httpServer.URL
	return server
}

// NewHandler returns a fake that accepts accessToken and refreshToken, for a caller that serves it.
func NewHandler(accessToken string, refreshToken string) *Server {
	return &Server{
		accessToken: accessToken, refreshToken: refreshToken,
		workbooks: map[string]*workbook{}, counts: map[string]int{},
	}
}

// AddWorksheet adds an empty worksheet to the workbook, creating the workbook when needed.
func (server *Server) AddWorksheet(driveID, workbookID, worksheetID, name string) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	book := server.workbookLocked(driveID, workbookID)
	book.worksheets = append(book.worksheets, &worksheet{id: worksheetID, name: name, cells: map[cellKey]any{}})
}

// AddTable adds a table to the workbook. A table without rows gets one empty data row, as in Excel.
func (server *Server) AddTable(driveID, workbookID string, added Table) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	book := server.workbookLocked(driveID, workbookID)
	if len(added.Rows) == 0 {
		emptyRow := make([]any, len(added.Columns))
		for index := range emptyRow {
			emptyRow[index] = ""
		}
		added.Rows = [][]any{emptyRow}
	}
	book.tables = append(book.tables, &table{Table: added, hiddenRowIndexes: map[int]bool{}})
}

// TableRows returns a copy of the table's data rows.
func (server *Server) TableRows(driveID, workbookID, tableName string) [][]any {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	found := server.findTableLocked(driveID, workbookID, tableName)
	if found == nil {
		return nil
	}
	rows := make([][]any, len(found.Rows))
	for index, row := range found.Rows {
		rows[index] = append([]any(nil), row...)
	}
	return rows
}

// CellValue returns the stored value of one worksheet cell, such as B2, or "" when it is empty.
func (server *Server) CellValue(driveID, workbookID, worksheetName, cell string) any {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	sheet := server.findWorksheetLocked(driveID, workbookID, worksheetName)
	column, row, err := parseCell(cell)
	if sheet == nil || err != nil {
		return nil
	}
	return cellOrEmpty(sheet.cells[cellKey{row: row, column: column}])
}

// SetCellValue stores a raw value in one worksheet cell.
func (server *Server) SetCellValue(driveID, workbookID, worksheetName, cell string, value any) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	sheet := server.findWorksheetLocked(driveID, workbookID, worksheetName)
	column, row, err := parseCell(cell)
	if sheet != nil && err == nil {
		sheet.cells[cellKey{row: row, column: column}] = value
	}
}

// QueueAppendBehaviors applies one behavior to each following append, in order.
func (server *Server) QueueAppendBehaviors(behaviors ...AppendBehavior) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.appendBehaviors = append(server.appendBehaviors, behaviors...)
}

// QueueReadFailures answers each following GET with one failure, in order.
func (server *Server) QueueReadFailures(failures ...ReadFailure) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.readFailures = append(server.readFailures, failures...)
}

// QueueRangeUpdateDelays delays each following range update's response after applying it.
func (server *Server) QueueRangeUpdateDelays(delays ...time.Duration) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.rangeUpdateDelays = append(server.rangeUpdateDelays, delays...)
}

// Count returns the named counter.
func (server *Server) Count(name string) int {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	return server.counts[name]
}

// AccessToken returns the access token the fake currently accepts.
func (server *Server) AccessToken() string {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	return server.accessToken
}

// WaitForDelayedRequests waits until every delayed or held request has answered.
func (server *Server) WaitForDelayedRequests(t testing.TB, timeout time.Duration) {
	t.Helper()
	finished := make(chan struct{})
	go func() {
		server.delayed.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(timeout):
		t.Fatalf("delayed fake Excel requests did not finish within %s", timeout)
	}
}

// ServeHTTP routes token and workbook requests.
func (server *Server) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("request-id", "00000000-0000-4000-8000-0000000000aa")
	if request.Method == http.MethodPost && request.URL.Path == "/organizations/oauth2/v2.0/token" {
		server.refreshAccessToken(response, request)
		return
	}
	if request.Header.Get("Authorization") != "Bearer "+server.AccessToken() {
		server.increment(CountUnauthorized)
		writeGraphError(response, http.StatusUnauthorized, "InvalidAuthenticationToken", "")
		return
	}
	match := workbookPathPattern.FindStringSubmatch(request.URL.Path)
	if match == nil {
		writeGraphError(response, http.StatusNotFound, "itemNotFound", "")
		return
	}
	driveID, workbookID, rest := match[1], match[2], match[3]
	if request.Method == http.MethodGet && server.answerQueuedReadFailure(response) {
		return
	}
	switch {
	case request.Method == http.MethodGet && columnsPattern.MatchString(rest):
		server.getColumns(response, driveID, workbookID, columnsPattern.FindStringSubmatch(rest)[1])
	case request.Method == http.MethodGet && tableDataPattern.MatchString(rest):
		server.getTableData(response, driveID, workbookID, tableDataPattern.FindStringSubmatch(rest)[1])
	case request.Method == http.MethodGet && columnDataPattern.MatchString(rest):
		parts := columnDataPattern.FindStringSubmatch(rest)
		server.getColumnData(response, driveID, workbookID, parts[1], parts[2])
	case request.Method == http.MethodPost && appendPattern.MatchString(rest):
		server.appendRows(response, request, driveID, workbookID, appendPattern.FindStringSubmatch(rest)[1])
	case rangePattern.MatchString(rest) && (request.Method == http.MethodGet || request.Method == http.MethodPatch):
		parts := rangePattern.FindStringSubmatch(rest)
		server.serveRange(response, request, driveID, workbookID, parts[1], parts[2])
	default:
		writeGraphError(response, http.StatusBadRequest, "invalidRequest", "")
	}
}

func (server *Server) refreshAccessToken(response http.ResponseWriter, request *http.Request) {
	if err := request.ParseForm(); err != nil || request.PostForm.Get("grant_type") != "refresh_token" {
		writeJSON(response, http.StatusBadRequest, map[string]any{"error": "invalid_request"})
		return
	}
	server.mutex.Lock()
	defer server.mutex.Unlock()
	if request.PostForm.Get("refresh_token") != server.refreshToken || request.PostForm.Get("client_secret") == "" {
		writeJSON(response, http.StatusBadRequest, map[string]any{"error": "invalid_grant"})
		return
	}
	server.tokenGeneration++
	server.counts[CountTokenRefreshes]++
	server.accessToken = fmt.Sprintf("fake-excel-access-%d", server.tokenGeneration)
	server.refreshToken = fmt.Sprintf("fake-excel-refresh-%d", server.tokenGeneration)
	writeJSON(response, http.StatusOK, map[string]any{
		"token_type": "Bearer", "expires_in": 3599, "scope": "Files.ReadWrite.All User.Read",
		"access_token": server.accessToken, "refresh_token": server.refreshToken,
	})
}

func (server *Server) answerQueuedReadFailure(response http.ResponseWriter) bool {
	server.mutex.Lock()
	if len(server.readFailures) == 0 {
		server.mutex.Unlock()
		return false
	}
	failure := server.readFailures[0]
	server.readFailures = server.readFailures[1:]
	server.mutex.Unlock()
	if failure.RetryAfterSeconds > 0 {
		response.Header().Set("Retry-After", strconv.Itoa(failure.RetryAfterSeconds))
	}
	writeGraphError(response, failure.Status, topLevelCode(failure.Status), failure.SecondLevelCode)
	return true
}

func (server *Server) getColumns(response http.ResponseWriter, driveID, workbookID, tableReference string) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.counts[CountColumnReads]++
	found := server.findTableLocked(driveID, workbookID, tableReference)
	if found == nil {
		writeGraphError(response, http.StatusNotFound, "itemNotFound", "notFoundUncategorized")
		return
	}
	columns := make([]map[string]any, len(found.Columns))
	for index, name := range found.Columns {
		columns[index] = map[string]any{"id": strconv.Itoa(index + 1), "index": index, "name": name}
	}
	writeJSON(response, http.StatusOK, map[string]any{"value": columns})
}

func (server *Server) getTableData(response http.ResponseWriter, driveID, workbookID, tableReference string) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.counts[CountTableDataReads]++
	found := server.findTableLocked(driveID, workbookID, tableReference)
	if found == nil {
		writeGraphError(response, http.StatusNotFound, "itemNotFound", "notFoundUncategorized")
		return
	}
	writeJSON(response, http.StatusOK, rangeBody("", found.Rows))
}

func (server *Server) getColumnData(response http.ResponseWriter, driveID, workbookID, tableReference, columnID string) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.counts[CountKeyColumnReads]++
	found := server.findTableLocked(driveID, workbookID, tableReference)
	columnIndex, err := strconv.Atoi(columnID)
	if found == nil || err != nil || columnIndex < 1 || columnIndex > len(found.Columns) {
		writeGraphError(response, http.StatusNotFound, "itemNotFound", "notFoundUncategorized")
		return
	}
	values := [][]any{}
	for rowIndex, row := range found.Rows {
		if !found.hiddenRowIndexes[rowIndex] {
			values = append(values, []any{row[columnIndex-1]})
		}
	}
	writeJSON(response, http.StatusOK, rangeBody("", values))
}

func (server *Server) appendRows(response http.ResponseWriter, request *http.Request, driveID, workbookID, tableReference string) {
	server.delayed.Add(1)
	defer server.delayed.Done()
	var body struct {
		Values [][]any `json:"values"`
	}
	content, err := io.ReadAll(request.Body)
	if err != nil || json.Unmarshal(content, &body) != nil || len(body.Values) == 0 {
		writeGraphError(response, http.StatusBadRequest, "badRequest", "invalidArgument")
		return
	}
	behavior := server.claimAppendBehavior()
	if behavior.StatusWithoutApplying != 0 {
		if behavior.RetryAfterSeconds > 0 {
			response.Header().Set("Retry-After", strconv.Itoa(behavior.RetryAfterSeconds))
		}
		writeGraphError(response, behavior.StatusWithoutApplying, topLevelCode(behavior.StatusWithoutApplying), "")
		return
	}
	if behavior.Hold != nil {
		<-behavior.Hold
	}
	time.Sleep(behavior.DelayBeforeApplying)
	server.mutex.Lock()
	found := server.findTableLocked(driveID, workbookID, tableReference)
	if found == nil {
		server.mutex.Unlock()
		writeGraphError(response, http.StatusNotFound, "itemNotFound", "notFoundUncategorized")
		return
	}
	for _, row := range body.Values {
		if len(row) != len(found.Columns) {
			server.mutex.Unlock()
			writeGraphError(response, http.StatusBadRequest, "badRequest", "invalidArgument")
			return
		}
	}
	firstIndex := len(found.Rows)
	for _, row := range body.Values {
		stored := make([]any, len(row))
		for index, value := range row {
			stored[index] = server.storeTypedInputLocked(value, "")
		}
		if behavior.IsHiddenFromKeyReads {
			found.hiddenRowIndexes[len(found.Rows)] = true
		}
		found.Rows = append(found.Rows, stored)
	}
	server.counts[CountAppendsApplied]++
	server.counts[CountRowsAppended] += len(body.Values)
	lastRow := found.Rows[len(found.Rows)-1]
	server.mutex.Unlock()
	time.Sleep(behavior.DelayAfterApplying)
	if behavior.StatusAfterApplying != 0 {
		writeGraphError(response, behavior.StatusAfterApplying, topLevelCode(behavior.StatusAfterApplying), "")
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"index": firstIndex, "values": [][]any{lastRow}})
}

func (server *Server) claimAppendBehavior() AppendBehavior {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.counts[CountAppendsReceived]++
	if len(server.appendBehaviors) == 0 {
		return AppendBehavior{}
	}
	behavior := server.appendBehaviors[0]
	server.appendBehaviors = server.appendBehaviors[1:]
	return behavior
}

func (server *Server) serveRange(response http.ResponseWriter, request *http.Request, driveID, workbookID, worksheetReference, address string) {
	firstColumn, firstRow, lastColumn, lastRow, err := parseAddress(address)
	if err != nil {
		writeGraphError(response, http.StatusBadRequest, "badRequest", "invalidArgument")
		return
	}
	var delay time.Duration
	if request.Method == http.MethodPatch {
		server.delayed.Add(1)
		defer server.delayed.Done()
		var body struct {
			Values [][]any `json:"values"`
		}
		content, readErr := io.ReadAll(request.Body)
		if readErr != nil || json.Unmarshal(content, &body) != nil {
			writeGraphError(response, http.StatusBadRequest, "badRequest", "invalidArgument")
			return
		}
		server.mutex.Lock()
		server.counts[CountRangeUpdatesReceived]++
		if len(server.rangeUpdateDelays) > 0 {
			delay = server.rangeUpdateDelays[0]
			server.rangeUpdateDelays = server.rangeUpdateDelays[1:]
		}
		sheet := server.findWorksheetLocked(driveID, workbookID, worksheetReference)
		if sheet == nil {
			server.mutex.Unlock()
			writeGraphError(response, http.StatusNotFound, "itemNotFound", "notFoundUncategorized")
			return
		}
		if len(body.Values) != lastRow-firstRow+1 {
			server.mutex.Unlock()
			writeGraphError(response, http.StatusBadRequest, "badRequest", "invalidArgument")
			return
		}
		for rowOffset, row := range body.Values {
			if len(row) != lastColumn-firstColumn+1 {
				server.mutex.Unlock()
				writeGraphError(response, http.StatusBadRequest, "badRequest", "invalidArgument")
				return
			}
			for columnOffset, value := range row {
				key := cellKey{row: firstRow + rowOffset, column: firstColumn + columnOffset}
				sheet.cells[key] = server.storeTypedInputLocked(value, sheet.cells[key])
			}
		}
		server.counts[CountRangeUpdatesApplied]++
		server.mutex.Unlock()
		time.Sleep(delay)
	}
	server.mutex.Lock()
	defer server.mutex.Unlock()
	if request.Method == http.MethodGet {
		server.counts[CountRangeReads]++
	}
	sheet := server.findWorksheetLocked(driveID, workbookID, worksheetReference)
	if sheet == nil {
		writeGraphError(response, http.StatusNotFound, "itemNotFound", "notFoundUncategorized")
		return
	}
	values := make([][]any, 0, lastRow-firstRow+1)
	for row := firstRow; row <= lastRow; row++ {
		cells := make([]any, 0, lastColumn-firstColumn+1)
		for column := firstColumn; column <= lastColumn; column++ {
			cells = append(cells, cellOrEmpty(sheet.cells[cellKey{row: row, column: column}]))
		}
		values = append(values, cells)
	}
	body := rangeBody(sheet.name+"!"+address, values)
	texts := make([][]string, len(values))
	for rowIndex, row := range values {
		texts[rowIndex] = make([]string, len(row))
		for columnIndex, value := range row {
			texts[rowIndex][columnIndex] = displayText(value)
		}
	}
	body["text"] = texts
	writeJSON(response, http.StatusOK, body)
}

// storeTypedInputLocked converts one written value the way Excel stores typed input; null keeps the cell.
func (server *Server) storeTypedInputLocked(value any, previous any) any {
	text, isText := value.(string)
	switch {
	case value == nil:
		return previous
	case !isText:
		return value
	case strings.HasPrefix(text, "'"):
		return strings.TrimPrefix(text, "'")
	case strings.HasPrefix(text, "="):
		server.counts[CountFormulasWritten]++
		return text
	case strings.EqualFold(text, "TRUE"), strings.EqualFold(text, "FALSE"):
		return strings.EqualFold(text, "TRUE")
	}
	if number, err := strconv.ParseFloat(text, 64); err == nil {
		return number
	}
	return text
}

func (server *Server) workbookLocked(driveID, workbookID string) *workbook {
	key := driveID + "/" + workbookID
	book, isFound := server.workbooks[key]
	if !isFound {
		book = &workbook{}
		server.workbooks[key] = book
	}
	return book
}

func (server *Server) findTableLocked(driveID, workbookID, reference string) *table {
	book := server.workbooks[driveID+"/"+workbookID]
	if book == nil {
		return nil
	}
	for _, candidate := range book.tables {
		if candidate.ID == reference || strings.EqualFold(candidate.Name, reference) {
			return candidate
		}
	}
	return nil
}

func (server *Server) findWorksheetLocked(driveID, workbookID, reference string) *worksheet {
	book := server.workbooks[driveID+"/"+workbookID]
	if book == nil {
		return nil
	}
	for _, candidate := range book.worksheets {
		if candidate.id == reference || strings.EqualFold(candidate.name, reference) {
			return candidate
		}
	}
	return nil
}

func (server *Server) increment(name string) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.counts[name]++
}

func rangeBody(address string, values [][]any) map[string]any {
	columnCount := 0
	if len(values) > 0 {
		columnCount = len(values[0])
	}
	return map[string]any{"address": address, "rowCount": len(values), "columnCount": columnCount, "values": values}
}

func displayText(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		return strings.ToUpper(strconv.FormatBool(typed))
	default:
		return ""
	}
}

func cellOrEmpty(value any) any {
	if value == nil {
		return ""
	}
	return value
}

func parseAddress(address string) (firstColumn, firstRow, lastColumn, lastRow int, err error) {
	first, last, isRange := strings.Cut(address, ":")
	if !isRange {
		last = first
	}
	if firstColumn, firstRow, err = parseCell(first); err != nil {
		return 0, 0, 0, 0, err
	}
	if lastColumn, lastRow, err = parseCell(last); err != nil {
		return 0, 0, 0, 0, err
	}
	return firstColumn, firstRow, lastColumn, lastRow, nil
}

func parseCell(cell string) (column int, row int, err error) {
	match := cellPattern.FindStringSubmatch(cell)
	if match == nil {
		return 0, 0, fmt.Errorf("cell %q is not in A1 form", cell)
	}
	for _, letter := range match[1] {
		column = column*26 + int(letter-'A'+1)
	}
	row, err = strconv.Atoi(match[2])
	return column, row, err
}

func topLevelCode(status int) string {
	switch status {
	case http.StatusTooManyRequests:
		return "tooManyRequests"
	case http.StatusServiceUnavailable:
		return "serviceUnavailable"
	case http.StatusGatewayTimeout:
		return "gatewayTimeout"
	case http.StatusBadGateway:
		return "badGateway"
	case http.StatusInternalServerError:
		return "internalServerError"
	default:
		return "badRequest"
	}
}

// writeGraphError writes a Graph error whose message is a sentinel, so tests can prove it never leaks.
func writeGraphError(response http.ResponseWriter, status int, code string, secondLevelCode string) {
	errorObject := map[string]any{"code": code, "message": "SENTINEL Excel provider message " + url.QueryEscape(code)}
	if secondLevelCode != "" {
		errorObject["innerError"] = map[string]any{"code": secondLevelCode, "message": "SENTINEL inner message"}
	}
	writeJSON(response, status, map[string]any{"error": errorObject})
}

func writeJSON(response http.ResponseWriter, status int, body any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	// The test client reads or drops the body; a failed write shows up as its transport error.
	_ = json.NewEncoder(response).Encode(body)
}
