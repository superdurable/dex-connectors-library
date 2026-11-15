// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package freshdeskticketfake serves Freshdesk's List All Tickets, View a Ticket, and List All
// Conversations endpoints over HTTP for ticketChanged tests. It stamps created_at and updated_at from the
// real clock at one-second precision, as Freshdesk does, unless a test passes an explicit time.
package freshdeskticketfake

import (
	"encoding/json"
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

const (
	timeLayout  = "2006-01-02T15:04:05Z"
	maxListPage = 300
)

// Request is one recorded request.
type Request struct {
	// Path is the request path below /api/v2.
	Path string
	// Query is the request query string.
	Query url.Values
	// Status is the HTTP status the fake answered.
	Status int
}

// Tickets is a stateful Freshdesk ticket store that is safe for concurrent use.
type Tickets struct {
	*httptest.Server
	// BaseURL is the fake's /api/v2 base URL for freshdesk.WithAPIBaseURL.
	BaseURL   string
	apiKey    string
	mutex     sync.Mutex
	tickets   map[int64]*storedTicket
	failures  []injectedFailure
	requests  []Request
	listCount int
	clockSkew time.Duration
}

type storedTicket struct {
	id        int64
	subject   string
	status    int
	createdAt time.Time
	updatedAt time.Time
}

type injectedFailure struct {
	status     int
	retryAfter string
	body       string
}

// New starts a fake that accepts apiKey over HTTP Basic; the test cleanup closes it.
func New(t *testing.T, apiKey string) *Tickets {
	t.Helper()
	fake := &Tickets{apiKey: apiKey, tickets: map[int64]*storedTicket{}}
	fake.Server = httptest.NewServer(http.HandlerFunc(fake.serve))
	fake.BaseURL = fake.URL + "/api/v2"
	t.Cleanup(fake.Close)
	return fake
}

// Create adds an Open ticket created and updated now.
func (fake *Tickets) Create(ticketID int64, subject string) {
	now := time.Now().UTC().Truncate(time.Second)
	fake.CreateAt(ticketID, subject, now, now)
}

// CreateAt adds an Open ticket with explicit created_at and updated_at times.
func (fake *Tickets) CreateAt(ticketID int64, subject string, createdAt time.Time, updatedAt time.Time) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.tickets[ticketID] = &storedTicket{id: ticketID, subject: subject, status: 2, createdAt: createdAt.UTC(), updatedAt: updatedAt.UTC()}
}

// Update changes a ticket's subject and status and stamps updated_at now, at least one second after the
// previous updated_at so every update is a new version.
func (fake *Tickets) Update(ticketID int64, subject string, status int) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	ticket := fake.tickets[ticketID]
	ticket.subject, ticket.status = subject, status
	minimum := ticket.updatedAt.Add(time.Second)
	ticket.updatedAt = time.Now().UTC().Truncate(time.Second)
	if ticket.updatedAt.Before(minimum) {
		ticket.updatedAt = minimum
	}
}

// UpdateAt stamps a ticket's updated_at with an explicit time, as a change Freshdesk committed then.
func (fake *Tickets) UpdateAt(ticketID int64, subject string, updatedAt time.Time) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	ticket := fake.tickets[ticketID]
	ticket.subject, ticket.updatedAt = subject, updatedAt.UTC()
}

// AdvanceClock moves the time the Date header reports by duration, as minutes passing on Freshdesk's clock.
func (fake *Tickets) AdvanceClock(duration time.Duration) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.clockSkew += duration
}

// FailNextList answers the next List All Tickets request with status, an optional Retry-After value, and a
// Freshdesk error body.
func (fake *Tickets) FailNextList(status int, retryAfter string, body string) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.failures = append(fake.failures, injectedFailure{status: status, retryAfter: retryAfter, body: body})
}

// Requests returns the recorded requests in arrival order.
func (fake *Tickets) Requests() []Request {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	return append([]Request(nil), fake.requests...)
}

// ListRequestCount returns how many List All Tickets requests arrived.
func (fake *Tickets) ListRequestCount() int {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	return fake.listCount
}

func (fake *Tickets) serve(response http.ResponseWriter, request *http.Request) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	path := strings.TrimPrefix(request.URL.Path, "/api/v2")
	status := fake.route(response, request, path)
	fake.requests = append(fake.requests, Request{Path: path, Query: request.URL.Query(), Status: status})
}

