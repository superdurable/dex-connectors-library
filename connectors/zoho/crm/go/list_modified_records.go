// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package crm

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	listModifiedRecordsOperation = "listModifiedRecords"

	// DefaultListModifiedRecordsLimit is the page size when ListModifiedRecordsInput.Limit is zero.
	DefaultListModifiedRecordsLimit = 100
	cursorTimeLayout                = "2006-01-02T15:04:05Z"
)

var modifiedRecordsCursorPattern = regexp.MustCompile(`^([0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z)/([0-9]{1,20})$`)

// ListModifiedRecordsInput lists records of one module changed at or after a point, oldest change
// first. Set ModifiedSince for the first page of a new feed, or Cursor to continue one.
type ListModifiedRecordsInput struct {
	// Module is the module API name, such as Deals.
	Module string `json:"module"`
	// Fields are 1 to 49 field API names to return; Modified_Time is always added.
	Fields []string `json:"fields"`
	// ModifiedSince starts a feed at records modified at or after this instant, in whole seconds.
	// Leave it zero when Cursor is set.
	ModifiedSince time.Time `json:"modifiedSince,omitzero"`
	// Cursor continues a feed after the last record of a previous page. Leave it empty when ModifiedSince is set.
	Cursor string `json:"cursor,omitempty"`
	// Limit is the page size, 1 to 200; zero uses DefaultListModifiedRecordsLimit.
	Limit int `json:"limit,omitempty"`
}

// ListModifiedRecordsOutput is one page of changed records and the watermark after them.
type ListModifiedRecordsOutput struct {
	// Module is the module API name that was listed.
	Module string `json:"module"`
	// Records are ordered by Modified_Time and then record ID.
	Records []Record `json:"records"`
	// HasMore reports that Zoho CRM holds further changed records now.
	HasMore bool `json:"hasMore,omitempty"`
	// Cursor is the watermark after the last record, or the input's starting point for an empty page.
	// Pass it back to read the next page, or store it to read the next changes in a later poll.
	Cursor string `json:"cursor"`
}

// ListModifiedRecordsOperation is the listModifiedRecords Query.
type ListModifiedRecordsOperation struct {
	client *Client
}

// modifiedRecordsWatermark is the cursor's position: records strictly after (modifiedAt, recordID).
// A zero recordID means at or after modifiedAt.
type modifiedRecordsWatermark struct {
	modifiedAt time.Time
	recordID   string
}

// Definition returns the immutable connector operation definition.
func (ListModifiedRecordsOperation) Definition() sdkgo.QueryDefinition {
	return ListModifiedRecordsDefinition
}

// Invoke sends POST /crm/v8/coql with the query BuildListModifiedRecordsQuery renders and returns the
// watermark after the last record. Paging by watermark instead of offset never skips a record whose
// change moves it to the end of the feed while the feed is read.
func (operation ListModifiedRecordsOperation) Invoke(call sdkgo.Call, input ListModifiedRecordsInput) sdkgo.QueryAttempt[ListModifiedRecordsOutput] {
	branches := queryBranches{providerRejected: ListModifiedRecordsBranchProviderRejected, invalidResponse: ListModifiedRecordsBranchInvalidResponse, defect: ListModifiedRecordsBranchDefect}
	query, watermark, err := buildListModifiedRecordsQuery(input)
	if err != nil {
		return sdkgo.NewQueryBranch(ListModifiedRecordsBranchDefect, ListModifiedRecordsOutput{}, crmFailurePointer(sdkgo.FailureValidation, listModifiedRecordsOperation, err.Error()), sdkgo.Receipt{})
	}
	session, cancel, failure := operation.client.startSession(call, listModifiedRecordsOperation)
	if failure != nil {
		return queryAttemptForSession[ListModifiedRecordsOutput](failure, branches)
	}
	defer cancel()
	result := operation.client.exchange(session, listModifiedRecordsOperation, crmRequest{method: http.MethodPost, path: coqlPath, payload: coqlRequestWire{SelectQuery: query}})
	receipt := operation.client.receipt(call, result.response, "")
	if attempt, isTerminal := queryAttemptForExchange[ListModifiedRecordsOutput](result, receipt, branches); isTerminal {
		return attempt
	}
	records, hasMore, err := decodeRecordPage(input.Module, result.response.body)
	if err == nil && len(records) > listModifiedRecordsLimit(input) {
		err = errors.New("Zoho CRM returned more records than the page size")
	}
	if err == nil {
		watermark, err = advanceWatermark(watermark, records)
	}
	if err != nil {
		return sdkgo.NewQueryBranch(ListModifiedRecordsBranchInvalidResponse, ListModifiedRecordsOutput{}, crmFailurePointer(sdkgo.FailureProtocol, listModifiedRecordsOperation, "Zoho CRM returned an invalid page: "+err.Error()), receipt)
	}
	return sdkgo.NewQueryBranch(ListModifiedRecordsBranchListed, ListModifiedRecordsOutput{
		Module: input.Module, Records: records, HasMore: hasMore && len(records) != 0, Cursor: watermark.String(),
	}, nil, receipt)
}

