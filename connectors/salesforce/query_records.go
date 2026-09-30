// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package salesforce

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	queryRecordsOperationID = "queryRecords"
	// maxEncodedQueryBytes keeps the query string under Salesforce's 16,384-byte REST URI limit.
	maxEncodedQueryBytes = 15000
)

// queryLocatorPattern matches the final nextRecordsUrl segment, such as 01gRM000001Bxy1YAC-2000.
var queryLocatorPattern = regexp.MustCompile(`^[A-Za-z0-9]{15,18}-[0-9]{1,10}$`)

// QueryRecordsInput selects one page of a SOQL query. Set exactly one of SOQL
// and NextRecordsCursor.
//
// SOQL values are never concatenated into the statement: write a :name
// placeholder, as in Apex, and bind a typed value such as SOQLString(email)
// under that name. Placeholders inside string literals are left untouched, and
// every binding must be used exactly by name.
type QueryRecordsInput struct {
	// SOQL is one SOQL statement with :name placeholders, such as
	// SELECT Id, Name FROM Contact WHERE Email = :email LIMIT 10.
	SOQL string `json:"soql,omitempty"`
	// Bindings maps each placeholder name, without its colon, to a typed value.
	Bindings map[string]SOQLValue `json:"bindings,omitempty"`
	// NextRecordsCursor continues a query with the cursor a previous page returned.
	NextRecordsCursor string `json:"nextRecordsCursor,omitempty"`
	// BatchSize requests from 200 to 2000 records per page; zero uses the
	// connection's queryBatchSize. Salesforce treats it as a hint.
	BatchSize int `json:"batchSize,omitempty"`
}

// QueryRecordsOutput is one page of query results.
type QueryRecordsOutput struct {
	// Records lists the page's records in Salesforce's order. An aggregate
	// query such as SELECT COUNT() FROM Contact returns no records and reports
	// its count in TotalSize.
	Records []Record `json:"records"`
	// TotalSize is the number of rows the whole query matches, across every page.
	TotalSize int64 `json:"totalSize"`
	// IsDone reports that this is the last page.
	IsDone bool `json:"isDone"`
	// NextRecordsCursor continues the query on the next page; blank when IsDone
	// is true. Salesforce expires an unused cursor after about 15 minutes.
	NextRecordsCursor string `json:"nextRecordsCursor,omitempty"`
}

// QueryRecordsOperation implements the queryRecords connector operation.
type QueryRecordsOperation struct{ client *Client }

type queryResponse struct {
	TotalSize      *int64            `json:"totalSize"`
	Done           *bool             `json:"done"`
	NextRecordsURL string            `json:"nextRecordsUrl"`
	Records        []json.RawMessage `json:"records"`
}

