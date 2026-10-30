// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package notion

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	queryDatabaseOperationID    = "queryDatabase"
	queryDatabaseFailureSubject = "database query"
	defaultQueryPageSize        = 50
	// MaximumReturnedProperties bounds the property names one query asks Notion to return.
	MaximumReturnedProperties = 100
	queryResultLimitReached   = "query_result_limit_reached"
)

// QueryDatabaseInput selects one page of a database's rows. Set exactly one of
// DataSourceID and DatabaseID.
type QueryDatabaseInput struct {
	// DataSourceID is the data source to query. In Notion, open the database's
	// settings menu > Manage data sources and choose Copy data source ID.
	DataSourceID string `json:"dataSourceId,omitempty"`
	// DatabaseID is the database ID from its URL, or the URL itself; the database
	// must hold exactly one data source.
	DatabaseID string `json:"databaseId,omitempty"`
	// Filter optionally limits the rows; nil returns every row.
	Filter *QueryFilter `json:"filter,omitempty"`
	// Sorts optionally orders the rows; Notion guarantees no order without one.
	Sorts []QuerySort `json:"sorts,omitempty"`
	// Properties optionally lists the property names or IDs to return, at most
	// 100; blank returns every property.
	Properties []string `json:"properties,omitempty"`
	// PageSize is the number of rows to request, 1 to 100; zero requests 50.
	PageSize int `json:"pageSize,omitempty"`
	// StartCursor continues a previous query with the same input; blank reads the first page.
	StartCursor string `json:"startCursor,omitempty"`
}

// QueryDatabaseOutput is one page of rows.
type QueryDatabaseOutput struct {
	// DataSourceID is the data source that was queried; store it to skip the database lookup.
	DataSourceID string `json:"dataSourceId"`
	// Pages lists the page's rows in the requested order.
	Pages []Page `json:"pages"`
	// NextCursor continues the query; it is empty on the last page.
	NextCursor string `json:"nextCursor,omitempty"`
	// HasMore reports that another page of rows exists.
	HasMore bool `json:"hasMore"`
	// IsResultLimitReached reports that Notion stopped at its 10,000-row limit for one
	// query; narrow the filter, such as by created_time, to read the rest.
	IsResultLimitReached bool `json:"resultLimitReached"`
}

// QueryDatabaseOperation implements the queryDatabase Query.
type QueryDatabaseOperation struct{ client *Client }

type queryRequestBody struct {
	Filter      map[string]any      `json:"filter,omitempty"`
	Sorts       []map[string]string `json:"sorts,omitempty"`
	StartCursor string              `json:"start_cursor,omitempty"`
	PageSize    int                 `json:"page_size"`
	ResultType  string              `json:"result_type"`
}

type queryResponseBody struct {
	Results       []pageResource `json:"results"`
	NextCursor    *string        `json:"next_cursor"`
	HasMore       bool           `json:"has_more"`
	RequestStatus *struct {
		Type             string `json:"type"`
		IncompleteReason string `json:"incomplete_reason"`
	} `json:"request_status"`
}

type queryDatabaseRequest struct {
	selection  dataSourceSelection
	body       queryRequestBody
	properties []string
}

// Definition returns the immutable connector operation definition.
func (QueryDatabaseOperation) Definition() sdkgo.QueryDefinition { return QueryDatabaseDefinition }

