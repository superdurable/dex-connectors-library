// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package crm

import (
	"fmt"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	findRecordsOperation = "findRecords"
	coqlPath             = "/coql"

	// DefaultFindRecordsLimit is the page size when FindRecordsInput.Limit is zero.
	DefaultFindRecordsLimit = 10
	// MaxRecordsPerPage is the most records one findRecords or listModifiedRecords page returns.
	MaxRecordsPerPage = 200
	// MaxCOQLResultDepth is how far offset paging reaches for one set of conditions, Zoho CRM's COQL limit.
	MaxCOQLResultDepth = 100000
)

// FindRecordsInput finds records of one module whose fields match every condition.
type FindRecordsInput struct {
	// Module is the module API name, such as Contacts.
	Module string `json:"module"`
	// Fields are 1 to 50 field API names or lookup paths to return, such as Email and Account_Name;
	// the record ID is always returned.
	Fields []string `json:"fields"`
	// Conditions are 1 to 10 criteria that every returned record matches, such as Email = jane@acme.example.com.
	Conditions []RecordCondition `json:"conditions"`
	// Sort orders the page by one field and then by record ID; nil orders by record ID.
	Sort *RecordSort `json:"sort,omitempty"`
	// Limit is the page size, 1 to 200; zero uses DefaultFindRecordsLimit.
	Limit int `json:"limit,omitempty"`
	// Offset skips that many matches; pass the previous page's NextOffset to read the next page.
	Offset int `json:"offset,omitempty"`
}

// FindRecordsOutput is one page of matching records.
type FindRecordsOutput struct {
	// Module is the module API name that was queried.
	Module string `json:"module"`
	// Records are the page's matches in the requested order.
	Records []Record `json:"records"`
	// NextOffset is the Offset of the next page, or zero when this page is the last one Zoho CRM can serve.
	NextOffset int `json:"nextOffset,omitempty"`
}

// FindRecordsOperation is the findRecords Query.
type FindRecordsOperation struct {
	client *Client
}

type coqlRequestWire struct {
	SelectQuery string `json:"select_query"`
}

// Definition returns the immutable connector operation definition.
func (FindRecordsOperation) Definition() sdkgo.QueryDefinition { return FindRecordsDefinition }

// Invoke sends POST /crm/v8/coql with the query BuildFindRecordsQuery renders. Zoho CRM's COQL reads
// committed records without the Search API's indexing delay, so a record written by an earlier Step
// is found at once.
func (operation FindRecordsOperation) Invoke(call sdkgo.Call, input FindRecordsInput) sdkgo.QueryAttempt[FindRecordsOutput] {
	branches := queryBranches{providerRejected: FindRecordsBranchProviderRejected, invalidResponse: FindRecordsBranchInvalidResponse, defect: FindRecordsBranchDefect}
	query, err := BuildFindRecordsQuery(input)
	if err != nil {
		return sdkgo.NewQueryBranch(FindRecordsBranchDefect, FindRecordsOutput{}, crmFailurePointer(sdkgo.FailureValidation, findRecordsOperation, err.Error()), sdkgo.Receipt{})
	}
	session, cancel, failure := operation.client.startSession(call, findRecordsOperation)
	if failure != nil {
		return queryAttemptForSession[FindRecordsOutput](failure, branches)
	}
	defer cancel()
	result := operation.client.exchange(session, findRecordsOperation, crmRequest{method: http.MethodPost, path: coqlPath, payload: coqlRequestWire{SelectQuery: query}})
	receipt := operation.client.receipt(call, result.response, "")
	if attempt, isTerminal := queryAttemptForExchange[FindRecordsOutput](result, receipt, branches); isTerminal {
		return attempt
	}
	records, hasMore, err := decodeRecordPage(input.Module, result.response.body)
	if err != nil {
		return sdkgo.NewQueryBranch(FindRecordsBranchInvalidResponse, FindRecordsOutput{}, crmFailurePointer(sdkgo.FailureProtocol, findRecordsOperation, "Zoho CRM returned an invalid page: "+err.Error()), receipt)
	}
	limit := findRecordsLimit(input)
	if len(records) > limit {
		return sdkgo.NewQueryBranch(FindRecordsBranchInvalidResponse, FindRecordsOutput{}, crmFailurePointer(sdkgo.FailureProtocol, findRecordsOperation, "Zoho CRM returned more records than the page size"), receipt)
	}
	output := FindRecordsOutput{Module: input.Module, Records: records}
	if hasMore && len(records) != 0 && input.Offset+len(records) < MaxCOQLResultDepth {
		output.NextOffset = input.Offset + len(records)
	}
	if len(records) == 0 {
		return sdkgo.NewQueryBranch(FindRecordsBranchNotFound, output, nil, receipt)
	}
	return sdkgo.NewQueryBranch(FindRecordsBranchFound, output, nil, receipt)
}

// BuildFindRecordsQuery validates input and returns the COQL statement findRecords sends, such as
// select Email, Account_Name from Contacts where Email = 'jane@acme.example.com' order by id asc limit 0, 10.
// Invalid input returns an error that never repeats a value.
func BuildFindRecordsQuery(input FindRecordsInput) (string, error) {
	if err := validateModule(input.Module); err != nil {
		return "", err
	}
	if err := validateFieldSelection(input.Fields, 1); err != nil {
		return "", err
	}
	limit := findRecordsLimit(input)
	if limit < 1 || limit > MaxRecordsPerPage {
		return "", fmt.Errorf("limit must be 1 to %d", MaxRecordsPerPage)
	}
	if input.Offset < 0 || input.Offset+limit > MaxCOQLResultDepth {
		return "", fmt.Errorf("offset plus limit must not pass Zoho CRM's %d-record COQL depth", MaxCOQLResultDepth)
	}
	where, err := renderConditions(input.Conditions)
	if err != nil {
		return "", err
	}
	orderBy, err := renderOrderBy(input.Sort)
	if err != nil {
		return "", err
	}
	return renderSelectQuery(input.Module, input.Fields, where, orderBy, input.Offset, limit), nil
}

func findRecordsLimit(input FindRecordsInput) int {
	if input.Limit == 0 {
		return DefaultFindRecordsLimit
	}
	return input.Limit
}
