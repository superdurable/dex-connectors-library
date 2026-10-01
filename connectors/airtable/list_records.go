// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package airtable

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	// MaximumPageSize is Airtable's largest page of listed records.
	MaximumPageSize = 100
	// MaximumListedFields bounds the field subset of one listRecords call.
	MaximumListedFields = 500
	// MaximumRecordSorts bounds the sort entries of one listRecords call.
	MaximumRecordSorts = 10
	// maximumOffsetBytes bounds the opaque offset cursor Airtable returns.
	maximumOffsetBytes = 1024
)

// SortDirection orders listed records by one field.
type SortDirection string

const (
	// SortDirectionAscending sorts from the smallest value; it is the default.
	SortDirectionAscending SortDirection = "asc"
	// SortDirectionDescending sorts from the largest value.
	SortDirectionDescending SortDirection = "desc"
)

// RecordSort orders listed records by one field.
type RecordSort struct {
	// Field is the field name or field ID.
	Field string `json:"field"`
	// Direction is asc or desc; blank sorts ascending.
	Direction SortDirection `json:"direction,omitempty"`
}

// ListRecordsInput selects one page of a table's records.
//
// Filters and Formula combine with AND into Airtable's filterByFormula: typed
// equality filters are escaped by the connector, and Formula is passed through
// as written, so it should come from application code rather than end users.
// Leaving both blank lists every record. Without Sort or View, Airtable
// returns records in no particular order.
type ListRecordsInput struct {
	// BaseID is the base ID, such as appXXXXXXXXXXXXXX, from the base picker.
	BaseID string `json:"baseId"`
	// TableIDOrName is the table ID, such as tblXXXXXXXXXXXXXX from the table
	// picker, which survives renames, or the table name.
	TableIDOrName string `json:"tableIdOrName"`
	// Filters match records whose fields equal typed values, at most 25.
	Filters []FieldEqualityFilter `json:"filters,omitempty"`
	// Formula is an optional raw Airtable formula, such as {Amount} > 100, ANDed with Filters.
	Formula string `json:"formula,omitempty"`
	// Fields limits each returned record to these field names or IDs; blank returns every non-empty field.
	Fields []string `json:"fields,omitempty"`
	// Sort orders the records and overrides View's order, at most 10 entries.
	Sort []RecordSort `json:"sort,omitempty"`
	// ViewIDOrName limits the records to one view, in its order unless Sort is set.
	ViewIDOrName string `json:"viewIdOrName,omitempty"`
	// PageSize is the most records returned, 1 to 100; zero returns up to 100.
	PageSize int `json:"pageSize,omitempty"`
	// Offset continues a listing from the previous page's Offset; blank starts at the first page.
	Offset string `json:"offset,omitempty"`
}

// RecordPage is one page of listed records.
type RecordPage struct {
	// Records holds at most the requested page size of records; an empty page is an empty list.
	Records []Record `json:"records"`
	// Offset continues the listing on the next call; blank means this is the last page.
	Offset string `json:"offset,omitempty"`
	// FilterFormula is the filterByFormula the connector sent, blank when the call had no filter.
	FilterFormula string `json:"filterFormula,omitempty"`
}

// ListRecordsOperation implements the listRecords connector Query.
type ListRecordsOperation struct{ client *Client }

// listRecordsRequestBody is the JSON body of POST /v0/{baseId}/{tableIdOrName}/listRecords.
type listRecordsRequestBody struct {
	PageSize        int          `json:"pageSize"`
	Offset          string       `json:"offset,omitempty"`
	FilterByFormula string       `json:"filterByFormula,omitempty"`
	Fields          []string     `json:"fields,omitempty"`
	Sort            []RecordSort `json:"sort,omitempty"`
	View            string       `json:"view,omitempty"`
}

type listRecordsResponseBody struct {
	Records *[]providerRecord `json:"records"`
	Offset  string            `json:"offset"`
}

