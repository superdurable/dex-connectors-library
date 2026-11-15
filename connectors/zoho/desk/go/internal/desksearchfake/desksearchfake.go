// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package desksearchfake serves the Zoho Desk ticket search and ticket read endpoints the ticketChanged
// Trigger and its example Flow use: modifiedTimeRange filtering, ascending modifiedTime order, from and
// limit paging with Zoho Desk's documented bounds, a Date header from the fake's clock, an indexing delay, a
// ticket listed twice, and injected 429 and 422 responses.
package desksearchfake

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	// AccessToken is the only bearer token the fake accepts.
	AccessToken = "1000.zohoDeskTriggerAccess0123456789"
	// OrganizationID is the only orgId header the fake accepts.
	OrganizationID = "2389290"

	zohoTimestampLayout = "2006-01-02T15:04:05.000Z"
	maximumSearchLimit  = 100
	maximumSearchFrom   = 999
)

// Desk is a Zoho Desk stand-in whose clock the test advances; it is safe for concurrent use.
type Desk struct {
	*httptest.Server
	mutex         sync.Mutex
	isRealClock   bool
	clock         time.Time
	nextID        int64
	tickets       map[string]*ticket
	searches      []Search
	rateLimits    int
	retryAfter    string
	rejectRanges  int
	hiddenTickets map[string]bool
	duplicates    map[string]bool
	clientOffset  time.Duration
}

type ticket struct {
	id, subject, status, departmentID string
	createdAt, modifiedAt             time.Time
}

// Search is one received search request.
type Search struct {
	ModifiedTimeRange string
	From              int
	Limit             int
	DepartmentID      string
	Returned          int
}

// New starts a fake whose clock begins at start and moves only with Advance, or follows the wall clock
// when start is zero; the server closes at test cleanup.
func New(t *testing.T, start time.Time) *Desk {
	t.Helper()
	desk := &Desk{isRealClock: start.IsZero(), clock: start.UTC(), nextID: 1892000000200000, tickets: map[string]*ticket{}, hiddenTickets: map[string]bool{}, duplicates: map[string]bool{}}
	desk.Server = httptest.NewServer(http.HandlerFunc(desk.serveHTTP))
	t.Cleanup(desk.Close)
	return desk
}

// Now returns the fake's clock, which every response also sends in its Date header.
func (desk *Desk) Now() time.Time {
	desk.mutex.Lock()
	defer desk.mutex.Unlock()
	return desk.currentTime()
}

// ClientNow is the connector client's clock in tests: the fake's clock plus the offset SkewClientClock set.
func (desk *Desk) ClientNow() time.Time {
	desk.mutex.Lock()
	defer desk.mutex.Unlock()
	return desk.currentTime().Add(desk.clientOffset)
}

// SkewClientClock makes ClientNow run offset ahead of the fake's clock, or behind it for a negative offset.
func (desk *Desk) SkewClientClock(offset time.Duration) {
	desk.mutex.Lock()
	defer desk.mutex.Unlock()
	desk.clientOffset = offset
}

// ListTwice models a search index that lists the ticket a second time on the page holding it.
func (desk *Desk) ListTwice(id string) {
	desk.mutex.Lock()
	defer desk.mutex.Unlock()
	desk.duplicates[id] = true
}

func (desk *Desk) currentTime() time.Time {
	if desk.isRealClock {
		return time.Now().UTC().Truncate(time.Millisecond)
	}
	return desk.clock
}

// Advance moves the clock forward.
func (desk *Desk) Advance(duration time.Duration) {
	desk.mutex.Lock()
	defer desk.mutex.Unlock()
	desk.clock = desk.clock.Add(duration)
}

// Create adds a ticket created and modified now and returns its ID.
func (desk *Desk) Create(subject string, departmentID string) string {
	desk.mutex.Lock()
	defer desk.mutex.Unlock()
	desk.nextID++
	id := strconv.FormatInt(desk.nextID, 10)
	now := desk.currentTime()
	desk.tickets[id] = &ticket{id: id, subject: subject, status: "Open", departmentID: departmentID, createdAt: now, modifiedAt: now}
	return id
}

