// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package hubspotsearchfake serves the HubSpot CRM search and record read endpoints over a deterministic
// in-memory record set, for tests of the crmObjectChanged Trigger and its example Flow.
package hubspotsearchfake

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// AccessToken is the only bearer token the fake accepts.
const AccessToken = "pat-na1-fake-hubspot-token"

const objectsPathPrefix = "/crm/objects/2026-09/"

// Record is one stored CRM record of one object type.
type Record struct {
	ID         string
	Properties map[string]string
	CreatedAt  time.Time
	ModifiedAt time.Time
	// IsIndexed reports whether search returns the record; reads by ID always find it.
	IsIndexed bool
}

// FailedResponse is one scripted error answer to the next search.
type FailedResponse struct {
	Status int
	Header http.Header
	Body   string
}

// Server is a running fake. Its methods are safe for concurrent use with requests.
type Server struct {
	// URL is the fake API base URL for Config.Endpoint.
	URL string

	mutex          sync.Mutex
	objectType     string
	clock          time.Time
	records        map[string]*Record
	nextID         int
	failures       []FailedResponse
	searchRequests int
	searchBodies   []map[string]any
}

// New starts a fake serving objectType, closed when the test ends. Its clock starts at start and moves
// one second per change, so modification times are deterministic.
func New(t *testing.T, objectType string, start time.Time) *Server {
	t.Helper()
	server := &Server{objectType: objectType, clock: start.UTC().Truncate(time.Millisecond), records: map[string]*Record{}, nextID: 1000}
	httpServer := httptest.NewServer(http.HandlerFunc(server.serveHTTP))
	t.Cleanup(httpServer.Close)
	server.URL = httpServer.URL
	return server
}

// Create stores a new indexed record and returns its ID.
func (server *Server) Create(properties map[string]string) string {
	return server.create(properties, true)
}

// CreateUnindexed stores a record that search omits until Index, as HubSpot does while it indexes.
func (server *Server) CreateUnindexed(properties map[string]string) string {
	return server.create(properties, false)
}

// CreateAtSameTime stores count indexed records that share one modification time and returns their IDs.
func (server *Server) CreateAtSameTime(count int, properties map[string]string) []string {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.clock = server.clock.Add(time.Second)
	ids := make([]string, 0, count)
	for range count {
		ids = append(ids, server.storeLocked(properties, true))
	}
	return ids
}

// Update sets properties on a record and moves its modification time forward.
func (server *Server) Update(id string, properties map[string]string) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	record := server.records[id]
	server.clock = server.clock.Add(time.Second)
	for name, value := range properties {
		record.Properties[name] = value
	}
	record.ModifiedAt = server.clock
}

// Index makes an unindexed record visible to search.
func (server *Server) Index(id string) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.records[id].IsIndexed = true
}

// FailNextSearches answers the next searches with responses, in order.
func (server *Server) FailNextSearches(responses ...FailedResponse) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.failures = append(server.failures, responses...)
}

// SearchRequestCount returns the number of search requests received.
func (server *Server) SearchRequestCount() int {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	return server.searchRequests
}

// SearchBodies returns the decoded search request bodies, in order.
func (server *Server) SearchBodies() []map[string]any {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	return slices.Clone(server.searchBodies)
}

func (server *Server) create(properties map[string]string, isIndexed bool) string {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.clock = server.clock.Add(time.Second)
	return server.storeLocked(properties, isIndexed)
}

func (server *Server) storeLocked(properties map[string]string, isIndexed bool) string {
	server.nextID++
	id := strconv.Itoa(server.nextID)
	stored := map[string]string{}
	for name, value := range properties {
		stored[name] = value
	}
	server.records[id] = &Record{ID: id, Properties: stored, CreatedAt: server.clock, ModifiedAt: server.clock, IsIndexed: isIndexed}
	return id
}

type searchRequest struct {
	FilterGroups []struct {
		Filters []struct {
			PropertyName string `json:"propertyName"`
			Operator     string `json:"operator"`
			Value        string `json:"value"`
		} `json:"filters"`
	} `json:"filterGroups"`
	Sorts []struct {
		PropertyName string `json:"propertyName"`
		Direction    string `json:"direction"`
	} `json:"sorts"`
	Properties []string `json:"properties"`
	Limit      int      `json:"limit"`
	After      string   `json:"after"`
}

func (server *Server) serveHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.Header.Get("Authorization") != "Bearer "+AccessToken {
		writeJSON(writer, http.StatusUnauthorized, map[string]any{"category": "INVALID_AUTHENTICATION"})
		return
	}
	suffix, isObjectPath := strings.CutPrefix(request.URL.Path, objectsPathPrefix+server.objectType+"/")
	switch {
	case isObjectPath && suffix == "search" && request.Method == http.MethodPost:
		server.serveSearch(writer, request)
	case isObjectPath && request.Method == http.MethodGet:
		server.serveRead(writer, suffix, request.URL.Query().Get("properties"))
	default:
		writeJSON(writer, http.StatusNotFound, map[string]any{"category": "OBJECT_NOT_FOUND"})
	}
}

