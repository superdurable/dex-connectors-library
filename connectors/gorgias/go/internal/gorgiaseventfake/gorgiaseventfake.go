// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package gorgiaseventfake serves Gorgias's GET /api/events and GET /api/tickets/{id} over HTTP for
// ticketChanged tests. It stamps each event's created_datetime from the real clock at microsecond
// precision without an offset, as Gorgias documents, unless a test passes an explicit time.
package gorgiaseventfake

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const timeLayout = "2006-01-02T15:04:05.000000"

// Request is one recorded request.
type Request struct {
	// Path is the request path below /api.
	Path string
	// Query is the request query string.
	Query url.Values
	// Status is the HTTP status the fake answered.
	Status int
}

// Helpdesk is a stateful Gorgias event log and ticket store that is safe for concurrent use.
type Helpdesk struct {
	*httptest.Server
	// BaseURL is the fake's /api base URL for gorgias.WithAPIBaseURL.
	BaseURL      string
	email        string
	apiKey       string
	mutex        sync.Mutex
	nextEventID  int64
	events       []storedEvent
	tickets      map[int64]*storedTicket
	failures     []injectedFailure
	requests     []Request
	eventCount   int
	apiCallLimit string
}

type storedEvent struct {
	id         int64
	eventType  string
	objectType string
	objectID   int64
	userID     int64
	createdAt  time.Time
}

type storedTicket struct {
	id      int64
	subject string
	status  string
	tags    []string
}

type injectedFailure struct {
	status     int
	retryAfter string
	body       string
}

// New starts a fake that accepts email and apiKey over HTTP Basic; the test cleanup closes it.
func New(t *testing.T, email string, apiKey string) *Helpdesk {
	t.Helper()
	fake := &Helpdesk{email: email, apiKey: apiKey, nextEventID: 9000, tickets: map[int64]*storedTicket{}, apiCallLimit: "1/40"}
	fake.Server = httptest.NewServer(http.HandlerFunc(fake.serve))
	fake.BaseURL = fake.URL + "/api"
	t.Cleanup(fake.Close)
	return fake
}

// SetTicket stores the ticket that GET /api/tickets/{id} returns, without recording an event.
func (fake *Helpdesk) SetTicket(ticketID int64, subject string, status string, tags ...string) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.tickets[ticketID] = &storedTicket{id: ticketID, subject: subject, status: status, tags: tags}
}

// RecordTicketEvent appends an event about ticketID created now by userID (0 for a rule) and returns its ID.
func (fake *Helpdesk) RecordTicketEvent(eventType string, ticketID int64, userID int64) int64 {
	return fake.RecordEventAt(eventType, "Ticket", ticketID, userID, time.Now())
}

// RecordEventAt appends an event with an explicit object and created_datetime and returns its ID.
func (fake *Helpdesk) RecordEventAt(eventType string, objectType string, objectID int64, userID int64, createdAt time.Time) int64 {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.nextEventID++
	fake.events = append(fake.events, storedEvent{
		id: fake.nextEventID, eventType: eventType, objectType: objectType, objectID: objectID, userID: userID,
		createdAt: createdAt.UTC().Truncate(time.Microsecond),
	})
	return fake.nextEventID
}

// StartEventIDsAfter makes later events take IDs after lastEventID, so Flow IDs stay unique across runs.
func (fake *Helpdesk) StartEventIDsAfter(lastEventID int64) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.nextEventID = lastEventID
}

// FailNextEvents answers the next GET /api/events with status, an optional Retry-After value, and a body.
func (fake *Helpdesk) FailNextEvents(status int, retryAfter string, body string) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.failures = append(fake.failures, injectedFailure{status: status, retryAfter: retryAfter, body: body})
}

// SetAPICallLimit sets the X-Gorgias-Account-Api-Call-Limit value of later responses, such as 35/40.
func (fake *Helpdesk) SetAPICallLimit(value string) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.apiCallLimit = value
}

// Requests returns the recorded requests in arrival order.
func (fake *Helpdesk) Requests() []Request {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	return append([]Request(nil), fake.requests...)
}

// EventRequestCount returns how many GET /api/events requests arrived.
func (fake *Helpdesk) EventRequestCount() int {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	return fake.eventCount
}

