// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package airtablefake serves one in-memory Airtable table over HTTP for the recordChanged Trigger tests:
// the base schema, POST listRecords with the Last modified time filter, sort, page size, and offset
// the Trigger sends, and GET of one record by ID for the example Flow's read-back. It never echoes the
// personal access token.
package airtablefake

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	// BaseID is the fake base's ID.
	BaseID = "appIntakeBase0001"
	// TableID is the fake table's ID.
	TableID = "tblIntakeLeads001"
	// FieldID is the fake Last modified time field's ID.
	FieldID = "fldLastModified01"
	// AccessToken is the only personal access token the fake accepts.
	AccessToken = "patFAKE.0123456789abcdef"
)

// filterPattern extracts the field ID and lower bound of the Trigger's IS_AFTER filter.
var filterPattern = regexp.MustCompile(`^IS_AFTER\(\{(fld[0-9A-Za-z]{14})\}, DATETIME_PARSE\("([^"]+)"\)\)$`)

// Record is one fake record. A zero ModifiedAt leaves the Last modified time field empty.
type Record struct {
	ID         string
	CreatedAt  time.Time
	ModifiedAt time.Time
	Fields     map[string]any
}

// ScriptedResponse replaces the next request's response.
type ScriptedResponse struct {
	Status     int
	RetryAfter string
	ErrorType  string
}

// ListRequest is one listRecords request body as the fake received it.
type ListRequest struct {
	PageSize        int    `json:"pageSize"`
	Offset          string `json:"offset"`
	FilterByFormula string `json:"filterByFormula"`
	Sort            []struct {
		Field     string `json:"field"`
		Direction string `json:"direction"`
	} `json:"sort"`
}

// Table is the fake provider. It is safe for concurrent use.
type Table struct {
	// URL is the fake API base URL, for Config.Endpoint.
	URL string

	mutex          sync.Mutex
	fieldName      string
	fieldType      string
	records        map[string]Record
	scripted       []ScriptedResponse
	listRequests   []ListRequest
	schemaRequests int
	completed      int
	// iterators maps each offset handed out to the request it continues, as Airtable binds an offset to its query.
	iterators map[string]string
}

// New starts the fake with a Last modified time field named "Last Modified" and the given records.
func New(t *testing.T, records ...Record) *Table {
	t.Helper()
	table := &Table{fieldName: "Last Modified", fieldType: "lastModifiedTime", records: map[string]Record{}, iterators: map[string]string{}}
	for _, record := range records {
		table.records[record.ID] = record
	}
	server := httptest.NewServer(http.HandlerFunc(table.serve))
	t.Cleanup(server.Close)
	table.URL = server.URL
	return table
}

// Put creates or replaces a record, as an edit in Airtable would.
func (table *Table) Put(records ...Record) {
	table.mutex.Lock()
	defer table.mutex.Unlock()
	for _, record := range records {
		table.records[record.ID] = record
	}
}

// RenameField renames the Last modified time field; its ID stays the same.
func (table *Table) RenameField(name string) {
	table.mutex.Lock()
	defer table.mutex.Unlock()
	table.fieldName = name
}

// SetFieldType changes the watched field's type in the schema.
func (table *Table) SetFieldType(fieldType string) {
	table.mutex.Lock()
	defer table.mutex.Unlock()
	table.fieldType = fieldType
}

// ExpireIterators forgets every offset handed out, as an Airtable iterator timeout or server restart does.
func (table *Table) ExpireIterators() {
	table.mutex.Lock()
	defer table.mutex.Unlock()
	clear(table.iterators)
}

// Script queues responses that replace the next requests' responses, in order.
func (table *Table) Script(responses ...ScriptedResponse) {
	table.mutex.Lock()
	defer table.mutex.Unlock()
	table.scripted = append(table.scripted, responses...)
}

// ListRequests returns every listRecords request body received so far.
func (table *Table) ListRequests() []ListRequest {
	table.mutex.Lock()
	defer table.mutex.Unlock()
	return slices.Clone(table.listRequests)
}

// SchemaRequests counts schema reads.
func (table *Table) SchemaRequests() int {
	table.mutex.Lock()
	defer table.mutex.Unlock()
	return table.schemaRequests
}

// CompletedRequests counts answered requests, scripted ones included.
func (table *Table) CompletedRequests() int {
	table.mutex.Lock()
	defer table.mutex.Unlock()
	return table.completed
}