func (fake *Tickets) route(response http.ResponseWriter, request *http.Request, path string) int {
	response.Header().Set("Date", time.Now().Add(fake.clockSkew).UTC().Format(http.TimeFormat))
	if user, password, ok := request.BasicAuth(); !ok || user != fake.apiKey || password != "X" {
		return writeJSON(response, http.StatusUnauthorized, map[string]string{"code": "invalid_credentials"})
	}
	if request.Method != http.MethodGet {
		return writeJSON(response, http.StatusMethodNotAllowed, map[string]string{"code": "method_not_allowed"})
	}
	if path == "/tickets" {
		fake.listCount++
		return fake.listTickets(response, request.URL.Query())
	}
	parts := strings.Split(strings.TrimPrefix(path, "/tickets/"), "/")
	ticketID, err := strconv.ParseInt(parts[0], 10, 64)
	ticket, exists := fake.tickets[ticketID]
	if err != nil || !exists || len(parts) > 2 || (len(parts) == 2 && parts[1] != "conversations") {
		return writeJSON(response, http.StatusNotFound, map[string]string{"code": "access_denied"})
	}
	if len(parts) == 2 {
		return writeJSON(response, http.StatusOK, []any{})
	}
	wire := ticketWire(ticket)
	wire["description_text"] = "Customer message for " + ticket.subject
	wire["requester"] = map[string]any{"id": 6007738334, "name": "Jane Smith", "email": "jane@acme.example.com"}
	return writeJSON(response, http.StatusOK, wire)
}

// listTickets serves updated_since, order_by=updated_at, order_type=asc, per_page, and page, with a Link
// header naming the next page up to page 300, as Freshdesk does. Pages are offsets into the current order,
// so a ticket updated between two page reads shifts later tickets onto an earlier page.
func (fake *Tickets) listTickets(response http.ResponseWriter, query url.Values) int {
	if len(fake.failures) != 0 {
		failure := fake.failures[0]
		fake.failures = fake.failures[1:]
		if failure.retryAfter != "" {
			response.Header().Set("Retry-After", failure.retryAfter)
		}
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(failure.status)
		_, _ = response.Write([]byte(failure.body)) // A failed write fails the test's decode.
		return failure.status
	}
	since, err := time.Parse(timeLayout, query.Get("updated_since"))
	perPage, perPageErr := strconv.Atoi(query.Get("per_page"))
	page, pageErr := strconv.Atoi(query.Get("page"))
	if err != nil || perPageErr != nil || pageErr != nil || perPage < 1 || perPage > 100 || page < 1 ||
		query.Get("order_by") != "updated_at" || query.Get("order_type") != "asc" {
		return writeJSON(response, http.StatusBadRequest, map[string]any{"description": "Validation failed",
			"errors": []map[string]string{{"field": "updated_since", "code": "invalid_value", "message": "bad"}}})
	}
	matched := make([]*storedTicket, 0, len(fake.tickets))
	for _, ticket := range fake.tickets {
		if !ticket.updatedAt.Before(since) {
			matched = append(matched, ticket)
		}
	}
	sort.Slice(matched, func(i, j int) bool {
		if !matched[i].updatedAt.Equal(matched[j].updatedAt) {
			return matched[i].updatedAt.Before(matched[j].updatedAt)
		}
		return matched[i].id < matched[j].id
	})
	start := min((page-1)*perPage, len(matched))
	end := min(start+perPage, len(matched))
	// Freshdesk returns at most 300 pages (30,000 tickets) for one list query.
	if end < len(matched) && page < maxListPage {
		next := url.Values{}
		for name, values := range query {
			next[name] = values
		}
		next.Set("page", strconv.Itoa(page+1))
		response.Header().Set("Link", `<`+fake.BaseURL+`/tickets?`+next.Encode()+`>; rel="next"`)
	}
	wires := make([]map[string]any, 0, end-start)
	for _, ticket := range matched[start:end] {
		wires = append(wires, ticketWire(ticket))
	}
	return writeJSON(response, http.StatusOK, wires)
}

func ticketWire(ticket *storedTicket) map[string]any {
	return map[string]any{
		"id": ticket.id, "subject": ticket.subject, "status": ticket.status, "priority": 1, "source": 2,
		"requester_id": 6007738334, "tags": []string{}, "created_at": ticket.createdAt.Format(timeLayout),
		"updated_at": ticket.updatedAt.Format(timeLayout),
	}
}

func writeJSON(response http.ResponseWriter, status int, value any) int {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value) // A failed write fails the test's decode.
	return status
}
