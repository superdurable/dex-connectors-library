// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package salesforcepollfake serves one Salesforce org's REST query and sObject Rows reads over HTTP for
// recordModifiedPolled tests. It answers only the SOQL statements the Trigger sends and stamps every
// change with a whole-second SystemModstamp that only moves forward, as Salesforce does.
package salesforcepollfake

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// DataPathPrefix is the REST path the default connector configuration uses.
const DataPathPrefix = "/services/data/v62.0"

const dateTimeLayout = "2006-01-02T15:04:05.000-0700"

var (
	seedQueryPattern    = regexp.MustCompile(`^SELECT Id, SystemModstamp FROM (\w+) WHERE SystemModstamp <= (\S+) ORDER BY SystemModstamp DESC, Id DESC LIMIT 1$`)
	changesQueryPattern = regexp.MustCompile(`^SELECT (.+) FROM (\w+) WHERE SystemModstamp <= (\S+) AND (?:SystemModstamp > (\S+)|\(SystemModstamp > (\S+) OR \(SystemModstamp = (\S+) AND Id > '(\w+)'\)\)) ORDER BY SystemModstamp ASC, Id ASC LIMIT ([0-9]+)$`)
)

// Request is one recorded REST request.
type Request struct {
	// Path is the request path.
	Path string
	// SOQL is the q parameter of a query request.
	SOQL string
	// Status is the HTTP status the fake answered.
	Status int
}

// Org is a stateful single-org fake that is safe for concurrent use.
type Org struct {
	*httptest.Server
	accessToken     string
	mutex           sync.Mutex
	clock           func() time.Time
	clockSkew       time.Duration
	lastStamp       time.Time
	nextID          int
	records         map[string]*storedRecord
	rejectedObjects map[string]string
	rateLimited     int
	retryAfter      string
	requests        []Request
}

type storedRecord struct {
	sObjectType string
	id          string
	created     time.Time
	modified    time.Time
	modstamp    time.Time
	fields      map[string]any
}

// New starts a fake that accepts only accessToken; the test cleanup closes it. Change stamps start one
// minute before the org's clock, so the Trigger's settle delay never hides them; clock nil uses time.Now.
func New(t testing.TB, accessToken string, clock func() time.Time) *Org {
	t.Helper()
	if clock == nil {
		clock = time.Now
	}
	org := &Org{accessToken: accessToken, clock: clock, records: map[string]*storedRecord{}, rejectedObjects: map[string]string{}, nextID: 1}
	org.lastStamp = clock().UTC().Add(-time.Minute).Truncate(time.Second)
	org.Server = httptest.NewServer(http.HandlerFunc(org.serve))
	t.Cleanup(org.Close)
	return org
}

// Insert creates a record of sObjectType with fields and returns its 18-character ID.
func (org *Org) Insert(sObjectType string, fields map[string]any) string {
	org.mutex.Lock()
	defer org.mutex.Unlock()
	stamp := org.advanceStamp()
	id := fmt.Sprintf("006RM%010dAAA", org.nextID)
	org.nextID++
	org.records[id] = &storedRecord{sObjectType: sObjectType, id: id, created: stamp, modified: stamp, modstamp: stamp, fields: copyFields(fields)}
	return id
}

// InsertBatch creates count records of sObjectType that share one SystemModstamp, as a bulk load does.
func (org *Org) InsertBatch(sObjectType string, count int, fields func(index int) map[string]any) []string {
	org.mutex.Lock()
	defer org.mutex.Unlock()
	stamp := org.advanceStamp()
	ids := make([]string, 0, count)
	for index := 0; index < count; index++ {
		id := fmt.Sprintf("006RM%010dAAA", org.nextID)
		org.nextID++
		org.records[id] = &storedRecord{sObjectType: sObjectType, id: id, created: stamp, modified: stamp, modstamp: stamp, fields: copyFields(fields(index))}
		ids = append(ids, id)
	}
	return ids
}

// InsertRecent creates a record stamped at the org's current second, as a change committed just now.
func (org *Org) InsertRecent(sObjectType string, fields map[string]any) string {
	org.mutex.Lock()
	defer org.mutex.Unlock()
	stamp := org.serverNow().Truncate(time.Second)
	if !stamp.After(org.lastStamp) {
		stamp = org.lastStamp.Add(time.Second)
	}
	org.lastStamp = stamp
	id := fmt.Sprintf("006RM%010dAAA", org.nextID)
	org.nextID++
	org.records[id] = &storedRecord{sObjectType: sObjectType, id: id, created: stamp, modified: stamp, modstamp: stamp, fields: copyFields(fields)}
	return id
}

