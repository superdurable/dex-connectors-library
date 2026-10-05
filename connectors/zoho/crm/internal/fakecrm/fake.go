// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package fakecrm is a stateful, credential-safe stand-in for the Zoho CRM API v8 and Zoho Accounts
// token endpoint that the examples' real-Dex integration tests drive. It answers the COQL subset the
// connector writes, upserts by duplicate-check field, updates, record reads, and field metadata.
// Its error bodies carry SENTINEL message text that the connector must never repeat.
package fakecrm

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// SlowResponseDelay outlasts Dex's roughly seven-second async local phase, so Dex dispatches an async Step again.
const SlowResponseDelay = 9 * time.Second

// zohoZone is the offset Zoho CRM uses for the fake organization's times, so the connector must convert to UTC.
var zohoZone = time.FixedZone("IST", 5*60*60+30*60)

// Request is one request the fake recorded.
type Request struct {
	// At is when the request arrived.
	At time.Time
	// Query is the URL query.
	Query url.Values
	// Form is the parsed body of a token request.
	Form url.Values
	// Body is the raw body.
	Body string
	// SelectQuery is the COQL statement of a coql request.
	SelectQuery string
}

// Server is the fake. Set the behavior fields before the first request.
type Server struct {
	*httptest.Server
	t     testing.TB
	mutex sync.Mutex
	// delayedRequests tracks handlers still sleeping, so a test can assert their final effect.
	delayedRequests sync.WaitGroup

	// AcceptedAccessToken is the only token Zoho CRM accepts.
	AcceptedAccessToken string
	// RefreshedAccessToken is the token the token endpoint issues; RefreshToken is the one it accepts.
	RefreshedAccessToken string
	RefreshToken         string
	// RefreshedAPIDomain is the api_domain the token endpoint returns.
	RefreshedAPIDomain string
	// RevokesTokenAtFirstUpsert makes Zoho CRM reject the accepted token from the first upsert on.
	RevokesTokenAtFirstUpsert bool
	// DelaysFirstUpsertOf applies the first upsert of the module, then answers after SlowResponseDelay.
	DelaysFirstUpsertOf string
	// DelaysFirstUpdate applies the first update, then answers after SlowResponseDelay.
	DelaysFirstUpdate bool
	// LosesFirstUpdateResponse applies the first update and drops the connection.
	LosesFirstUpdateResponse bool
	// RateLimitsFirstCOQL answers the first COQL query with 429 and Retry-After: 1.
	RateLimitsFirstCOQL bool
	// LocksUpdates answers every update with RECORD_LOCKED.
	LocksUpdates bool
	// RejectsUpsertField answers an upsert that sets this field with INVALID_DATA about it.
	RejectsUpsertField string
	// TouchesAfterFirstCOQL is a Deals record ID whose Modified_Time moves to now right after the
	// first COQL page is computed, as a change made by another user while a feed is read.
	TouchesAfterFirstCOQL string

	clock    time.Time
	nextID   int64
	records  map[string]map[string]map[string]any
	counts   map[string]int
	requests map[string][]Request
}

// New starts the fake for accessToken and stops it when the test ends.
func New(t testing.TB, accessToken string) *Server {
	t.Helper()
	server := &Server{
		t: t, AcceptedAccessToken: accessToken, clock: time.Date(2026, 1, 28, 13, 0, 0, 0, time.UTC), nextID: 4150868000005000000,
		records: map[string]map[string]map[string]any{}, counts: map[string]int{}, requests: map[string][]Request{},
	}
	server.Server = httptest.NewServer(http.HandlerFunc(server.serveHTTP))
	t.Cleanup(server.Close)
	return server
}

// SeedRecord stores a record of module modified at modifiedAt and returns its ID.
func (server *Server) SeedRecord(module string, fields map[string]any, modifiedAt time.Time) string {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	record := server.storeRecord(module, fields, modifiedAt)
	return record["id"].(string)
}

