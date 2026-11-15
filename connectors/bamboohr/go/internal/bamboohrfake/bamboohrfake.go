// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package bamboohrfake serves the BambooHR API v1 endpoints the employeeChanged Trigger and its example
// use: Get Changed Employee IDs and Get Employee. It keeps one change per employee, as BambooHR does, and
// stamps changes with its own clock, which it also sends in the Date header. The clock advances one second
// per change and by Advance, as BambooHR's does between polls.
package bamboohrfake

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// APIKey is the only key the fake accepts. It is split so secret scanners do not mistake the 40-hex
// fixture for a real key.
const APIKey = "0123456789abcdef" + "0123456789abcdef01234567"

// Start is the fake clock's initial time.
var Start = time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)

// BambooHR is a credential-checking BambooHR fake. Its URL plus /api/v1 is the connector's API base URL.
type BambooHR struct {
	*httptest.Server
	t         *testing.T
	mutex     sync.Mutex
	clock     time.Time
	employees map[string]*employee
	requests  []url.Values

	rateLimitedChangeLists int
	rejectedChangeLists    int
}

type employee struct {
	fields      map[string]string
	action      string
	lastChanged time.Time
	isDeleted   bool
}

// New starts a fake whose clock reads Start.
func New(t *testing.T) *BambooHR {
	t.Helper()
	return NewAt(t, Start)
}

// NewAt starts a fake whose clock reads start, truncated to the second; tests against a shared Dex
// server pass a unique start so Trigger event IDs, and the Flow IDs derived from them, stay unique.
func NewAt(t *testing.T, start time.Time) *BambooHR {
	t.Helper()
	fake := &BambooHR{t: t, clock: start.UTC().Truncate(time.Second), employees: map[string]*employee{}}
	fake.Server = httptest.NewServer(http.HandlerFunc(fake.serveHTTP))
	t.Cleanup(fake.Close)
	return fake
}

// APIBaseURL returns the base URL for bamboohr.WithAPIBaseURL.
func (fake *BambooHR) APIBaseURL() string { return fake.URL + "/api/v1" }

// Insert adds an employee one second after the previous change, as an Inserted change.
func (fake *BambooHR) Insert(employeeID string, fields map[string]string) {
	fake.change(employeeID, "Inserted", fields)
}

// Update merges fields into an employee one second after the previous change, as an Updated change.
func (fake *BambooHR) Update(employeeID string, fields map[string]string) {
	fake.change(employeeID, "Updated", fields)
}

// Delete deletes an employee one second after the previous change, as a Deleted change.
func (fake *BambooHR) Delete(employeeID string) {
	fake.change(employeeID, "Deleted", nil)
}

// Advance moves the clock, and so the Date header, forward by duration without changing any employee.
func (fake *BambooHR) Advance(duration time.Duration) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.clock = fake.clock.Add(duration).Truncate(time.Second)
}

// InsertLate adds an employee stamped lastChanged without moving the clock, as a change BambooHR makes
// visible only after a later change was already listed.
func (fake *BambooHR) InsertLate(employeeID string, lastChanged time.Time) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.employees[employeeID] = &employee{fields: map[string]string{}, action: "Inserted", lastChanged: lastChanged.UTC().Truncate(time.Second)}
}

// RateLimitChangeLists answers the next count change-list requests with 429 and Retry-After: 1.
func (fake *BambooHR) RateLimitChangeLists(count int) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.rateLimitedChangeLists = count
}

// RejectChangeLists answers the next count change-list requests with 400, as BambooHR does for a since it
// cannot accept.
func (fake *BambooHR) RejectChangeLists(count int) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.rejectedChangeLists = count
}

// Clock returns the fake's current time, which the Date header also reports.
func (fake *BambooHR) Clock() time.Time {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	return fake.clock
}

// LatestChange returns the latest lastChanged of any employee, or the zero time without employees.
func (fake *BambooHR) LatestChange() time.Time {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	var latest time.Time
	for _, record := range fake.employees {
		if record.lastChanged.After(latest) {
			latest = record.lastChanged
		}
	}
	return latest
}

