// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bigquery

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// BigQueryCell holds one provider-encoded table value.
type BigQueryCell struct {
	// Value is the JSON cell value, which can be scalar or nested.
	Value json.RawMessage `json:"value"`
}

// BigQueryRow holds cells in schema field order.
type BigQueryRow struct {
	// Cells are provider values in schema field order.
	Cells []BigQueryCell `json:"cells"`
}

// ListTableRowsInput selects a bounded page of table data.
type ListTableRowsInput struct {
	// ProjectID is the Google Cloud project ID.
	ProjectID string `json:"projectId"`
	// DatasetID is the dataset ID.
	DatasetID string `json:"datasetId"`
	// TableID is the table ID.
	TableID string `json:"tableId"`
	// MaxResults is 1 to 100; zero uses 100.
	MaxResults int `json:"maxResults,omitempty"`
	// PageToken is the prior page's cursor; blank starts at the first page.
	PageToken string `json:"pageToken,omitempty"`
}

// TableRowsPage contains one bounded page of table rows.
type TableRowsPage struct {
	// Rows contain provider-encoded cell values.
	Rows []BigQueryRow `json:"rows"`
	// NextPageToken is blank on the final page.
	NextPageToken string `json:"nextPageToken,omitempty"`
}

// RunReadQueryInput submits one SELECT statement with a billing cap.
type RunReadQueryInput struct {
	// ProjectID is the billing and job project ID.
	ProjectID string `json:"projectId"`
	// Query is one Google Standard SQL SELECT statement without comments or semicolons.
	Query string `json:"query"`
	// MaximumBytesBilled is a positive ceiling for scanned bytes.
	MaximumBytesBilled int64 `json:"maximumBytesBilled"`
	// MaxResults is 1 to 100; zero uses 100.
	MaxResults int `json:"maxResults,omitempty"`
	// Location is the dataset location such as US or us-central1; blank lets BigQuery infer it.
	Location string `json:"location,omitempty"`
}

// GetQueryResultsInput selects a page from a previously created query job.
type GetQueryResultsInput struct {
	// ProjectID is the project that owns the job.
	ProjectID string `json:"projectId"`
	// JobID is the ID returned by runReadQuery.
	JobID string `json:"jobId"`
	// Location is the job location returned by runReadQuery; blank uses BigQuery's default.
	Location string `json:"location,omitempty"`
	// MaxResults is 1 to 100; zero uses 100.
	MaxResults int `json:"maxResults,omitempty"`
	// PageToken is the prior result page cursor; blank starts at the first page.
	PageToken string `json:"pageToken,omitempty"`
}

// QueryResultsPage contains completed rows or the identity of a pending job.
type QueryResultsPage struct {
	// JobID identifies the provider query job.
	JobID string `json:"jobId"`
	// Location is the job's BigQuery location when returned.
	Location string `json:"location,omitempty"`
	// Rows contain provider-encoded cell values.
	Rows []BigQueryRow `json:"rows"`
	// NextPageToken requests the next result page.
	NextPageToken string `json:"nextPageToken,omitempty"`
	// TotalRows is BigQuery's decimal row count when returned.
	TotalRows string `json:"totalRows,omitempty"`
}

// ListTableRowsOperation reads table data pages.
type ListTableRowsOperation struct{ client *Client }

// RunReadQueryOperation executes one bounded SELECT query.
type RunReadQueryOperation struct{ client *Client }

// GetQueryResultsOperation reads completed query job pages.
type GetQueryResultsOperation struct{ client *Client }

// ListTableRows binds the row listing Query to this client.
func (client *Client) ListTableRows() ListTableRowsOperation { return ListTableRowsOperation{client} }

// RunReadQuery binds the SELECT Query to this client.
func (client *Client) RunReadQuery() RunReadQueryOperation { return RunReadQueryOperation{client} }

// GetQueryResults binds the job results Query to this client.
func (client *Client) GetQueryResults() GetQueryResultsOperation {
	return GetQueryResultsOperation{client}
}

// Definition returns the generated table row contract.
func (ListTableRowsOperation) Definition() sdkgo.QueryDefinition { return ListTableRowsDefinition }

// Definition returns the generated SELECT contract.
func (RunReadQueryOperation) Definition() sdkgo.QueryDefinition { return RunReadQueryDefinition }

// Definition returns the generated job results contract.
func (GetQueryResultsOperation) Definition() sdkgo.QueryDefinition { return GetQueryResultsDefinition }