func (fake *Helpdesk) serve(response http.ResponseWriter, request *http.Request) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	path := strings.TrimPrefix(request.URL.Path, "/api")
	status := fake.route(response, request, path)
	fake.requests = append(fake.requests, Request{Path: path, Query: request.URL.Query(), Status: status})
}

func (fake *Helpdesk) route(response http.ResponseWriter, request *http.Request, path string) int {
	response.Header().Set("Date", time.Now().UTC().Format(http.TimeFormat))
	response.Header().Set("X-Gorgias-Account-Api-Call-Limit", fake.apiCallLimit)
	if user, password, ok := request.BasicAuth(); !ok || user != fake.email || password != fake.apiKey {
		return writeJSON(response, http.StatusUnauthorized, map[string]any{"error": map[string]string{"msg": "invalid_credentials"}})
	}
	if request.Method != http.MethodGet {
		return writeJSON(response, http.StatusMethodNotAllowed, map[string]any{"error": map[string]string{"msg": "method_not_allowed"}})
	}
	if path == "/events" {
		fake.eventCount++
		return fake.listEvents(response, request.URL.Query())
	}
	ticketID, err := strconv.ParseInt(strings.TrimPrefix(path, "/tickets/"), 10, 64)
	ticket, exists := fake.tickets[ticketID]
	if err != nil || !exists {
		return writeJSON(response, http.StatusNotFound, map[string]any{"error": map[string]string{"msg": "not_found"}})
	}
	return writeJSON(response, http.StatusOK, ticketWire(ticket))
}

// listEvents serves types, order_by=created_datetime:asc, created_datetime[gte], and limit as a bare array.
func (fake *Helpdesk) listEvents(response http.ResponseWriter, query url.Values) int {
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
	since, err := time.Parse(time.RFC3339Nano, query.Get("created_datetime[gte]"))
	limit, limitErr := strconv.Atoi(query.Get("limit"))
	if err != nil || limitErr != nil || limit < 1 || limit > 100 || query.Get("order_by") != "created_datetime:asc" || len(query["types"]) == 0 {
		return writeJSON(response, http.StatusBadRequest, map[string]any{"error": map[string]any{"msg": "invalid_query", "data": map[string]any{"created_datetime": []string{"bad"}}}})
	}
	matched := make([]storedEvent, 0, len(fake.events))
	for _, event := range fake.events {
		if !event.createdAt.Before(since) && slices.Contains(query["types"], event.eventType) {
			matched = append(matched, event)
		}
	}
	sort.SliceStable(matched, func(i, j int) bool { return matched[i].createdAt.Before(matched[j].createdAt) })
	wires := make([]map[string]any, 0, min(limit, len(matched)))
	for _, event := range matched[:min(limit, len(matched))] {
		wire := map[string]any{
			"id": event.id, "type": event.eventType, "object_type": event.objectType, "object_id": event.objectID,
			"created_datetime": event.createdAt.Format(timeLayout), "data": nil, "context": "dd4ff312-69df-494a-be96-1a58b3d8b8e0",
			"uri": "/api/events/" + strconv.FormatInt(event.id, 10) + "/",
		}
		if event.userID > 0 {
			wire["user_id"] = event.userID
		} else {
			wire["user_id"] = nil
		}
		wires = append(wires, wire)
	}
	return writeJSON(response, http.StatusOK, wires)
}

func ticketWire(ticket *storedTicket) map[string]any {
	tags := make([]map[string]string, 0, len(ticket.tags))
	for _, tag := range ticket.tags {
		tags = append(tags, map[string]string{"name": tag})
	}
	created := "2026-10-20T09:00:00.000000"
	return map[string]any{
		"id": ticket.id, "subject": ticket.subject, "status": ticket.status, "priority": "normal", "channel": "email",
		"customer": map[string]any{"id": 501, "email": "ada@example.com", "name": "Ada Lovelace"}, "tags": tags,
		"created_datetime": created, "updated_datetime": created,
		"messages": []map[string]any{{
			"id": ticket.id*10 + 1, "ticket_id": ticket.id, "channel": "email", "via": "email", "public": true, "from_agent": false,
			"sender": map[string]any{"id": 501}, "subject": ticket.subject, "body_text": "Customer message for " + ticket.subject,
			"created_datetime": created,
		}},
	}
}

func writeJSON(response http.ResponseWriter, status int, value any) int {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value) // A failed write fails the test's decode.
	return status
}