// CreateAt adds a ticket created and modified at instant, such as several tickets in one millisecond.
func (desk *Desk) CreateAt(subject string, departmentID string, instant time.Time) string {
	id := desk.Create(subject, departmentID)
	desk.mutex.Lock()
	defer desk.mutex.Unlock()
	desk.tickets[id].createdAt, desk.tickets[id].modifiedAt = instant.UTC(), instant.UTC()
	return id
}

// Update sets a ticket's status and moves its modified time to now.
func (desk *Desk) Update(id string, status string) {
	desk.mutex.Lock()
	defer desk.mutex.Unlock()
	desk.tickets[id].status, desk.tickets[id].modifiedAt = status, desk.currentTime()
}

// HideFromSearch models the indexing delay: the ticket is readable but absent from search until shown.
func (desk *Desk) HideFromSearch(id string, isHidden bool) {
	desk.mutex.Lock()
	defer desk.mutex.Unlock()
	desk.hiddenTickets[id] = isHidden
}

// RateLimitNext answers the next count searches with 429 and retryAfter, which may be empty.
func (desk *Desk) RateLimitNext(count int, retryAfter string) {
	desk.mutex.Lock()
	defer desk.mutex.Unlock()
	desk.rateLimits, desk.retryAfter = count, retryAfter
}

// RejectNextRanges answers the next count searches with 422 naming modifiedTimeRange.
func (desk *Desk) RejectNextRanges(count int) {
	desk.mutex.Lock()
	defer desk.mutex.Unlock()
	desk.rejectRanges = count
}

// Searches returns every search received, in order.
func (desk *Desk) Searches() []Search {
	desk.mutex.Lock()
	defer desk.mutex.Unlock()
	return slices.Clone(desk.searches)
}

func (desk *Desk) serveHTTP(response http.ResponseWriter, request *http.Request) {
	// Zoho Desk, like any HTTP server, dates each response with its own clock.
	response.Header().Set("Date", desk.Now().Format(http.TimeFormat))
	if request.Header.Get("Authorization") != "Zoho-oauthtoken "+AccessToken {
		writeJSON(response, http.StatusUnauthorized, map[string]any{"errorCode": "INVALID_OAUTH"})
		return
	}
	if request.Header.Get("orgId") != OrganizationID {
		writeJSON(response, http.StatusForbidden, map[string]any{"errorCode": "OAUTH_ORG_MISMATCH"})
		return
	}
	path := strings.TrimPrefix(request.URL.Path, "/api/v1")
	desk.mutex.Lock()
	defer desk.mutex.Unlock()
	switch {
	case request.Method == http.MethodGet && path == "/tickets/search":
		desk.search(response, request)
	case request.Method == http.MethodGet && strings.HasPrefix(path, "/tickets/"):
		desk.read(response, strings.Split(strings.TrimPrefix(path, "/tickets/"), "/"))
	default:
		writeJSON(response, http.StatusNotFound, map[string]any{"errorCode": "URL_NOT_FOUND"})
	}
}