// BuildListModifiedRecordsQuery validates input and returns the COQL statement listModifiedRecords
// sends, such as select Deal_Name, Modified_Time from Deals where Modified_Time >= '2026-01-28T13:00:00+00:00'
// order by Modified_Time asc, id asc limit 0, 100. Invalid input returns an error that never repeats a value.
func BuildListModifiedRecordsQuery(input ListModifiedRecordsInput) (string, error) {
	query, _, err := buildListModifiedRecordsQuery(input)
	return query, err
}

func buildListModifiedRecordsQuery(input ListModifiedRecordsInput) (string, modifiedRecordsWatermark, error) {
	if err := validateModule(input.Module); err != nil {
		return "", modifiedRecordsWatermark{}, err
	}
	fields := input.Fields
	if !slices.Contains(fields, FieldModifiedTime) {
		fields = append(slices.Clone(fields), FieldModifiedTime)
	}
	if len(input.Fields) == 0 {
		return "", modifiedRecordsWatermark{}, fmt.Errorf("fields must name 1 to %d field API names", MaxFieldsPerRequest-1)
	}
	if err := validateFieldSelection(fields, 1); err != nil {
		return "", modifiedRecordsWatermark{}, err
	}
	limit := listModifiedRecordsLimit(input)
	if limit < 1 || limit > MaxRecordsPerPage {
		return "", modifiedRecordsWatermark{}, fmt.Errorf("limit must be 1 to %d", MaxRecordsPerPage)
	}
	watermark, err := startingWatermark(input)
	if err != nil {
		return "", modifiedRecordsWatermark{}, err
	}
	return renderSelectQuery(input.Module, fields, watermark.renderCondition(), FieldModifiedTime+" asc, id asc", 0, limit), watermark, nil
}

func startingWatermark(input ListModifiedRecordsInput) (modifiedRecordsWatermark, error) {
	switch {
	case input.Cursor != "" && !input.ModifiedSince.IsZero():
		return modifiedRecordsWatermark{}, errors.New("set modifiedSince to start a feed or cursor to continue one, not both")
	case input.Cursor != "":
		return parseModifiedRecordsCursor(input.Cursor)
	case input.ModifiedSince.IsZero():
		return modifiedRecordsWatermark{}, errors.New("modifiedSince or cursor is required")
	default:
		return modifiedRecordsWatermark{modifiedAt: input.ModifiedSince.UTC().Truncate(time.Second), recordID: "0"}, nil
	}
}

func parseModifiedRecordsCursor(cursor string) (modifiedRecordsWatermark, error) {
	matches := modifiedRecordsCursorPattern.FindStringSubmatch(cursor)
	if matches == nil {
		return modifiedRecordsWatermark{}, errors.New("cursor must be a cursor returned by listModifiedRecords")
	}
	modifiedAt, err := time.Parse(cursorTimeLayout, matches[1])
	if err != nil {
		return modifiedRecordsWatermark{}, errors.New("cursor must be a cursor returned by listModifiedRecords")
	}
	return modifiedRecordsWatermark{modifiedAt: modifiedAt, recordID: strings.TrimLeft(matches[2], "0")}, nil
}

// advanceWatermark requires records strictly after the watermark in ascending order and returns the last one's position.
func advanceWatermark(watermark modifiedRecordsWatermark, records []Record) (modifiedRecordsWatermark, error) {
	for _, record := range records {
		modifiedAt, isFound := record.ModifiedAt()
		if !isFound {
			return modifiedRecordsWatermark{}, errors.New("a record has no valid Modified_Time")
		}
		next := modifiedRecordsWatermark{modifiedAt: modifiedAt.UTC().Truncate(time.Second), recordID: strings.TrimLeft(record.ID, "0")}
		if !next.isAfter(watermark) {
			return modifiedRecordsWatermark{}, errors.New("records are not in ascending Modified_Time and id order after the cursor")
		}
		watermark = next
	}
	return watermark, nil
}

// isAfter compares (modifiedAt, recordID); record IDs compare as decimal numbers.
func (watermark modifiedRecordsWatermark) isAfter(other modifiedRecordsWatermark) bool {
	if !watermark.modifiedAt.Equal(other.modifiedAt) {
		return watermark.modifiedAt.After(other.modifiedAt)
	}
	if other.recordID == "" || other.recordID == "0" {
		return true
	}
	if len(watermark.recordID) != len(other.recordID) {
		return len(watermark.recordID) > len(other.recordID)
	}
	return watermark.recordID > other.recordID
}

func (watermark modifiedRecordsWatermark) renderCondition() string {
	at := "'" + formatZohoTime(watermark.modifiedAt) + "'"
	if watermark.recordID == "" || watermark.recordID == "0" {
		return FieldModifiedTime + " >= " + at
	}
	return "(" + FieldModifiedTime + " > " + at + " or (" + FieldModifiedTime + " = " + at + " and id > " + watermark.recordID + "))"
}

// String returns the cursor form, such as 2026-01-28T13:00:05Z/4150868000003194012.
func (watermark modifiedRecordsWatermark) String() string {
	recordID := watermark.recordID
	if recordID == "" {
		recordID = "0"
	}
	return watermark.modifiedAt.UTC().Format(cursorTimeLayout) + "/" + recordID
}

func listModifiedRecordsLimit(input ListModifiedRecordsInput) int {
	if input.Limit == 0 {
		return DefaultListModifiedRecordsLimit
	}
	return input.Limit
}