// A 404 from the query resource means its path does not exist, not an empty result.
var queryRecordsBranches = operationBranches{
	operationID: queryRecordsOperationID, notFound: QueryRecordsBranchProviderRejected,
	requestRejected: QueryRecordsBranchQueryRejected, providerRejected: QueryRecordsBranchProviderRejected,
	invalidResponse: QueryRecordsBranchInvalidResponse, defect: QueryRecordsBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (QueryRecordsOperation) Definition() sdkgo.QueryDefinition { return QueryRecordsDefinition }

// Invoke renders the bound SOQL statement or follows the cursor, sends one
// query request, and classifies its attempt. Invalid input, bindings, or cursors
// select defect without a provider request; a query that matches no rows selects notFound.
func (operation QueryRecordsOperation) Invoke(call sdkgo.Call, input QueryRecordsInput) sdkgo.QueryAttempt[QueryRecordsOutput] {
	client := operation.client
	request, err := client.buildQueryRequest(input)
	if err != nil {
		return sdkgo.NewQueryBranch(QueryRecordsBranchDefect, QueryRecordsOutput{}, salesforceFailurePointer(sdkgo.FailureValidation, queryRecordsOperationID, err.Error()), sdkgo.Receipt{})
	}
	credential, outcome := client.resolveCredential(call, queryRecordsBranches)
	if outcome != nil {
		return queryAttemptFromOutcome[QueryRecordsOutput](client, call, queryRecordsBranches, outcome)
	}
	response, outcome := client.send(call, &credential, queryRecordsBranches, request)
	if outcome != nil {
		return queryAttemptFromOutcome[QueryRecordsOutput](client, call, queryRecordsBranches, outcome)
	}
	receipt := client.receipt(call, "", response.apiUsage)
	output, err := client.decodeQueryResponse(response)
	if err != nil {
		return sdkgo.NewQueryBranch(QueryRecordsBranchInvalidResponse, QueryRecordsOutput{}, salesforceFailurePointer(sdkgo.FailureProtocol, queryRecordsOperationID, "Salesforce returned an invalid query page"), receipt)
	}
	if output.TotalSize == 0 && len(output.Records) == 0 && output.IsDone {
		return sdkgo.NewQueryBranch(QueryRecordsBranchNotFound, output, salesforceFailurePointer(sdkgo.FailureNotFound, queryRecordsOperationID, "no record matched the query"), receipt)
	}
	return sdkgo.NewQueryBranch(QueryRecordsBranchFound, output, nil, receipt)
}

// buildQueryRequest validates input and returns the first-page or cursor request.
func (client *Client) buildQueryRequest(input QueryRecordsInput) (salesforceRequest, error) {
	batchSize := input.BatchSize
	if batchSize == 0 {
		batchSize = client.queryBatchSize
	}
	if batchSize < minimumQueryBatchSize || batchSize > maximumQueryBatchSize {
		return salesforceRequest{}, fmt.Errorf("batchSize must be zero or from %d to %d", minimumQueryBatchSize, maximumQueryBatchSize)
	}
	request := salesforceRequest{
		method: http.MethodGet, responseLimit: client.maxResponseBytes,
		header: http.Header{queryOptionsHeader: {"batchSize=" + strconv.Itoa(batchSize)}},
	}
	hasSOQL := strings.TrimSpace(input.SOQL) != ""
	switch {
	case hasSOQL && input.NextRecordsCursor != "":
		return salesforceRequest{}, errors.New("set soql or nextRecordsCursor, not both")
	case input.NextRecordsCursor != "":
		if len(input.Bindings) > 0 {
			return salesforceRequest{}, errors.New("bindings apply only to soql, not to nextRecordsCursor")
		}
		locator, err := client.queryLocatorFromCursor(input.NextRecordsCursor)
		if err != nil {
			return salesforceRequest{}, err
		}
		request.path = client.dataPath("query", locator)
		return request, nil
	case !hasSOQL:
		return salesforceRequest{}, errors.New("soql or nextRecordsCursor is required")
	}
	statement, err := renderSOQL(input.SOQL, input.Bindings)
	if err != nil {
		return salesforceRequest{}, err
	}
	request.path = client.dataPath("query")
	request.query = url.Values{"q": {statement}}
	if len(request.query.Encode()) > maxEncodedQueryBytes {
		return salesforceRequest{}, errors.New("soql is too long for one Salesforce REST request")
	}
	return request, nil
}

// queryLocatorFromCursor accepts only a nextRecordsUrl for this client's API version.
func (client *Client) queryLocatorFromCursor(cursor string) (string, error) {
	prefix := client.dataPath("query") + "/"
	locator, hasPrefix := strings.CutPrefix(cursor, prefix)
	if !hasPrefix || !queryLocatorPattern.MatchString(locator) {
		return "", errors.New("nextRecordsCursor must be a nextRecordsCursor returned by queryRecords for this apiVersion")
	}
	return locator, nil
}

func (client *Client) decodeQueryResponse(response salesforceResponse) (QueryRecordsOutput, error) {
	if response.status != http.StatusOK {
		return QueryRecordsOutput{}, errors.New("query response status is not 200")
	}
	var decoded queryResponse
	if err := json.Unmarshal(response.body, &decoded); err != nil {
		return QueryRecordsOutput{}, err
	}
	if decoded.TotalSize == nil || decoded.Done == nil || *decoded.TotalSize < 0 {
		return QueryRecordsOutput{}, errors.New("query response lacks totalSize or done")
	}
	output := QueryRecordsOutput{Records: make([]Record, 0, len(decoded.Records)), TotalSize: *decoded.TotalSize, IsDone: *decoded.Done}
	if !output.IsDone {
		if _, err := client.queryLocatorFromCursor(decoded.NextRecordsURL); err != nil {
			return QueryRecordsOutput{}, errors.New("unfinished query response lacks a valid nextRecordsUrl")
		}
		output.NextRecordsCursor = decoded.NextRecordsURL
	}
	for _, content := range decoded.Records {
		record, err := decodeRecord(content)
		if err != nil {
			return QueryRecordsOutput{}, err
		}
		output.Records = append(output.Records, record)
	}
	return output, nil
}
