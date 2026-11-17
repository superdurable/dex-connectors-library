// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bigquery

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetDatasetInput selects one dataset in a Google Cloud project.
type GetDatasetInput struct {
	// ProjectID is the Google Cloud project ID shown in BigQuery Studio.
	ProjectID string `json:"projectId"`
	// DatasetID is the dataset ID within the project.
	DatasetID string `json:"datasetId"`
}

// ListTablesInput selects a bounded page of tables in a dataset.
type ListTablesInput struct {
	// ProjectID is the Google Cloud project ID.
	ProjectID string `json:"projectId"`
	// DatasetID is the dataset ID.
	DatasetID string `json:"datasetId"`
	// MaxResults is 1 to 100; zero uses 100.
	MaxResults int `json:"maxResults,omitempty"`
	// PageToken is the prior page's cursor; blank starts at the first page.
	PageToken string `json:"pageToken,omitempty"`
}

// GetTableInput selects one table in a dataset.
type GetTableInput struct {
	// ProjectID is the Google Cloud project ID.
	ProjectID string `json:"projectId"`
	// DatasetID is the dataset ID.
	DatasetID string `json:"datasetId"`
	// TableID is the table ID.
	TableID string `json:"tableId"`
}

// TableField is a top-level BigQuery table schema field.
type TableField struct {
	// Name is the field name.
	Name string `json:"name"`
	// Type is the BigQuery data type.
	Type string `json:"type"`
	// Mode is NULLABLE, REQUIRED, or REPEATED when present.
	Mode string `json:"mode,omitempty"`
}

// Table describes a BigQuery table or view without row data.
type Table struct {
	// ID is the table ID within its dataset.
	ID string `json:"id"`
	// DatasetID is the containing dataset.
	DatasetID string `json:"datasetId"`
	// ProjectID is the containing project.
	ProjectID string `json:"projectId"`
	// Type is TABLE, VIEW, or another BigQuery table type.
	Type string `json:"type"`
	// FriendlyName is the optional display name.
	FriendlyName string `json:"friendlyName,omitempty"`
	// Fields are top-level schema fields when included by the provider.
	Fields []TableField `json:"fields,omitempty"`
}

// TablesPage contains one bounded page of table identities.
type TablesPage struct {
	// Tables are visible in the selected dataset.
	Tables []Table `json:"tables"`
	// NextPageToken is blank on the final page.
	NextPageToken string `json:"nextPageToken,omitempty"`
}

// GetDatasetOperation reads one dataset.
type GetDatasetOperation struct{ client *Client }

// ListTablesOperation lists a page of tables.
type ListTablesOperation struct{ client *Client }

// GetTableOperation reads one table's metadata.
type GetTableOperation struct{ client *Client }

// GetDataset binds the dataset lookup Query to this client.
func (client *Client) GetDataset() GetDatasetOperation { return GetDatasetOperation{client} }

// ListTables binds the table listing Query to this client.
func (client *Client) ListTables() ListTablesOperation { return ListTablesOperation{client} }

// GetTable binds the table lookup Query to this client.
func (client *Client) GetTable() GetTableOperation { return GetTableOperation{client} }

// Definition returns the generated dataset lookup contract.
func (GetDatasetOperation) Definition() sdkgo.QueryDefinition { return GetDatasetDefinition }

// Definition returns the generated table listing contract.
func (ListTablesOperation) Definition() sdkgo.QueryDefinition { return ListTablesDefinition }

// Definition returns the generated table lookup contract.
func (GetTableOperation) Definition() sdkgo.QueryDefinition { return GetTableDefinition }

var tableIDPattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