// ChangeListQueries returns the query of every change-list request, in arrival order.
func (fake *BambooHR) ChangeListQueries() []url.Values {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	return append([]url.Values(nil), fake.requests...)
}

func (fake *BambooHR) change(employeeID string, action string, fields map[string]string) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.clock = fake.clock.Add(time.Second)
	record := fake.employees[employeeID]
	if record == nil {
		record = &employee{fields: map[string]string{}}
		fake.employees[employeeID] = record
	}
	for name, value := range fields {
		record.fields[name] = value
	}
	record.action, record.lastChanged, record.isDeleted = action, fake.clock, action == "Deleted"
}

func (fake *BambooHR) serveHTTP(response http.ResponseWriter, request *http.Request) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	response.Header().Set("Date", fake.clock.Format(http.TimeFormat))
	expected := "Basic " + base64.StdEncoding.EncodeToString([]byte(APIKey+":x"))
	switch {
	case request.Header.Get("Authorization") != expected:
		response.WriteHeader(http.StatusUnauthorized)
	case request.Method == http.MethodGet && request.URL.Path == "/api/v1/employees/changed":
		fake.serveChangeList(response, request.URL.Query())
	case request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, "/api/v1/employees/"):
		fake.serveEmployee(response, strings.TrimPrefix(request.URL.Path, "/api/v1/employees/"), request.URL.Query().Get("fields"))
	default:
		response.WriteHeader(http.StatusNotFound)
	}
}

// serveChangeList treats since as exclusive and returns [] for an empty map, as PHP encodes one.
func (fake *BambooHR) serveChangeList(response http.ResponseWriter, query url.Values) {
	fake.requests = append(fake.requests, query)
	switch {
	case fake.rateLimitedChangeLists > 0:
		fake.rateLimitedChangeLists--
		response.Header().Set("Retry-After", "1")
		response.WriteHeader(http.StatusTooManyRequests)
		return
	case fake.rejectedChangeLists > 0:
		fake.rejectedChangeLists--
		fake.writeJSON(response, http.StatusBadRequest, map[string]any{"error": map[string]string{"code": "BadRequest"}})
		return
	}
	since, err := time.Parse(time.RFC3339, query.Get("since"))
	changeType := query.Get("type")
	if err != nil || (changeType != "" && changeType != "inserted" && changeType != "updated" && changeType != "deleted" && changeType != "all") {
		response.WriteHeader(http.StatusBadRequest)
		return
	}
	employees := map[string]any{}
	latest := ""
	for employeeID, record := range fake.employees {
		if !record.lastChanged.After(since) || (changeType != "" && changeType != "all" && !strings.EqualFold(record.action, changeType)) {
			continue
		}
		stamp := record.lastChanged.Format("2006-01-02T15:04:05+00:00")
		employees[employeeID] = map[string]string{"id": employeeID, "action": record.action, "lastChanged": stamp}
		latest = max(latest, stamp)
	}
	body := map[string]any{"latest": latest, "employees": employees}
	if len(employees) == 0 {
		body["employees"] = []any{}
	}
	fake.writeJSON(response, http.StatusOK, body)
}

func (fake *BambooHR) serveEmployee(response http.ResponseWriter, employeeID string, fields string) {
	record := fake.employees[employeeID]
	if _, err := strconv.ParseUint(employeeID, 10, 64); err != nil || record == nil || record.isDeleted {
		response.WriteHeader(http.StatusNotFound)
		return
	}
	body := map[string]string{"id": employeeID}
	for _, name := range strings.Split(fields, ",") {
		if value, ok := record.fields[name]; ok {
			body[name] = value
		}
	}
	fake.writeJSON(response, http.StatusOK, body)
}

func (fake *BambooHR) writeJSON(response http.ResponseWriter, status int, body any) {
	encoded, err := json.Marshal(body)
	if err != nil {
		fake.t.Errorf("encode fake BambooHR response: %v", err)
		response.WriteHeader(http.StatusInternalServerError)
		return
	}
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	if _, err := response.Write(encoded); err != nil {
		fake.t.Errorf("write fake BambooHR response: %v", err)
	}
}