// Invoke resolves the data source when given a database ID, then reads one page of rows.
// Transport failures, 408, 409, 429, 529, and 5xx are retried.
func (operation QueryDatabaseOperation) Invoke(call sdkgo.Call, input QueryDatabaseInput) sdkgo.QueryAttempt[QueryDatabaseOutput] {
	client := operation.client
	request, err := buildQueryDatabaseRequest(input)
	if err != nil {
		return sdkgo.NewQueryBranch(QueryDatabaseBranchDefect, QueryDatabaseOutput{}, validationFailure(queryDatabaseOperationID, err), sdkgo.Receipt{})
	}
	session, cancel, sessionFailure := client.startSession(call, queryDatabaseOperationID, readOperationDeadline)
	if sessionFailure != nil {
		return sdkgo.NewQueryBranch(QueryDatabaseBranchDefect, QueryDatabaseOutput{}, sessionFailure, sdkgo.Receipt{})
	}
	defer cancel()
	dataSourceID, resolution, resolutionResponse := client.resolveDataSourceID(session, queryDatabaseOperationID, request.selection)
	if resolution.outcome != readSucceeded {
		return queryDatabaseAttemptForRead(resolution, QueryDatabaseOutput{}, client.receipt(session, resolutionResponse, request.selection.databaseID))
	}
	output := QueryDatabaseOutput{DataSourceID: dataSourceID}
	query := url.Values{}
	for _, property := range request.properties {
		query.Add("filter_properties[]", property)
	}
	result := client.exchange(session, notionRequest{
		method: http.MethodPost, path: "/data_sources/" + url.PathEscape(dataSourceID) + "/query", query: query, payload: request.body,
	})
	receipt := client.receipt(session, result.response, dataSourceID)
	classification := client.classifyRead(queryDatabaseOperationID, queryDatabaseFailureSubject, result)
	if classification.outcome != readSucceeded {
		return queryDatabaseAttemptForRead(classification, output, receipt)
	}
	var page queryResponseBody
	if err := json.Unmarshal(result.response.body, &page); err != nil || len(page.Results) > request.body.PageSize {
		return sdkgo.NewQueryBranch(QueryDatabaseBranchInvalidResponse, output,
			failurePointer(queryDatabaseOperationID, sdkgo.FailureProtocol, "Notion returned an invalid query page"), receipt)
	}
	nextCursor, err := validateCursor(dereferenceText(page.NextCursor))
	if err != nil {
		return sdkgo.NewQueryBranch(QueryDatabaseBranchInvalidResponse, output,
			failurePointer(queryDatabaseOperationID, sdkgo.FailureProtocol, "Notion returned an invalid query cursor"), receipt)
	}
	output.Pages = make([]Page, 0, len(page.Results))
	for _, resource := range page.Results {
		decoded, err := decodePage(resource)
		if err != nil {
			return sdkgo.NewQueryBranch(QueryDatabaseBranchInvalidResponse, QueryDatabaseOutput{DataSourceID: dataSourceID},
				failurePointer(queryDatabaseOperationID, sdkgo.FailureProtocol, "Notion returned an invalid row in the query page"), receipt)
		}
		output.Pages = append(output.Pages, decoded)
	}
	output.HasMore = page.HasMore
	if page.HasMore {
		output.NextCursor = nextCursor
	}
	output.IsResultLimitReached = page.RequestStatus != nil && page.RequestStatus.IncompleteReason == queryResultLimitReached
	return sdkgo.NewQueryBranch(QueryDatabaseBranchQueried, output, nil, receipt)
}

func queryDatabaseAttemptForRead(classification readClassification, output QueryDatabaseOutput, receipt sdkgo.Receipt) sdkgo.QueryAttempt[QueryDatabaseOutput] {
	switch classification.outcome {
	case readRetry:
		return sdkgo.NewQueryRetry[QueryDatabaseOutput](classification.failure, classification.retryAfter)
	case readNotFound:
		return sdkgo.NewQueryBranch(QueryDatabaseBranchNotFound, output, &classification.failure, receipt)
	case readDefect:
		return sdkgo.NewQueryBranch(QueryDatabaseBranchDefect, output, &classification.failure, receipt)
	case readInvalid:
		return sdkgo.NewQueryBranch(QueryDatabaseBranchInvalidResponse, output, &classification.failure, receipt)
	default:
		return sdkgo.NewQueryBranch(QueryDatabaseBranchProviderRejected, output, &classification.failure, receipt)
	}
}

func buildQueryDatabaseRequest(input QueryDatabaseInput) (queryDatabaseRequest, error) {
	selection, err := selectDataSource(input.DataSourceID, input.DatabaseID)
	if err != nil {
		return queryDatabaseRequest{}, err
	}
	request := queryDatabaseRequest{selection: selection, body: queryRequestBody{PageSize: input.PageSize, ResultType: "page"}}
	if input.Filter != nil {
		if request.body.Filter, err = encodeQueryFilter(*input.Filter); err != nil {
			return queryDatabaseRequest{}, fmt.Errorf("filter: %w", err)
		}
	}
	if request.body.Sorts, err = encodeQuerySorts(input.Sorts); err != nil {
		return queryDatabaseRequest{}, fmt.Errorf("sorts: %w", err)
	}
	switch {
	case request.body.PageSize == 0:
		request.body.PageSize = defaultQueryPageSize
	case request.body.PageSize < 1 || request.body.PageSize > MaximumPageSize:
		return queryDatabaseRequest{}, fmt.Errorf("pageSize must be between 1 and %d", MaximumPageSize)
	}
	if request.body.StartCursor, err = validateCursor(input.StartCursor); err != nil {
		return queryDatabaseRequest{}, err
	}
	if request.properties, err = validateReturnedProperties(input.Properties); err != nil {
		return queryDatabaseRequest{}, err
	}
	return request, nil
}

func validateReturnedProperties(properties []string) ([]string, error) {
	if len(properties) > MaximumReturnedProperties {
		return nil, fmt.Errorf("properties lists at most %d names or IDs", MaximumReturnedProperties)
	}
	for _, property := range properties {
		if strings.TrimSpace(property) == "" || strings.TrimSpace(property) != property || utf8.RuneCountInString(property) > maximumPropertyKeyRunes {
			return nil, errors.New("properties lists property names or IDs without surrounding spaces")
		}
	}
	return properties, nil
}