func (table *Table) serve(writer http.ResponseWriter, request *http.Request) {
	table.mutex.Lock()
	defer table.mutex.Unlock()
	defer func() { table.completed++ }()
	if request.Header.Get("Authorization") != "Bearer "+AccessToken {
		writeError(writer, http.StatusUnauthorized, "AUTHENTICATION_REQUIRED")
		return
	}
	if len(table.scripted) > 0 {
		scripted := table.scripted[0]
		table.scripted = table.scripted[1:]
		if scripted.RetryAfter != "" {
			writer.Header().Set("Retry-After", scripted.RetryAfter)
		}
		writeError(writer, scripted.Status, scripted.ErrorType)
		return
	}
	switch {
	case request.Method == http.MethodGet && request.URL.Path == "/v0/meta/bases/"+BaseID+"/tables":
		table.schemaRequests++
		writeJSON(writer, map[string]any{"tables": []any{map[string]any{
			"id": TableID, "name": "Leads", "primaryFieldId": "fldLeadEmail00001",
			"fields": []any{
				map[string]any{"id": "fldLeadEmail00001", "name": "Email", "type": "email"},
				map[string]any{"id": FieldID, "name": table.fieldName, "type": table.fieldType, "options": map[string]any{
					"isValid": true, "referencedFieldIds": nil, "result": map[string]any{"type": "dateTime"},
				}},
			},
		}}})
	case request.Method == http.MethodPost && request.URL.Path == "/v0/"+BaseID+"/"+TableID+"/listRecords":
		table.serveList(writer, request)
	case request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, "/v0/"+BaseID+"/"+TableID+"/rec"):
		record, isFound := table.records[strings.TrimPrefix(request.URL.Path, "/v0/"+BaseID+"/"+TableID+"/")]
		if !isFound {
			writeError(writer, http.StatusNotFound, "NOT_FOUND")
			return
		}
		writeJSON(writer, table.providerRecord(record))
	default:
		writeError(writer, http.StatusNotFound, "NOT_FOUND")
	}
}

func (table *Table) serveList(writer http.ResponseWriter, request *http.Request) {
	var body ListRequest
	if err := json.NewDecoder(request.Body).Decode(&body); err != nil || body.PageSize < 1 || body.PageSize > 100 {
		writeError(writer, http.StatusUnprocessableEntity, "INVALID_REQUEST_UNKNOWN")
		return
	}
	table.listRequests = append(table.listRequests, body)
	match := filterPattern.FindStringSubmatch(body.FilterByFormula)
	if match == nil || match[1] != FieldID || len(body.Sort) != 1 || body.Sort[0].Field != FieldID {
		writeError(writer, http.StatusUnprocessableEntity, "INVALID_FILTER_BY_FORMULA")
		return
	}
	since, err := time.Parse(time.RFC3339Nano, match[2])
	if err != nil {
		writeError(writer, http.StatusUnprocessableEntity, "INVALID_FILTER_BY_FORMULA")
		return
	}
	var matched []Record
	for _, record := range table.records {
		if !record.ModifiedAt.IsZero() && record.ModifiedAt.After(since) {
			matched = append(matched, record)
		}
	}
	// Airtable gives no order between equal sort values; the fake uses record ID descending to expose that.
	slices.SortFunc(matched, func(left, right Record) int {
		if compared := left.ModifiedAt.Compare(right.ModifiedAt); compared != 0 {
			if body.Sort[0].Direction == "desc" {
				return -compared
			}
			return compared
		}
		return strings.Compare(right.ID, left.ID)
	})
	// Offsets name the next record, as Airtable's "itr.../rec..." offsets do, and work only with their query.
	query := body.FilterByFormula + "|" + body.Sort[0].Direction
	start := 0
	if body.Offset != "" {
		_, nextRecordID, isOffset := strings.Cut(body.Offset, "/")
		position := slices.IndexFunc(matched, func(record Record) bool { return record.ID == nextRecordID })
		if !isOffset || table.iterators[body.Offset] != query || position < 0 {
			writeError(writer, http.StatusUnprocessableEntity, "LIST_RECORDS_ITERATOR_NOT_AVAILABLE")
			return
		}
		start = position
	}
	end := min(start+body.PageSize, len(matched))
	records := make([]any, 0, end-start)
	for _, record := range matched[start:end] {
		records = append(records, table.providerRecord(record))
	}
	response := map[string]any{"records": records}
	if end < len(matched) {
		offset := "itr" + strconv.Itoa(len(table.iterators)) + "/" + matched[end].ID
		table.iterators[offset] = query
		response["offset"] = offset
	}
	writeJSON(writer, response)
}

// providerRecord renders a record as Airtable does, with the Last modified time field under its current name.
func (table *Table) providerRecord(record Record) map[string]any {
	fields := map[string]any{}
	for name, value := range record.Fields {
		fields[name] = value
	}
	if !record.ModifiedAt.IsZero() {
		fields[table.fieldName] = record.ModifiedAt.UTC().Format("2006-01-02T15:04:05.000Z")
	}
	return map[string]any{"id": record.ID, "createdTime": record.CreatedAt.UTC().Format("2006-01-02T15:04:05.000Z"), "fields": fields}
}

func writeJSON(writer http.ResponseWriter, value any) {
	writer.Header().Set("Content-Type", "application/json")
	// The httptest client reads what arrives; a failed write fails the test's own assertions.
	_ = json.NewEncoder(writer).Encode(value)
}

func writeError(writer http.ResponseWriter, status int, errorType string) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(map[string]any{"error": map[string]any{"type": errorType, "message": "SENTINEL provider message text"}})
}