// SetClockSkew makes the org's clock, which stamps changes and answers the Date header, run skew ahead
// of clock(); a negative skew puts Salesforce behind the client host.
func (org *Org) SetClockSkew(skew time.Duration) {
	org.mutex.Lock()
	defer org.mutex.Unlock()
	org.clockSkew = skew
}

// Update merges fields into the record and moves LastModifiedDate and SystemModstamp forward.
func (org *Org) Update(id string, fields map[string]any) {
	org.mutex.Lock()
	defer org.mutex.Unlock()
	record := org.records[id]
	stamp := org.advanceStamp()
	record.modified, record.modstamp = stamp, stamp
	for name, value := range fields {
		record.fields[name] = value
	}
}

// Delete removes the record; a poll never reports it, and a read answers 404 NOT_FOUND.
func (org *Org) Delete(id string) {
	org.mutex.Lock()
	defer org.mutex.Unlock()
	delete(org.records, id)
}

// RejectObject answers every query on sObjectType with 400 and errorCode, such as INVALID_TYPE for an
// object the user cannot see.
func (org *Org) RejectObject(sObjectType string, errorCode string) {
	org.mutex.Lock()
	defer org.mutex.Unlock()
	org.rejectedObjects[sObjectType] = errorCode
}

// RateLimitNext answers the next count requests with 403 REQUEST_LIMIT_EXCEEDED and retryAfter, if set.
func (org *Org) RateLimitNext(count int, retryAfter string) {
	org.mutex.Lock()
	defer org.mutex.Unlock()
	org.rateLimited, org.retryAfter = count, retryAfter
}

// Requests returns a copy of every request served so far.
func (org *Org) Requests() []Request {
	org.mutex.Lock()
	defer org.mutex.Unlock()
	return append([]Request(nil), org.requests...)
}

// QueryCount returns how many query requests were served.
func (org *Org) QueryCount() int {
	count := 0
	for _, request := range org.Requests() {
		if request.SOQL != "" {
			count++
		}
	}
	return count
}

// advanceStamp returns the next change stamp: one second after the last, or clock() minus one minute.
func (org *Org) advanceStamp() time.Time {
	stamp := org.lastStamp.Add(time.Second)
	if floor := org.serverNow().Add(-time.Minute).Truncate(time.Second); floor.After(stamp) {
		stamp = floor
	}
	org.lastStamp = stamp
	return stamp
}

func (org *Org) serverNow() time.Time {
	return org.clock().UTC().Add(org.clockSkew)
}

func (org *Org) serve(writer http.ResponseWriter, request *http.Request) {
	org.mutex.Lock()
	defer org.mutex.Unlock()
	recorded := Request{Path: request.URL.Path, SOQL: request.URL.Query().Get("q")}
	status, body := org.answer(request)
	recorded.Status = status
	org.requests = append(org.requests, recorded)
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Sforce-Limit-Info", "api-usage=25/15000")
	writer.Header().Set("Date", org.serverNow().Format(http.TimeFormat))
	if status == http.StatusForbidden && org.retryAfter != "" {
		writer.Header().Set("Retry-After", org.retryAfter)
	}
	writer.WriteHeader(status)
	_, _ = writer.Write(body) // The client reports a short write as a transport failure.
}

func (org *Org) answer(request *http.Request) (int, []byte) {
	if request.Header.Get("Authorization") != "Bearer "+org.accessToken {
		return http.StatusUnauthorized, errorBody("INVALID_SESSION_ID")
	}
	if org.rateLimited > 0 {
		org.rateLimited--
		return http.StatusForbidden, errorBody("REQUEST_LIMIT_EXCEEDED")
	}
	switch {
	case request.URL.Path == DataPathPrefix+"/query" && org.rejectedQueryErrorCode(request.URL.Query().Get("q")) != "":
		return http.StatusBadRequest, errorBody(org.rejectedQueryErrorCode(request.URL.Query().Get("q")))
	case request.URL.Path == DataPathPrefix+"/query":
		return org.answerQuery(request.URL.Query().Get("q"))
	case strings.HasPrefix(request.URL.Path, DataPathPrefix+"/sobjects/"):
		return org.answerRecordRead(strings.TrimPrefix(request.URL.Path, DataPathPrefix+"/sobjects/"), request.URL.Query().Get("fields"))
	default:
		return http.StatusNotFound, errorBody("NOT_FOUND")
	}
}

// rejectedQueryErrorCode returns the error code configured for the queried object, or "".
func (org *Org) rejectedQueryErrorCode(statement string) string {
	for sObjectType, errorCode := range org.rejectedObjects {
		if strings.Contains(statement, " FROM "+sObjectType+" ") {
			return errorCode
		}
	}
	return ""
}