var listRecordsBranches = operationBranches{
	offsetExpired: ListRecordsBranchOffsetExpired, rejected: ListRecordsBranchProviderRejected,
	invalidResponse: ListRecordsBranchInvalidResponse, defect: ListRecordsBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (ListRecordsOperation) Definition() sdkgo.QueryDefinition { return ListRecordsDefinition }

// Invoke sends one listRecords request and classifies its attempt. It uses
// Airtable's POST listRecords form, which takes the same parameters as the GET
// list in a JSON body, so a long formula never meets Airtable's URL length limit.
func (operation ListRecordsOperation) Invoke(call sdkgo.Call, input ListRecordsInput) sdkgo.QueryAttempt[RecordPage] {
	requestBody, err := buildListRecordsRequestBody(input)
	if err != nil {
		return sdkgo.NewQueryBranch(ListRecordsBranchDefect, RecordPage{}, providerFailurePointer("listRecords", sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	encoded, err := json.Marshal(requestBody)
	if err != nil {
		return sdkgo.NewQueryBranch(ListRecordsBranchDefect, RecordPage{}, providerFailurePointer("listRecords", sdkgo.FailureLocalDefect, "listRecords request could not be encoded"), sdkgo.Receipt{})
	}
	objectID := input.BaseID + "/" + input.TableIDOrName
	response, outcome := operation.client.exchange(call, "listRecords", providerRequest{
		method: http.MethodPost, path: recordPath(input.BaseID, input.TableIDOrName) + "/listRecords",
		baseID: input.BaseID, body: encoded,
	}, objectID)
	if outcome != nil {
		return queryAttemptForOutcome[RecordPage](*outcome, listRecordsBranches)
	}
	receipt := operation.client.receipt(call, objectID)
	page, err := decodeRecordPage(response.body, requestBody.PageSize)
	if err != nil {
		return sdkgo.NewQueryBranch(ListRecordsBranchInvalidResponse, RecordPage{}, providerFailurePointer("listRecords", sdkgo.FailureProtocol, err.Error()), receipt)
	}
	page.FilterFormula = requestBody.FilterByFormula
	return sdkgo.NewQueryBranch(ListRecordsBranchListed, page, nil, receipt)
}

func buildListRecordsRequestBody(input ListRecordsInput) (listRecordsRequestBody, error) {
	if err := validateBaseAndTable(input.BaseID, input.TableIDOrName); err != nil {
		return listRecordsRequestBody{}, err
	}
	pageSize := input.PageSize
	if pageSize == 0 {
		pageSize = MaximumPageSize
	}
	if pageSize < 1 || pageSize > MaximumPageSize {
		return listRecordsRequestBody{}, fmt.Errorf("pageSize must be between 1 and %d", MaximumPageSize)
	}
	if len(input.Offset) > maximumOffsetBytes {
		return listRecordsRequestBody{}, fmt.Errorf("offset is limited to %d bytes", maximumOffsetBytes)
	}
	formula, err := buildFilterFormula(input.Filters, input.Formula)
	if err != nil {
		return listRecordsRequestBody{}, err
	}
	if len(input.Fields) > MaximumListedFields {
		return listRecordsRequestBody{}, fmt.Errorf("at most %d fields can be listed", MaximumListedFields)
	}
	fields, err := uniqueIdentifiers("fields", input.Fields)
	if err != nil {
		return listRecordsRequestBody{}, err
	}
	if len(input.Sort) > MaximumRecordSorts {
		return listRecordsRequestBody{}, fmt.Errorf("at most %d sort entries are allowed", MaximumRecordSorts)
	}
	for index, sort := range input.Sort {
		if err := validateIdentifier(fmt.Sprintf("sort[%d].field", index), sort.Field); err != nil {
			return listRecordsRequestBody{}, err
		}
		switch sort.Direction {
		case "", SortDirectionAscending, SortDirectionDescending:
		default:
			return listRecordsRequestBody{}, fmt.Errorf("sort[%d].direction must be asc or desc", index)
		}
	}
	if input.ViewIDOrName != "" {
		if err := validateIdentifier("viewIdOrName", input.ViewIDOrName); err != nil {
			return listRecordsRequestBody{}, err
		}
	}
	return listRecordsRequestBody{
		PageSize: pageSize, Offset: input.Offset, FilterByFormula: formula, Fields: fields,
		Sort: input.Sort, View: input.ViewIDOrName,
	}, nil
}

// uniqueIdentifiers validates names or IDs and drops repeats, keeping the first order.
func uniqueIdentifiers(label string, values []string) ([]string, error) {
	var unique []string
	seen := map[string]bool{}
	for index, value := range values {
		if err := validateIdentifier(fmt.Sprintf("%s[%d]", label, index), value); err != nil {
			return nil, err
		}
		if !seen[value] {
			seen[value] = true
			unique = append(unique, value)
		}
	}
	return unique, nil
}

func decodeRecordPage(body []byte, pageSize int) (RecordPage, error) {
	var decoded listRecordsResponseBody
	if err := decodeResponse(body, &decoded); err != nil {
		return RecordPage{}, err
	}
	if decoded.Records == nil {
		return RecordPage{}, errors.New("Airtable returned a list response without records")
	}
	if len(*decoded.Records) > pageSize {
		return RecordPage{}, errors.New("Airtable returned more records than the requested page size")
	}
	if len(decoded.Offset) > maximumOffsetBytes {
		return RecordPage{}, errors.New("Airtable returned an oversized offset")
	}
	records, err := convertProviderRecords(*decoded.Records)
	if err != nil {
		return RecordPage{}, err
	}
	return RecordPage{Records: records, Offset: decoded.Offset}, nil
}