var selectStatement = regexp.MustCompile(`(?is)^SELECT\s+`)
var locationPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// Invoke reads up to 100 rows from one table.
func (operation ListTableRowsOperation) Invoke(call sdkgo.Call, input ListTableRowsInput) sdkgo.QueryAttempt[TableRowsPage] {
	if !validTableInput(GetTableInput{ProjectID: input.ProjectID, DatasetID: input.DatasetID, TableID: input.TableID}) || validateRowPage(input.MaxResults, input.PageToken) != nil {
		return bigqueryReadBranch(ListTableRowsBranchDefect, TableRowsPage{}, "listTableRows", sdkgo.FailureValidation, "table or page input is invalid")
	}
	maxResults := input.MaxResults
	if maxResults == 0 {
		maxResults = 100
	}
	query := url.Values{"maxResults": {strconv.Itoa(maxResults)}}
	if input.PageToken != "" {
		query.Set("pageToken", input.PageToken)
	}
	response := operation.client.get(call, tablePath(GetTableInput{ProjectID: input.ProjectID, DatasetID: input.DatasetID, TableID: input.TableID})+"/data?"+query.Encode())
	if decision, failed := classifyBigQueryRead[TableRowsPage](response, "listTableRows", bigqueryReadBranches{notFound: ListTableRowsBranchNotFound, rejected: ListTableRowsBranchProviderRejected, invalid: ListTableRowsBranchInvalidResponse, defect: ListTableRowsBranchDefect}); failed {
		return decision
	}
	rows, nextPageToken, err := decodeBigQueryRows(response.body, maxResults)
	if err != nil {
		return bigqueryReadBranch(ListTableRowsBranchInvalidResponse, TableRowsPage{}, "listTableRows", sdkgo.FailureProtocol, "BigQuery returned invalid row JSON")
	}
	return sdkgo.NewQueryBranch(ListTableRowsBranchListed, TableRowsPage{Rows: rows, NextPageToken: nextPageToken}, nil, sdkgo.Receipt{})
}

// Invoke submits a single SELECT statement with a positive billing ceiling.
func (operation RunReadQueryOperation) Invoke(call sdkgo.Call, input RunReadQueryInput) sdkgo.QueryAttempt[QueryResultsPage] {
	queryText := strings.TrimSpace(input.Query)
	if !validProjectID(input.ProjectID) || !selectStatement.MatchString(queryText) || strings.ContainsAny(queryText, ";\x00") || strings.Contains(queryText, "--") || strings.Contains(queryText, "/*") || strings.Contains(queryText, "#") || len(queryText) > 100000 || input.MaximumBytesBilled <= 0 || validateRowPage(input.MaxResults, "") != nil || !validLocation(input.Location) {
		return bigqueryReadBranch(RunReadQueryBranchDefect, QueryResultsPage{}, "runReadQuery", sdkgo.FailureValidation, "only one bounded SELECT statement is accepted")
	}
	maxResults := input.MaxResults
	if maxResults == 0 {
		maxResults = 100
	}
	requestBody := map[string]any{"query": queryText, "useLegacySql": false, "maxResults": maxResults, "maximumBytesBilled": strconv.FormatInt(input.MaximumBytesBilled, 10), "timeoutMs": 10000}
	if input.Location != "" {
		requestBody["location"] = input.Location
	}
	body, _ := json.Marshal(requestBody)
	response := operation.client.post(call, "/bigquery/v2/projects/"+url.PathEscape(input.ProjectID)+"/queries", body)
	if decision, failed := classifyBigQueryRead[QueryResultsPage](response, "runReadQuery", bigqueryReadBranches{rejected: RunReadQueryBranchProviderRejected, invalid: RunReadQueryBranchInvalidResponse, defect: RunReadQueryBranchDefect}); failed {
		return decision
	}
	page, complete, err := decodeQueryResults(response.body, "", maxResults)
	if err != nil {
		return bigqueryReadBranch(RunReadQueryBranchInvalidResponse, QueryResultsPage{}, "runReadQuery", sdkgo.FailureProtocol, "BigQuery returned invalid query JSON")
	}
	if !complete {
		return sdkgo.NewQueryBranch(RunReadQueryBranchPending, page, nil, sdkgo.Receipt{})
	}
	return sdkgo.NewQueryBranch(RunReadQueryBranchCompleted, page, nil, sdkgo.Receipt{})
}