// Record returns a copy of a stored record, or nil.
func (server *Server) Record(module string, recordID string) map[string]any {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	record, isFound := server.records[module][recordID]
	if !isFound {
		return nil
	}
	copied := make(map[string]any, len(record))
	for key, value := range record {
		copied[key] = value
	}
	return copied
}

// RecordCount returns how many records module holds.
func (server *Server) RecordCount(module string) int {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	return len(server.records[module])
}

// Count returns how many requests of a kind arrived: coql, upsert, update, get, fields, or token.
func (server *Server) Count(name string) int {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	return server.counts[name]
}

// RequestsNamed returns the recorded requests of a kind in arrival order.
func (server *Server) RequestsNamed(name string) []Request {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	return append([]Request(nil), server.requests[name]...)
}

// LastRequest returns the latest request of a kind; the test fails when there is none.
func (server *Server) LastRequest(name string) Request {
	requests := server.RequestsNamed(name)
	if len(requests) == 0 {
		server.t.Fatalf("no fake Zoho CRM %s request", name)
	}
	return requests[len(requests)-1]
}

// TotalRequests counts every recorded request.
func (server *Server) TotalRequests() int {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	total := 0
	for _, requests := range server.requests {
		total += len(requests)
	}
	return total
}

// WaitForDelayedRequests waits until every sleeping handler has answered.
func (server *Server) WaitForDelayedRequests(t testing.TB) {
	t.Helper()
	finished := make(chan struct{})
	go func() {
		server.delayedRequests.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(2 * SlowResponseDelay):
		t.Fatal("a delayed fake Zoho CRM request did not finish")
	}
}

// AccountsRoutingClient sends requests for accountsHost and apiHost to the fake and refuses every other host.
func (server *Server) AccountsRoutingClient(timeout time.Duration, accountsHost string, apiHost string) *http.Client {
	target, err := url.Parse(server.URL)
	if err != nil {
		server.t.Fatalf("fake Zoho CRM URL: %v", err)
	}
	return &http.Client{Timeout: timeout, Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host != accountsHost && request.URL.Host != apiHost {
			return nil, errors.New("request to an unexpected host")
		}
		routed := request.Clone(request.Context())
		routed.URL.Scheme, routed.URL.Host = target.Scheme, target.Host
		return http.DefaultTransport.RoundTrip(routed)
	})}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func (server *Server) serveHTTP(response http.ResponseWriter, request *http.Request) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		server.writeJSON(response, http.StatusBadRequest, `{"code":"INVALID_DATA","status":"error"}`)
		return
	}
	if request.URL.Path == "/oauth/v2/token" {
		server.refreshToken(response, request, body)
		return
	}
	path := strings.TrimPrefix(request.URL.Path, "/crm/v8")
	segments := strings.Split(strings.TrimPrefix(path, "/"), "/")
	isUpsert := request.Method == http.MethodPost && len(segments) == 2 && segments[1] == "upsert"
	server.mutex.Lock()
	if server.RevokesTokenAtFirstUpsert && isUpsert {
		server.AcceptedAccessToken, server.RevokesTokenAtFirstUpsert = server.RefreshedAccessToken, false
	}
	acceptedAccessToken := server.AcceptedAccessToken
	server.mutex.Unlock()
	if request.Header.Get("Authorization") != "Zoho-oauthtoken "+acceptedAccessToken {
		if isUpsert {
			server.record("upsert", request, body, "")
		}
		server.writeJSON(response, http.StatusUnauthorized, `{"code":"INVALID_TOKEN","details":{},"message":"SENTINEL invalid oauth token","status":"error"}`)
		return
	}
	switch {
	case request.Method == http.MethodPost && path == "/coql":
		server.runCOQL(response, request, body)
	case request.Method == http.MethodGet && path == "/settings/fields":
		server.listFields(response, request, body)
	case isUpsert:
		server.upsert(response, request, body, segments[0])
	case request.Method == http.MethodPut && len(segments) == 2:
		server.update(response, request, body, segments[0], segments[1])
	case request.Method == http.MethodGet && len(segments) == 2:
		server.getRecord(response, request, body, segments[0], segments[1])
	default:
		server.writeJSON(response, http.StatusNotFound, `{"code":"INVALID_URL_PATTERN","details":{},"message":"SENTINEL","status":"error"}`)
	}
}