// Invoke reads one dataset identity and display name.
func (operation GetDatasetOperation) Invoke(call sdkgo.Call, input GetDatasetInput) sdkgo.QueryAttempt[Dataset] {
	if !validProjectID(input.ProjectID) || !datasetIDPattern.MatchString(input.DatasetID) {
		return bigqueryReadBranch(GetDatasetBranchDefect, Dataset{}, "getDataset", sdkgo.FailureValidation, "dataset identity is invalid")
	}
	response := operation.client.get(call, datasetPath(input.ProjectID, input.DatasetID))
	if decision, failed := classifyBigQueryRead[Dataset](response, "getDataset", bigqueryReadBranches{notFound: GetDatasetBranchNotFound, rejected: GetDatasetBranchProviderRejected, invalid: GetDatasetBranchInvalidResponse, defect: GetDatasetBranchDefect}); failed {
		return decision
	}
	var wire struct {
		DatasetReference struct {
			DatasetID string `json:"datasetId"`
			ProjectID string `json:"projectId"`
		} `json:"datasetReference"`
		FriendlyName string `json:"friendlyName"`
	}
	if err := json.Unmarshal(response.body, &wire); err != nil || wire.DatasetReference.DatasetID != input.DatasetID || wire.DatasetReference.ProjectID != input.ProjectID {
		return bigqueryReadBranch(GetDatasetBranchInvalidResponse, Dataset{}, "getDataset", sdkgo.FailureProtocol, "BigQuery returned invalid dataset JSON")
	}
	return sdkgo.NewQueryBranch(GetDatasetBranchFound, Dataset{ID: input.DatasetID, ProjectID: input.ProjectID, FriendlyName: wire.FriendlyName}, nil, sdkgo.Receipt{})
}

// Invoke lists a bounded page of table identities.
func (operation ListTablesOperation) Invoke(call sdkgo.Call, input ListTablesInput) sdkgo.QueryAttempt[TablesPage] {
	if !validProjectID(input.ProjectID) || !datasetIDPattern.MatchString(input.DatasetID) || input.MaxResults < 0 || input.MaxResults > 100 || !validPageToken(input.PageToken) {
		return bigqueryReadBranch(ListTablesBranchDefect, TablesPage{}, "listTables", sdkgo.FailureValidation, "dataset or page input is invalid")
	}
	maxResults := input.MaxResults
	if maxResults == 0 {
		maxResults = 100
	}
	query := url.Values{"maxResults": {strconv.Itoa(maxResults)}}
	if input.PageToken != "" {
		query.Set("pageToken", input.PageToken)
	}
	response := operation.client.get(call, datasetPath(input.ProjectID, input.DatasetID)+"/tables?"+query.Encode())
	if decision, failed := classifyBigQueryRead[TablesPage](response, "listTables", bigqueryReadBranches{notFound: ListTablesBranchNotFound, rejected: ListTablesBranchProviderRejected, invalid: ListTablesBranchInvalidResponse, defect: ListTablesBranchDefect}); failed {
		return decision
	}
	var wire struct {
		Tables []struct {
			TableReference struct {
				TableID   string `json:"tableId"`
				DatasetID string `json:"datasetId"`
				ProjectID string `json:"projectId"`
			} `json:"tableReference"`
			Type         string `json:"type"`
			FriendlyName string `json:"friendlyName"`
		} `json:"tables"`
		NextPageToken string `json:"nextPageToken"`
	}
	if err := json.Unmarshal(response.body, &wire); err != nil || len(wire.Tables) > maxResults || !validPageToken(wire.NextPageToken) {
		return bigqueryReadBranch(ListTablesBranchInvalidResponse, TablesPage{}, "listTables", sdkgo.FailureProtocol, "BigQuery returned invalid table page JSON")
	}
	page := TablesPage{Tables: make([]Table, 0, len(wire.Tables)), NextPageToken: wire.NextPageToken}
	for _, item := range wire.Tables {
		if !tableIDPattern.MatchString(item.TableReference.TableID) || item.TableReference.DatasetID != input.DatasetID || item.TableReference.ProjectID != input.ProjectID {
			return bigqueryReadBranch(ListTablesBranchInvalidResponse, TablesPage{}, "listTables", sdkgo.FailureProtocol, "BigQuery returned invalid table reference")
		}
		page.Tables = append(page.Tables, Table{ID: item.TableReference.TableID, DatasetID: input.DatasetID, ProjectID: input.ProjectID, Type: item.Type, FriendlyName: item.FriendlyName})
	}
	return sdkgo.NewQueryBranch(ListTablesBranchListed, page, nil, sdkgo.Receipt{})
}