// Invoke reads one page from a submitted BigQuery query job.
func (operation GetQueryResultsOperation) Invoke(call sdkgo.Call, input GetQueryResultsInput) sdkgo.QueryAttempt[QueryResultsPage] {
	if !validProjectID(input.ProjectID) || !validJobID(input.JobID) || !validLocation(input.Location) || validateRowPage(input.MaxResults, input.PageToken) != nil {
		return bigqueryReadBranch(GetQueryResultsBranchDefect, QueryResultsPage{}, "getQueryResults", sdkgo.FailureValidation, "job or page input is invalid")
	}
	maxResults := input.MaxResults
	if maxResults == 0 {
		maxResults = 100
	}
	query := url.Values{"maxResults": {strconv.Itoa(maxResults)}}
	if input.Location != "" {
		query.Set("location", input.Location)
	}
	if input.PageToken != "" {
		query.Set("pageToken", input.PageToken)
	}
	path := "/bigquery/v2/projects/" + url.PathEscape(input.ProjectID) + "/queries/" + url.PathEscape(input.JobID) + "?" + query.Encode()
	response := operation.client.get(call, path)
	if decision, failed := classifyBigQueryRead[QueryResultsPage](response, "getQueryResults", bigqueryReadBranches{notFound: GetQueryResultsBranchNotFound, rejected: GetQueryResultsBranchProviderRejected, invalid: GetQueryResultsBranchInvalidResponse, defect: GetQueryResultsBranchDefect}); failed {
		return decision
	}
	page, complete, err := decodeQueryResults(response.body, input.JobID, maxResults)
	if err != nil {
		return bigqueryReadBranch(GetQueryResultsBranchInvalidResponse, QueryResultsPage{}, "getQueryResults", sdkgo.FailureProtocol, "BigQuery returned invalid result JSON")
	}
	if !complete {
		return sdkgo.NewQueryBranch(GetQueryResultsBranchPending, page, nil, sdkgo.Receipt{})
	}
	return sdkgo.NewQueryBranch(GetQueryResultsBranchCompleted, page, nil, sdkgo.Receipt{})
}

func validLocation(location string) bool {
	return location == "" || locationPattern.MatchString(location)
}

func decodeBigQueryRows(body []byte, maxResults int) ([]BigQueryRow, string, error) {
	var wire struct {
		Rows []struct {
			Fields []struct {
				Value json.RawMessage `json:"v"`
			} `json:"f"`
		} `json:"rows"`
		PageToken string `json:"pageToken"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, "", err
	}
	if len(wire.Rows) > maxResults || !validPageToken(wire.PageToken) {
		return nil, "", fmt.Errorf("row page exceeds bounds")
	}
	rows := make([]BigQueryRow, 0, len(wire.Rows))
	for _, row := range wire.Rows {
		if len(row.Fields) > 1000 {
			return nil, "", fmt.Errorf("row has too many cells")
		}
		projected := BigQueryRow{Cells: make([]BigQueryCell, 0, len(row.Fields))}
		for _, field := range row.Fields {
			if !json.Valid(field.Value) {
				return nil, "", fmt.Errorf("invalid cell JSON")
			}
			projected.Cells = append(projected.Cells, BigQueryCell{Value: field.Value})
		}
		rows = append(rows, projected)
	}
	return rows, wire.PageToken, nil
}

func decodeQueryResults(body []byte, requestedJobID string, maxResults int) (QueryResultsPage, bool, error) {
	var wire struct {
		JobReference struct {
			JobID    string `json:"jobId"`
			Location string `json:"location"`
		} `json:"jobReference"`
		JobComplete *bool `json:"jobComplete"`
		Rows        []struct {
			Fields []struct {
				Value json.RawMessage `json:"v"`
			} `json:"f"`
		} `json:"rows"`
		PageToken string `json:"pageToken"`
		TotalRows string `json:"totalRows"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return QueryResultsPage{}, false, err
	}
	jobID := wire.JobReference.JobID
	if jobID == "" {
		jobID = requestedJobID
	}
	if wire.JobComplete == nil || !validJobID(jobID) || !validPageToken(wire.PageToken) || len(wire.Rows) > maxResults {
		return QueryResultsPage{}, false, fmt.Errorf("query response is invalid")
	}
	rows := make([]BigQueryRow, 0, len(wire.Rows))
	for _, row := range wire.Rows {
		if len(row.Fields) > 1000 {
			return QueryResultsPage{}, false, fmt.Errorf("result row has too many cells")
		}
		projected := BigQueryRow{Cells: make([]BigQueryCell, 0, len(row.Fields))}
		for _, field := range row.Fields {
			if !json.Valid(field.Value) {
				return QueryResultsPage{}, false, fmt.Errorf("invalid result cell JSON")
			}
			projected.Cells = append(projected.Cells, BigQueryCell{Value: field.Value})
		}
		rows = append(rows, projected)
	}
	return QueryResultsPage{JobID: jobID, Location: wire.JobReference.Location, Rows: rows, NextPageToken: wire.PageToken, TotalRows: wire.TotalRows}, *wire.JobComplete, nil
}