// refreshToken answers like Zoho Accounts: HTTP 200 with a one-hour token, an api_domain, and no refresh token.
func (server *Server) refreshToken(response http.ResponseWriter, request *http.Request, body []byte) {
	server.record("token", request, body, "")
	form, err := url.ParseQuery(string(body))
	if err != nil || form.Get("grant_type") != "refresh_token" || form.Get("refresh_token") != server.RefreshToken {
		server.writeJSON(response, http.StatusOK, `{"error":"invalid_code"}`)
		return
	}
	server.writeValue(response, http.StatusOK, map[string]any{
		"access_token": server.RefreshedAccessToken, "api_domain": server.RefreshedAPIDomain, "token_type": "Bearer", "expires_in": 3600,
	})
}

func (server *Server) runCOQL(response http.ResponseWriter, request *http.Request, body []byte) {
	var input struct {
		SelectQuery string `json:"select_query"`
	}
	if json.Unmarshal(body, &input) != nil {
		server.writeJSON(response, http.StatusBadRequest, `{"code":"INVALID_DATA","status":"error","message":"SENTINEL"}`)
		return
	}
	attempt := server.record("coql", request, body, input.SelectQuery)
	if server.RateLimitsFirstCOQL && attempt == 1 {
		response.Header().Set("Retry-After", "1")
		server.writeJSON(response, http.StatusTooManyRequests, `{"code":"TOO_MANY_REQUESTS","details":{},"message":"SENTINEL","status":"error"}`)
		return
	}
	query, err := parseSelectQuery(input.SelectQuery)
	if err != nil || query.limit < 1 || query.limit > 2000 {
		server.writeJSON(response, http.StatusBadRequest, `{"code":"SYNTAX_ERROR","details":{},"message":"SENTINEL","status":"error"}`)
		return
	}
	server.mutex.Lock()
	var matches []map[string]any
	for _, record := range server.records[query.module] {
		if query.criterion.matches(record) {
			matches = append(matches, record)
		}
	}
	sortRecords(matches, query.orderBy)
	var page []any
	for index := query.offset; index < len(matches) && index < query.offset+query.limit; index++ {
		page = append(page, projectCOQLRecord(matches[index], query.fields))
	}
	if touched, isFound := server.records["Deals"][server.TouchesAfterFirstCOQL]; isFound && attempt == 1 {
		touched["Modified_Time"] = formatZohoTime(server.advanceClock())
	}
	server.mutex.Unlock()
	if len(page) == 0 {
		response.WriteHeader(http.StatusNoContent)
		return
	}
	server.writeValue(response, http.StatusOK, map[string]any{
		"data": page, "info": map[string]any{"count": len(page), "more_records": query.offset+query.limit < len(matches)},
	})
}

// projectCOQLRecord returns the selected fields; COQL returns only the id of a lookup.
func projectCOQLRecord(record map[string]any, fields []string) map[string]any {
	projected := map[string]any{"id": record["id"]}
	for _, field := range fields {
		value := record[field]
		if lookup, isLookup := value.(map[string]any); isLookup {
			value = map[string]any{"id": lookup["id"]}
		}
		projected[field] = value
	}
	return projected
}

func sortRecords(records []map[string]any, orderBy []orderTerm) {
	sort.SliceStable(records, func(left int, right int) bool {
		for _, term := range orderBy {
			leftValue, _ := comparableValue(records[left], term.field)
			rightValue, _ := comparableValue(records[right], term.field)
			comparison := compareValues(leftValue, literal{text: rightValue, isQuoted: true})
			if comparison == 0 {
				continue
			}
			return (comparison < 0) != term.isDescending
		}
		return false
	})
}