func (server *Server) serveSearch(writer http.ResponseWriter, request *http.Request) {
	var raw map[string]any
	var search searchRequest
	if err := json.NewDecoder(request.Body).Decode(&raw); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]any{"category": "VALIDATION_ERROR"})
		return
	}
	encoded, _ := json.Marshal(raw) // Re-encoding a decoded map cannot fail.
	if err := json.Unmarshal(encoded, &search); err != nil || len(search.Sorts) > 1 || search.Limit < 1 || search.Limit > 200 {
		writeJSON(writer, http.StatusBadRequest, map[string]any{"category": "VALIDATION_ERROR"})
		return
	}
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.searchRequests++
	server.searchBodies = append(server.searchBodies, raw)
	if len(server.failures) > 0 {
		failure := server.failures[0]
		server.failures = server.failures[1:]
		for name, values := range failure.Header {
			writer.Header()[name] = values
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(failure.Status)
		_, _ = writer.Write([]byte(failure.Body)) // A failed write only ends this test response.
		return
	}
	offset, _ := strconv.Atoi(search.After)
	if offset+search.Limit > 10000 {
		writeJSON(writer, http.StatusBadRequest, map[string]any{"category": "VALIDATION_ERROR"})
		return
	}
	matched := server.matchingRecordsLocked(search)
	results := []map[string]any{}
	for _, record := range matched[min(offset, len(matched)):min(offset+search.Limit, len(matched))] {
		results = append(results, server.recordJSON(record, search.Properties))
	}
	response := map[string]any{"total": len(matched), "results": results}
	if offset+search.Limit < len(matched) {
		response["paging"] = map[string]any{"next": map[string]any{"after": strconv.Itoa(offset + search.Limit)}}
	}
	writeJSON(writer, http.StatusOK, response)
}

func (server *Server) matchingRecordsLocked(search searchRequest) []*Record {
	var matched []*Record
	for _, record := range server.records {
		if !record.IsIndexed {
			continue
		}
		isMatch := true
		for _, group := range search.FilterGroups {
			for _, filter := range group.Filters {
				threshold, err := strconv.ParseInt(filter.Value, 10, 64)
				if filter.PropertyName != server.modifiedProperty() || filter.Operator != "GTE" || err != nil || record.ModifiedAt.UnixMilli() < threshold {
					isMatch = false
				}
			}
		}
		if isMatch {
			matched = append(matched, record)
		}
	}
	isDescending := len(search.Sorts) == 1 && search.Sorts[0].Direction == "DESCENDING"
	slices.SortStableFunc(matched, func(left, right *Record) int {
		order := left.ModifiedAt.Compare(right.ModifiedAt)
		if order == 0 {
			order = len(left.ID) - len(right.ID)
			if order == 0 {
				order = strings.Compare(left.ID, right.ID)
			}
		}
		if isDescending {
			return -order
		}
		return order
	})
	return matched
}

func (server *Server) serveRead(writer http.ResponseWriter, id string, properties string) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	record, ok := server.records[id]
	if !ok {
		writeJSON(writer, http.StatusNotFound, map[string]any{"category": "OBJECT_NOT_FOUND"})
		return
	}
	var names []string
	if properties != "" {
		names = strings.Split(properties, ",")
	}
	writeJSON(writer, http.StatusOK, server.recordJSON(record, names))
}

// recordJSON returns the requested properties, or every property when none are named.
func (server *Server) recordJSON(record *Record, names []string) map[string]any {
	all := map[string]string{"hs_object_id": record.ID, "createdate": formatTime(record.CreatedAt), server.modifiedProperty(): formatTime(record.ModifiedAt)}
	for name, value := range record.Properties {
		all[name] = value
	}
	properties := map[string]any{}
	for name, value := range all {
		if len(names) == 0 || slices.Contains(names, name) {
			properties[name] = value
		}
	}
	return map[string]any{
		"id": record.ID, "properties": properties, "archived": false,
		"createdAt": formatTime(record.CreatedAt), "updatedAt": formatTime(record.ModifiedAt),
	}
}

func (server *Server) modifiedProperty() string {
	if server.objectType == "contacts" {
		return "lastmodifieddate"
	}
	return "hs_lastmodifieddate"
}

func formatTime(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05.000Z")
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		panic(fmt.Sprintf("hubspotsearchfake: encode response: %v", err))
	}
}