// Invoke reads one table's identity and top-level schema.
func (operation GetTableOperation) Invoke(call sdkgo.Call, input GetTableInput) sdkgo.QueryAttempt[Table] {
	if !validTableInput(input) {
		return bigqueryReadBranch(GetTableBranchDefect, Table{}, "getTable", sdkgo.FailureValidation, "table identity is invalid")
	}
	response := operation.client.get(call, tablePath(input))
	if decision, failed := classifyBigQueryRead[Table](response, "getTable", bigqueryReadBranches{notFound: GetTableBranchNotFound, rejected: GetTableBranchProviderRejected, invalid: GetTableBranchInvalidResponse, defect: GetTableBranchDefect}); failed {
		return decision
	}
	var wire struct {
		TableReference struct {
			TableID   string `json:"tableId"`
			DatasetID string `json:"datasetId"`
			ProjectID string `json:"projectId"`
		} `json:"tableReference"`
		Type         string `json:"type"`
		FriendlyName string `json:"friendlyName"`
		Schema       struct {
			Fields []TableField `json:"fields"`
		} `json:"schema"`
	}
	if err := json.Unmarshal(response.body, &wire); err != nil || wire.TableReference.TableID != input.TableID || wire.TableReference.DatasetID != input.DatasetID || wire.TableReference.ProjectID != input.ProjectID || wire.Type == "" || len(wire.Schema.Fields) > 10000 {
		return bigqueryReadBranch(GetTableBranchInvalidResponse, Table{}, "getTable", sdkgo.FailureProtocol, "BigQuery returned invalid table JSON")
	}
	for _, field := range wire.Schema.Fields {
		if field.Name == "" || field.Type == "" {
			return bigqueryReadBranch(GetTableBranchInvalidResponse, Table{}, "getTable", sdkgo.FailureProtocol, "BigQuery returned invalid schema field")
		}
	}
	return sdkgo.NewQueryBranch(GetTableBranchFound, Table{ID: input.TableID, DatasetID: input.DatasetID, ProjectID: input.ProjectID, Type: wire.Type, FriendlyName: wire.FriendlyName, Fields: wire.Schema.Fields}, nil, sdkgo.Receipt{})
}

func validTableInput(input GetTableInput) bool {
	return validProjectID(input.ProjectID) && datasetIDPattern.MatchString(input.DatasetID) && tableIDPattern.MatchString(input.TableID)
}

func datasetPath(projectID, datasetID string) string {
	return "/bigquery/v2/projects/" + url.PathEscape(projectID) + "/datasets/" + url.PathEscape(datasetID)
}

func tablePath(input GetTableInput) string {
	return datasetPath(input.ProjectID, input.DatasetID) + "/tables/" + url.PathEscape(input.TableID)
}

type bigqueryReadBranches struct{ notFound, rejected, invalid, defect sdkgo.BranchID }

func classifyBigQueryRead[T any](response providerResponse, operation string, branches bigqueryReadBranches) (sdkgo.QueryAttempt[T], bool) {
	var zero T
	if errors.Is(response.err, errCredential) {
		return bigqueryReadBranch(branches.defect, zero, operation, sdkgo.FailureAuthentication, "connection credentials are unavailable"), true
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return bigqueryReadBranch(branches.invalid, zero, operation, sdkgo.FailureResponseTooLarge, "BigQuery response exceeded the limit"), true
	}
	if response.err != nil {
		return bigqueryReadRetry[T](operation, sdkgo.FailureTransport, "BigQuery request failed", 0), true
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable || response.status == http.StatusRequestTimeout || response.status >= 500 {
		kind := sdkgo.FailureAvailability
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return bigqueryReadRetry[T](operation, kind, "BigQuery asked to retry", response.retryAfter), true
	}
	if response.status == http.StatusNotFound && branches.notFound != "" {
		return bigqueryReadBranch(branches.notFound, zero, operation, sdkgo.FailureNotFound, "BigQuery resource was not found"), true
	}
	if response.status != http.StatusOK {
		return bigqueryReadBranch(branches.rejected, zero, operation, sdkgo.FailureProviderRejection, "BigQuery rejected HTTP "+strconv.Itoa(response.status)), true
	}
	return sdkgo.QueryAttempt[T]{}, false
}

func bigqueryReadBranch[T any](branch sdkgo.BranchID, value T, operation string, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	failure := sdkgo.Failure{Kind: kind, Provider: "google-bigquery", Operation: operation, Message: message}
	return sdkgo.NewQueryBranch(branch, value, &failure, sdkgo.Receipt{})
}

func bigqueryReadRetry[T any](operation string, kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[T] {
	return sdkgo.NewQueryRetry[T](sdkgo.Failure{Kind: kind, Provider: "google-bigquery", Operation: operation, Message: message}, delay)
}

func validateRowPage(maxResults int, pageToken string) error {
	if maxResults < 0 || maxResults > 100 || !validPageToken(pageToken) {
		return fmt.Errorf("row page input is invalid")
	}
	return nil
}

func validJobID(jobID string) bool {
	return len(jobID) > 0 && len(jobID) <= 1024 && !strings.ContainsAny(jobID, "/\r\n")
}
