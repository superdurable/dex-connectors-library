// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package notionrowsfake serves one Notion data source's rows over HTTP for databaseRowChanged tests: the
// data source query with a last_edited_time on_or_after filter and timestamp sort, and page reads. Start
// cursors are "rows-<offset>". Tests set every row's created and last edited time, so minute rounding and
// shared timestamps are scripted, not guessed.
package notionrowsfake

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// Request is one recorded request.
type Request struct {
	// Method and Path identify the endpoint.
	Method, Path string
	// Body is the decoded query body; nil for a page read.
	Body *QueryBody
	// Status is the HTTP status the fake answered.
	Status int
}

// QueryBody is the subset of a data source query the fake reads.
type QueryBody struct {
	Filter *struct {
		Timestamp      string `json:"timestamp"`
		LastEditedTime struct {
			OnOrAfter string `json:"on_or_after"`
		} `json:"last_edited_time"`
	} `json:"filter"`
	Sorts []struct {
		Timestamp string `json:"timestamp"`
		Direction string `json:"direction"`
	} `json:"sorts"`
	StartCursor string `json:"start_cursor"`
	PageSize    int    `json:"page_size"`
}

// DataSource is a stateful single-data-source fake that is safe for concurrent use.
type DataSource struct {
	*httptest.Server
	dataSourceID       string
	databaseID         string
	mutex              sync.Mutex
	rows               map[string]*storedRow
	hiddenRows         map[string]bool
	rateLimited        int
	retryAfter         string
	isStartCursorStale bool
	requests           []Request
}

type storedRow struct {
	id, title           string
	created, lastEdited time.Time
	isInTrash           bool
}

// New starts a fake serving dataSourceID inside databaseID; the test cleanup closes it.
func New(t *testing.T, dataSourceID string, databaseID string) *DataSource {
	t.Helper()
	fake := &DataSource{dataSourceID: dataSourceID, databaseID: databaseID, rows: map[string]*storedRow{}, hiddenRows: map[string]bool{}}
	fake.Server = httptest.NewServer(http.HandlerFunc(fake.serveHTTP))
	t.Cleanup(fake.Close)
	return fake
}

// Create adds a row created and last edited at createdAt.
func (fake *DataSource) Create(rowID string, title string, createdAt time.Time) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.rows[rowID] = &storedRow{id: rowID, title: title, created: createdAt.UTC(), lastEdited: createdAt.UTC()}
}

// Edit retitles a row and sets its last edited time.
func (fake *DataSource) Edit(rowID string, title string, editedAt time.Time) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	row := fake.mustRow(rowID)
	row.title, row.lastEdited = title, editedAt.UTC()
}

// Trash moves a row to the trash, which the data source query no longer returns.
func (fake *DataSource) Trash(rowID string, editedAt time.Time) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	row := fake.mustRow(rowID)
	row.isInTrash, row.lastEdited = true, editedAt.UTC()
}

// HideFromQueries simulates Notion indexing a row late: queries omit it until RevealToQueries.
func (fake *DataSource) HideFromQueries(rowID string) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.hiddenRows[rowID] = true
}

// RevealToQueries makes a hidden row visible to queries again.
func (fake *DataSource) RevealToQueries(rowID string) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	delete(fake.hiddenRows, rowID)
}

// RateLimitNextRequests answers the next count requests 429 rate_limited with Retry-After seconds.
func (fake *DataSource) RateLimitNextRequests(count int, retryAfter string) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.rateLimited, fake.retryAfter = count, retryAfter
}

// ExpireStartCursors answers the next query that sends a start cursor 400 validation_error.
func (fake *DataSource) ExpireStartCursors() {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.isStartCursorStale = true
}

// Requests returns a copy of the recorded requests.
func (fake *DataSource) Requests() []Request {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	return append([]Request(nil), fake.requests...)
}

func (fake *DataSource) mustRow(rowID string) *storedRow {
	row, exists := fake.rows[rowID]
	if !exists {
		panic("notionrowsfake: unknown row " + rowID)
	}
	return row
}
