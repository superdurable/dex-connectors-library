// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package calendarsyncfake serves one Google Calendar's events.list full and incremental sync over HTTP
// for eventChanged tests. Sync tokens are "sync-<sequence>", and every insert, update, or cancel advances
// the sequence, as Google's documented sync protocol behaves.
package calendarsyncfake

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// forbiddenWithSyncToken lists events.list parameters Google rejects together with syncToken.
var forbiddenWithSyncToken = []string{"iCalUID", "orderBy", "privateExtendedProperty", "q", "sharedExtendedProperty", "timeMin", "timeMax", "updatedMin"}

var baseTime = time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)

// Request is one recorded events.list request.
type Request struct {
	// Path is the request path, which names the event for events.get.
	Path string
	// Query is the request query string.
	Query url.Values
	// Status is the HTTP status the fake answered.
	Status int
}

// Calendar is a stateful single-calendar fake that is safe for concurrent use.
type Calendar struct {
	*httptest.Server
	calendarID       string
	mutex            sync.Mutex
	sequence         int
	events           map[string]*storedEvent
	oldestValidToken int
	rateLimited      int
	rateLimitStatus  int
	rateLimitReason  string
	retryAfter       string
	requests         []Request
}

type storedEvent struct {
	id        string
	summary   string
	status    string
	createdAt int
	sequence  int
}

// New starts a fake serving calendarID; the test cleanup closes it.
func New(t *testing.T, calendarID string) *Calendar {
	t.Helper()
	fake := &Calendar{calendarID: calendarID, events: map[string]*storedEvent{}}
	fake.Server = httptest.NewServer(http.HandlerFunc(fake.serveEvents))
	t.Cleanup(fake.Close)
	return fake
}

// Insert creates a confirmed event.
func (fake *Calendar) Insert(eventID string, summary string) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.sequence++
	fake.events[eventID] = &storedEvent{id: eventID, summary: summary, status: "confirmed", createdAt: fake.sequence, sequence: fake.sequence}
}

// Update changes an existing event's summary.
func (fake *Calendar) Update(eventID string, summary string) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.sequence++
	event := fake.events[eventID]
	event.summary, event.sequence = summary, fake.sequence
}

// Cancel deletes an existing event; incremental sync then reports it with status cancelled.
func (fake *Calendar) Cancel(eventID string) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.sequence++
	event := fake.events[eventID]
	event.status, event.sequence = "cancelled", fake.sequence
}

// ExpireSyncTokens makes every sync token issued so far answer 410 Gone.
func (fake *Calendar) ExpireSyncTokens() {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.sequence++
	fake.oldestValidToken = fake.sequence
}

// RateLimitNextRequests answers the next count requests with 429 and the given Retry-After header value.
func (fake *Calendar) RateLimitNextRequests(count int, retryAfter string) {
	fake.ThrottleNextRequests(count, http.StatusTooManyRequests, "rateLimitExceeded", retryAfter)
}

// ThrottleNextRequests answers the next count requests with status, a Google error reason such as
// userRateLimitExceeded or quotaExceeded, and the given Retry-After header value.
func (fake *Calendar) ThrottleNextRequests(count int, status int, reason string, retryAfter string) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.rateLimited, fake.rateLimitStatus, fake.rateLimitReason, fake.retryAfter = count, status, reason, retryAfter
}

// Requests returns the recorded requests in arrival order.
func (fake *Calendar) Requests() []Request {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	return append([]Request(nil), fake.requests...)
}

// SyncToken returns the token a full sync would issue now.
func (fake *Calendar) SyncToken() string {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	return "sync-" + strconv.Itoa(fake.sequence)
}

func (fake *Calendar) serveEvents(writer http.ResponseWriter, request *http.Request) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	status, body := fake.answer(request)
	fake.requests = append(fake.requests, Request{Path: request.URL.Path, Query: request.URL.Query(), Status: status})
	writer.Header().Set("Content-Type", "application/json")
	if status == fake.rateLimitStatus && fake.retryAfter != "" {
		writer.Header().Set("Retry-After", fake.retryAfter)
	}
	writer.WriteHeader(status)
	_, _ = writer.Write([]byte(body)) // A failed write only fails the client's read, which the test observes.
}

