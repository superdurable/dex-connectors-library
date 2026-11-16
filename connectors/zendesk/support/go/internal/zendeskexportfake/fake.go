// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package zendeskexportfake serves one Zendesk account's cursor-based incremental ticket export, ticket
// reads, and comment reads over HTTP for ticketChanged tests. Every create, update, or delete is one change
// with the next sequence number; the export lists each ticket once, at its latest change, and cursors are
// "c-<sequence>", as Zendesk's export reports each changed ticket's current state.
package zendeskexportfake

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"
)

// Email and APIToken are the only credentials the fake accepts, as {email}/token over HTTP Basic.
const (
	Email    = "admin@acme.example.com"
	APIToken = "fake-zendesk-api-token"
)

// Request is one recorded request.
type Request struct {
	// Path is the request path below the API base URL, such as /incremental/tickets/cursor.
	Path string
	// Query is the request query string.
	Query url.Values
	// Status is the HTTP status the fake answered.
	Status int
}

// Account is a stateful Zendesk account fake that is safe for concurrent use. Its URL plus /api/v2 is the
// API base URL.
type Account struct {
	*httptest.Server
	mutex          sync.Mutex
	sequence       int
	clock          time.Time
	tickets        map[int64]*storedTicket
	cursorEpoch    int
	rateLimited    int
	retryAfter     string
	isAgentOnly    bool
	requests       []Request
	maximumPerPage int
	isTyingChanges bool
}

type storedTicket struct {
	id        int64
	subject   string
	status    string
	createdAt time.Time
	updatedAt time.Time
	sequence  int
}

// New starts a fake account; the test cleanup closes it. Each change is stamped one minute before now, as
// Zendesk serves a change only once it is a minute old, and at least two seconds after the previous change,
// so generated_timestamp values are distinct and an update is more than a second after creation; TieChanges
// stamps a burst in one second instead.
func New(t *testing.T) *Account {
	t.Helper()
	fake := &Account{tickets: map[int64]*storedTicket{}, maximumPerPage: 1000}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v2/incremental/tickets/cursor", fake.serveExport)
	mux.HandleFunc("GET /api/v2/tickets/{id}", fake.serveTicket)
	mux.HandleFunc("GET /api/v2/tickets/{id}/comments", fake.serveComments)
	fake.Server = httptest.NewServer(fake.authenticated(mux))
	t.Cleanup(fake.Close)
	return fake
}

// APIBaseURL returns the URL support.WithAPIBaseURL takes.
func (fake *Account) APIBaseURL() string { return fake.URL + "/api/v2" }

// Create adds a new open ticket.
func (fake *Account) Create(ticketID int64, subject string) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	now := fake.advance()
	fake.tickets[ticketID] = &storedTicket{id: ticketID, subject: subject, status: "new", createdAt: now, updatedAt: now, sequence: fake.sequence}
}

// Update changes an existing ticket's status.
func (fake *Account) Update(ticketID int64, status string) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	now := fake.advance()
	ticket := fake.tickets[ticketID]
	ticket.status, ticket.updatedAt, ticket.sequence = status, now, fake.sequence
}

// Delete deletes an existing ticket; the export then reports it with status deleted unless exclude_deleted
// is true, and ticket reads answer 404.
func (fake *Account) Delete(ticketID int64) {
	fake.Update(ticketID, "deleted")
}

// InvalidateCursors makes every cursor issued so far answer HTTP 422.
func (fake *Account) InvalidateCursors() {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.cursorEpoch++
}

// RateLimitNextRequests answers the next count requests with 429 and the given Retry-After header value.
func (fake *Account) RateLimitNextRequests(count int, retryAfter string) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.rateLimited, fake.retryAfter = count, retryAfter
}

// RejectAsAgent makes the export answer 403, as Zendesk does for a user who is not an administrator.
func (fake *Account) RejectAsAgent(isAgentOnly bool) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.isAgentOnly = isAgentOnly
}

// LimitPerPage caps per_page, as a smaller provider page would.
func (fake *Account) LimitPerPage(maximumPerPage int) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.maximumPerPage = maximumPerPage
}

// TieChanges stamps every following change in the same second as the previous one while isTying is true,
// as a burst of changes shares one generated_timestamp in Zendesk.
func (fake *Account) TieChanges(isTying bool) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.isTyingChanges = isTying
}

// Cursor returns the cursor of the latest change, the after_cursor a caught-up export reports.
func (fake *Account) Cursor() string {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	return fake.epochCursor(fake.sequence)
}

// Requests returns the recorded requests in arrival order.
func (fake *Account) Requests() []Request {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	return append([]Request(nil), fake.requests...)
}

func (fake *Account) advance() time.Time {
	fake.sequence++
	servedAt := time.Now().UTC().Truncate(time.Second).Add(-time.Minute)
	if fake.isTyingChanges && !fake.clock.IsZero() {
		return fake.clock
	}
	fake.clock = fake.clock.Add(2 * time.Second)
	if servedAt.After(fake.clock) {
		fake.clock = servedAt
	}
	return fake.clock
}

func (fake *Account) authenticated(next http.Handler) http.Handler {
	expected := "Basic " + base64.StdEncoding.EncodeToString([]byte(Email+"/token:"+APIToken))
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != expected {
			fake.answer(writer, request, http.StatusUnauthorized, map[string]any{"error": "Couldn't authenticate you"})
			return
		}
		next.ServeHTTP(writer, request)
	})
}