// search serves GET /api/v1/tickets/search with modifiedTimeRange, sortBy=modifiedTime, from, and limit.
func (desk *Desk) search(response http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	from, fromErr := strconv.Atoi(query.Get("from"))
	limit, limitErr := strconv.Atoi(query.Get("limit"))
	recorded := Search{ModifiedTimeRange: query.Get("modifiedTimeRange"), From: from, Limit: limit, DepartmentID: query.Get("departmentId")}
	defer func() { desk.searches = append(desk.searches, recorded) }()
	if desk.rateLimits > 0 {
		desk.rateLimits--
		if desk.retryAfter != "" {
			response.Header().Set("Retry-After", desk.retryAfter)
		}
		writeJSON(response, http.StatusTooManyRequests, map[string]any{"errorCode": "THRESHOLD_EXCEEDED"})
		return
	}
	lower, upper, rangeErr := parseRange(recorded.ModifiedTimeRange)
	if desk.rejectRanges > 0 || rangeErr != nil {
		if desk.rejectRanges > 0 {
			desk.rejectRanges--
		}
		writeJSON(response, http.StatusUnprocessableEntity, map[string]any{"errorCode": "UNPROCESSABLE_ENTITY",
			"errors": []map[string]any{{"fieldName": "/modifiedTimeRange", "errorType": "invalid"}}})
		return
	}
	if fromErr != nil || limitErr != nil || from < 0 || from > maximumSearchFrom || limit < 1 || limit > maximumSearchLimit ||
		query.Get("sortBy") != "modifiedTime" {
		writeJSON(response, http.StatusUnprocessableEntity, map[string]any{"errorCode": "UNPROCESSABLE_ENTITY",
			"errors": []map[string]any{{"fieldName": "/from", "errorType": "invalid"}}})
		return
	}
	var matches []*ticket
	for _, candidate := range desk.tickets {
		if desk.hiddenTickets[candidate.id] || candidate.modifiedAt.Before(lower) || candidate.modifiedAt.After(upper) {
			continue
		}
		if recorded.DepartmentID != "" && candidate.departmentID != recorded.DepartmentID {
			continue
		}
		matches = append(matches, candidate)
	}
	slices.SortFunc(matches, func(left, right *ticket) int {
		if comparison := left.modifiedAt.Compare(right.modifiedAt); comparison != 0 {
			return comparison
		}
		return strings.Compare(left.id, right.id)
	})
	if from >= len(matches) {
		response.WriteHeader(http.StatusNoContent)
		return
	}
	page := matches[from:min(len(matches), from+limit)]
	data := make([]map[string]any, 0, len(page)+1)
	for _, item := range page {
		data = append(data, item.wire())
		if desk.duplicates[item.id] {
			data = append(data, item.wire())
		}
	}
	// A listed-twice ticket takes a page slot, as an index entry would.
	data = data[:min(len(data), limit)]
	recorded.Returned = len(data)
	writeJSON(response, http.StatusOK, map[string]any{"data": data, "count": strconv.Itoa(len(matches))})
}

// read serves the getTicket requests: the ticket, then its empty thread and comment lists.
func (desk *Desk) read(response http.ResponseWriter, segments []string) {
	found, isFound := desk.tickets[segments[0]]
	if !isFound {
		writeJSON(response, http.StatusNotFound, map[string]any{"errorCode": "URL_NOT_FOUND"})
		return
	}
	switch {
	case len(segments) == 1:
		writeJSON(response, http.StatusOK, found.wire())
	case len(segments) == 2 && (segments[1] == "threads" || segments[1] == "comments"):
		writeJSON(response, http.StatusOK, map[string]any{"data": []any{}})
	default:
		writeJSON(response, http.StatusNotFound, map[string]any{"errorCode": "URL_NOT_FOUND"})
	}
}

func (item *ticket) wire() map[string]any {
	return map[string]any{
		"id": item.id, "ticketNumber": strings.TrimPrefix(item.id, "18920000002"), "subject": item.subject,
		"description": "<div>" + item.subject + "</div>", "status": item.status, "statusType": statusType(item.status),
		"priority": "Medium", "channel": "Email", "departmentId": item.departmentID, "contactId": "1892000000042032",
		"email": "jane@example.test", "createdTime": item.createdAt.Format(zohoTimestampLayout),
		"modifiedTime": item.modifiedAt.Format(zohoTimestampLayout),
	}
}

func statusType(status string) string {
	switch status {
	case "Closed":
		return "Closed"
	case "On Hold":
		return "On Hold"
	default:
		return "Open"
	}
}

func parseRange(value string) (time.Time, time.Time, error) {
	lowerText, upperText, _ := strings.Cut(value, ",")
	lower, err := time.Parse(zohoTimestampLayout, lowerText)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	upper, err := time.Parse(zohoTimestampLayout, upperText)
	return lower, upper, err
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	encoded, err := json.Marshal(value)
	if err != nil {
		http.Error(response, "fake could not encode its response", http.StatusInternalServerError)
		return
	}
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_, _ = response.Write(encoded) // A test client that hung up has no use for the rest.
}
