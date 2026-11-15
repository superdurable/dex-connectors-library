// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package mondayboardfake serves one monday.com board over the GraphQL API for itemChanged tests. It answers
// items_page ordered by __last_updated__, next_items_page with expiring cursors, and items by ID. Every
// change advances the board clock by one second unless the test sets the clock itself.
package mondayboardfake

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Token is the personal API token the fake accepts.
const Token = "monday-fake-token"

var baseTime = time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)

// Request is one recorded GraphQL request.
type Request struct {
	// Operation is the GraphQL operation name, such as ListBoardItems.
	Operation string
	// Variables are the request variables.
	Variables map[string]any
	// Status is the HTTP status the fake answered.
	Status int
}

// Board is a stateful single-board fake that is safe for concurrent use.
type Board struct {
	*httptest.Server
	boardID string

	mutex            sync.Mutex
	clock            time.Time
	items            map[string]*fakeItem
	cursors          map[string][]fakeItem
	nextCursor       int
	requests         []Request
	rateLimitedCalls int
	retryAfter       int
	dailyLimitCalls  int
}

type fakeItem struct {
	ID        string
	Name      string
	Status    string
	GroupID   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// New starts a fake for boardID and closes it when the test ends.
func New(t *testing.T, boardID string) *Board {
	t.Helper()
	board := &Board{boardID: boardID, clock: baseTime, items: map[string]*fakeItem{}, cursors: map[string][]fakeItem{}}
	board.Server = httptest.NewServer(http.HandlerFunc(board.serveGraphQL))
	t.Cleanup(board.Close)
	return board
}

// Create adds an active item with status Working on it.
func (board *Board) Create(itemID string, name string) {
	board.mutex.Lock()
	defer board.mutex.Unlock()
	now := board.tick()
	board.items[itemID] = &fakeItem{ID: itemID, Name: name, Status: "Working on it", GroupID: "topics", CreatedAt: now, UpdatedAt: now}
}

// SetStatus changes an item's status label and moves its last-updated time.
func (board *Board) SetStatus(itemID string, status string) {
	board.mutex.Lock()
	defer board.mutex.Unlock()
	item := board.items[itemID]
	item.Status, item.UpdatedAt = status, board.tick()
}

// Archive removes an item from items_page, as monday.com lists only active items.
func (board *Board) Archive(itemID string) {
	board.mutex.Lock()
	defer board.mutex.Unlock()
	delete(board.items, itemID)
}

// SetClock fixes the time of the next change, such as a second change within the same second.
func (board *Board) SetClock(at time.Time) {
	board.mutex.Lock()
	defer board.mutex.Unlock()
	board.clock = at.Add(-time.Second)
}

// RateLimitNextRequests answers the next count requests with HTTP 429, Retry-After, and the
// RATE_LIMIT_EXCEEDED code.
func (board *Board) RateLimitNextRequests(count int, retryAfterSeconds int) {
	board.mutex.Lock()
	defer board.mutex.Unlock()
	board.rateLimitedCalls, board.retryAfter = count, retryAfterSeconds
}

// ExhaustDailyLimitForNextRequests answers the next count requests with DAILY_LIMIT_EXCEEDED.
func (board *Board) ExhaustDailyLimitForNextRequests(count int) {
	board.mutex.Lock()
	defer board.mutex.Unlock()
	board.dailyLimitCalls = count
}

// Requests returns the recorded requests.
func (board *Board) Requests() []Request {
	board.mutex.Lock()
	defer board.mutex.Unlock()
	return append([]Request(nil), board.requests...)
}

// UpdatedAt returns an item's last-updated time.
func (board *Board) UpdatedAt(itemID string) time.Time {
	board.mutex.Lock()
	defer board.mutex.Unlock()
	return board.items[itemID].UpdatedAt
}

func (board *Board) tick() time.Time {
	board.clock = board.clock.Add(time.Second)
	return board.clock
}

type graphQLBody struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

func (board *Board) serveGraphQL(response http.ResponseWriter, request *http.Request) {
	var body graphQLBody
	decodeErr := json.NewDecoder(request.Body).Decode(&body)
	board.mutex.Lock()
	defer board.mutex.Unlock()
	operation := operationName(body.Query)
	reply := func(status int, payload any) {
		board.requests = append(board.requests, Request{Operation: operation, Variables: body.Variables, Status: status})
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(status)
		_ = json.NewEncoder(response).Encode(payload) // A failed write fails the client's read.
	}
	switch {
	case request.Method != http.MethodPost || decodeErr != nil || request.Header.Get("API-Version") == "":
		reply(http.StatusBadRequest, map[string]any{"errors": []any{map[string]any{"extensions": map[string]any{"code": "GRAPHQL_PARSE_FAILED"}}}})
	case request.Header.Get("Authorization") != Token:
		reply(http.StatusUnauthorized, map[string]any{"errors": []any{map[string]any{"extensions": map[string]any{"code": "UNAUTHORIZED"}}}})
	case board.rateLimitedCalls > 0:
		board.rateLimitedCalls--
		response.Header().Set("Retry-After", strconv.Itoa(board.retryAfter))
		reply(http.StatusTooManyRequests, map[string]any{"errors": []any{map[string]any{"extensions": map[string]any{"code": "RATE_LIMIT_EXCEEDED", "retry_in_seconds": board.retryAfter}}}})
	case board.dailyLimitCalls > 0:
		board.dailyLimitCalls--
		reply(http.StatusOK, map[string]any{"errors": []any{map[string]any{"extensions": map[string]any{"code": "DAILY_LIMIT_EXCEEDED"}}}})
	case operation == "ListBoardItems":
		reply(board.listFirstPage(body.Variables))
	case operation == "ListNextBoardItems":
		reply(board.listNextPage(body.Variables))
	case operation == "GetItem":
		reply(board.getItems(body.Variables))
	default:
		reply(http.StatusOK, map[string]any{"errors": []any{map[string]any{"extensions": map[string]any{"code": "undefinedField"}}}})
	}
}

// listFirstPage snapshots the board ordered by last-updated time, newest first, as the connector requests.
func (board *Board) listFirstPage(variables map[string]any) (int, any) {
	boardIDs, _ := variables["boardIds"].([]any)
	if len(boardIDs) != 1 || boardIDs[0] != board.boardID {
		return http.StatusOK, map[string]any{"data": map[string]any{"boards": []any{}}}
	}
	order := fmt.Sprint(variables["queryParams"])
	if order != "map[order_by:[map[column_id:__last_updated__ direction:desc]]]" {
		return http.StatusOK, map[string]any{"errors": []any{map[string]any{"extensions": map[string]any{"code": "InvalidArgumentException"}}}}
	}
	snapshot := make([]fakeItem, 0, len(board.items))
	for _, item := range board.items {
		snapshot = append(snapshot, *item)
	}
	sort.Slice(snapshot, func(i, j int) bool {
		if !snapshot[i].UpdatedAt.Equal(snapshot[j].UpdatedAt) {
			return snapshot[i].UpdatedAt.After(snapshot[j].UpdatedAt)
		}
		return snapshot[i].ID > snapshot[j].ID
	})
	page := board.page(snapshot, limit(variables))
	return http.StatusOK, map[string]any{"data": map[string]any{"boards": []any{map[string]any{"id": board.boardID, "items_page": page}}}}
}

func (board *Board) listNextPage(variables map[string]any) (int, any) {
	cursor, _ := variables["cursor"].(string)
	remaining, ok := board.cursors[cursor]
	if !ok {
		return http.StatusOK, map[string]any{"errors": []any{map[string]any{"extensions": map[string]any{"code": "InvalidArgumentException"}}}}
	}
	delete(board.cursors, cursor)
	return http.StatusOK, map[string]any{"data": map[string]any{"next_items_page": board.page(remaining, limit(variables))}}
}

func (board *Board) page(items []fakeItem, size int) map[string]any {
	resources := []any{}
	for index := 0; index < len(items) && index < size; index++ {
		resources = append(resources, board.resource(items[index]))
	}
	var cursor any
	if len(items) > size {
		board.nextCursor++
		key := "cursor-" + strconv.Itoa(board.nextCursor)
		board.cursors[key] = items[size:]
		cursor = key
	}
	return map[string]any{"cursor": cursor, "items": resources}
}

func (board *Board) getItems(variables map[string]any) (int, any) {
	itemIDs, _ := variables["itemIds"].([]any)
	resources := []any{}
	for _, itemID := range itemIDs {
		if item, ok := board.items[fmt.Sprint(itemID)]; ok {
			resources = append(resources, board.resource(*item))
		}
	}
	return http.StatusOK, map[string]any{"data": map[string]any{"items": resources}}
}

func (board *Board) resource(item fakeItem) map[string]any {
	statusIndex := map[string]int{"Working on it": 0, "Done": 1, "Stuck": 2}[item.Status]
	return map[string]any{
		"id": item.ID, "name": item.Name, "state": "active", "url": "https://contoso.monday.com/boards/" + board.boardID + "/pulses/" + item.ID,
		"created_at": item.CreatedAt.Format(time.RFC3339), "updated_at": item.UpdatedAt.Format(time.RFC3339), "creator_id": "48202303",
		"board": map[string]any{"id": board.boardID}, "group": map[string]any{"id": item.GroupID, "title": "Topics"},
		"column_values": []any{map[string]any{"id": "status", "type": "status", "text": item.Status, "value": fmt.Sprintf(`{"index":%d}`, statusIndex)}},
	}
}

func limit(variables map[string]any) int {
	if value, ok := variables["limit"].(float64); ok && value >= 1 {
		return int(value)
	}
	return 25
}

func operationName(query string) string {
	fields := strings.Fields(query)
	if len(fields) < 2 || fields[0] != "query" {
		return ""
	}
	name, _, _ := strings.Cut(fields[1], "(")
	return name
}