func (fake *Calendar) answer(request *http.Request) (int, string) {
	query := request.URL.Query()
	eventsPath := "/calendars/" + url.PathEscape(fake.calendarID) + "/events"
	switch {
	case request.Method != http.MethodGet || !strings.HasPrefix(request.URL.EscapedPath(), eventsPath):
		return http.StatusNotFound, googleError(404, "notFound")
	case request.Header.Get("Authorization") == "":
		return http.StatusUnauthorized, googleError(401, "authError")
	case fake.rateLimited > 0:
		fake.rateLimited--
		return fake.rateLimitStatus, googleError(fake.rateLimitStatus, fake.rateLimitReason)
	case strings.HasPrefix(request.URL.EscapedPath(), eventsPath+"/"):
		return fake.answerGet(strings.TrimPrefix(request.URL.EscapedPath(), eventsPath+"/"))
	case request.URL.EscapedPath() != eventsPath:
		return http.StatusNotFound, googleError(404, "notFound")
	}
	after := -1
	if token := query.Get("syncToken"); token != "" {
		for _, name := range forbiddenWithSyncToken {
			if query.Has(name) {
				return http.StatusBadRequest, googleError(400, "invalid")
			}
		}
		parsed, err := strconv.Atoi(strings.TrimPrefix(token, "sync-"))
		if err != nil || !strings.HasPrefix(token, "sync-") || query.Get("showDeleted") == "false" {
			return http.StatusBadRequest, googleError(400, "invalid")
		}
		if parsed < fake.oldestValidToken {
			return http.StatusGone, googleError(410, "fullSyncRequired")
		}
		after = parsed
	}
	pageSize, err := strconv.Atoi(query.Get("maxResults"))
	if err != nil || pageSize < 1 {
		pageSize = 250
	}
	offset, snapshot := 0, fake.sequence
	if pageToken := query.Get("pageToken"); pageToken != "" {
		if _, err := fmt.Sscanf(pageToken, "page-%d-%d", &offset, &snapshot); err != nil {
			return http.StatusBadRequest, googleError(400, "invalid")
		}
	}
	items := fake.changedEvents(after, snapshot)
	end := min(offset+pageSize, len(items))
	resources := make([]string, 0, end-offset)
	for _, event := range items[offset:end] {
		resources = append(resources, event.resource())
	}
	continuation := fmt.Sprintf(`"nextSyncToken":"sync-%d"`, snapshot)
	if end < len(items) {
		continuation = fmt.Sprintf(`"nextPageToken":"page-%d-%d"`, end, snapshot)
	}
	return http.StatusOK, `{"kind":"calendar#events","items":[` + strings.Join(resources, ",") + `],` + continuation + `}`
}

// answerGet serves events.get, which returns cancelled events too.
func (fake *Calendar) answerGet(escapedEventID string) (int, string) {
	eventID, err := url.PathUnescape(escapedEventID)
	if err != nil || fake.events[eventID] == nil {
		return http.StatusNotFound, googleError(404, "notFound")
	}
	return http.StatusOK, fake.events[eventID].resource()
}

// changedEvents lists a full sync's live events (after < 0) or every event changed after the token.
func (fake *Calendar) changedEvents(after int, snapshot int) []*storedEvent {
	var items []*storedEvent
	for _, event := range fake.events {
		isFullSync := after < 0
		if event.sequence > snapshot || (isFullSync && event.status == "cancelled") || (!isFullSync && event.sequence <= after) {
			continue
		}
		items = append(items, event)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].sequence < items[j].sequence })
	return items
}

func (event *storedEvent) resource() string {
	updated := baseTime.Add(time.Duration(event.sequence) * time.Minute).Format(time.RFC3339)
	if event.status == "cancelled" {
		return fmt.Sprintf(`{"id":%q,"status":"cancelled","etag":"\"%d\"","updated":%q}`, event.id, event.sequence, updated)
	}
	created := baseTime.Add(time.Duration(event.createdAt) * time.Minute).Format(time.RFC3339)
	return fmt.Sprintf(`{"id":%q,"status":"confirmed","summary":%q,"etag":"\"%d\"","created":%q,"updated":%q,`+
		`"start":{"dateTime":"2026-03-10T10:00:00Z"},"end":{"dateTime":"2026-03-10T11:00:00Z"}}`,
		event.id, event.summary, event.sequence, created, updated)
}

func googleError(code int, reason string) string {
	return fmt.Sprintf(`{"error":{"code":%d,"errors":[{"reason":%q}]}}`, code, reason)
}
