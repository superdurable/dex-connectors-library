// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package taosdata

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const databaseMetadataSQL = "select name, ntables, status from information_schema.ins_databases;"

// ListDatabasesInput has no caller-supplied SQL or filters.
type ListDatabasesInput struct{}

// DatabaseMetadata contains the three documented metadata columns.
type DatabaseMetadata struct {
	// Name is the database name.
	Name string `json:"name"`
	// TableCount is the number of tables in the database.
	TableCount int64 `json:"tableCount"`
	// Status is the provider's database status.
	Status string `json:"status"`
}

// ListDatabasesOutput contains accessible database metadata.
type ListDatabasesOutput struct {
	// Databases is ordered as returned by TDengine Cloud.
	Databases []DatabaseMetadata `json:"databases"`
}

// ListDatabasesOperation reads database metadata without accepting arbitrary SQL.
type ListDatabasesOperation struct{ client *Client }

// Definition returns the generated list-databases contract.
func (ListDatabasesOperation) Definition() sdkgo.QueryDefinition { return ListDatabasesDefinition }

// Invoke sends a fixed metadata query and returns bounded fields.
func (operation ListDatabasesOperation) Invoke(call sdkgo.Call, _ ListDatabasesInput) sdkgo.QueryAttempt[ListDatabasesOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidCloudURL(credentials.CloudURL.Reveal()) || !isValidCloudToken(credentials.CloudToken.Reveal()) {
		return databaseBranch(ListDatabasesBranchDefect, sdkgo.FailureAuthentication, "Cloud URL or token is unavailable or invalid")
	}
	endpoint := credentials.CloudURL.Reveal() + "/rest/sql?token=" + url.QueryEscape(credentials.CloudToken.Reveal())
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, endpoint, strings.NewReader(databaseMetadataSQL))
	if err != nil {
		return databaseBranch(ListDatabasesBranchDefect, sdkgo.FailureValidation, "metadata request is invalid")
	}
	request.Header.Set("Content-Type", "text/plain")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return databaseRetry(sdkgo.FailureAvailability, "TDengine Cloud request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
		return databaseRetry(sdkgo.FailureAvailability, "TDengine Cloud is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return databaseBranch(ListDatabasesBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "TDengine Cloud response exceeded the configured limit")
	}
	if err != nil {
		return databaseRetry(sdkgo.FailureAvailability, "TDengine Cloud response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		return classifyDatabaseStatus(response.StatusCode)
	}
	var wire struct {
		Code       int                 `json:"code"`
		Desc       string              `json:"desc"`
		ColumnMeta [][]json.RawMessage `json:"column_meta"`
		Data       [][]json.RawMessage `json:"data"`
		Rows       int                 `json:"rows"`
	}
	if json.Unmarshal(body, &wire) != nil {
		return databaseBranch(ListDatabasesBranchInvalidResponse, sdkgo.FailureProtocol, "TDengine Cloud returned invalid JSON")
	}
	if wire.Code != 0 {
		return databaseBranch(ListDatabasesBranchProviderRejected, sdkgo.FailureProviderRejection, "TDengine Cloud rejected the metadata query with code "+strconv.Itoa(wire.Code))
	}
	if wire.Rows < 0 || wire.Rows != len(wire.Data) || len(wire.ColumnMeta) != 3 || !isExpectedColumns(wire.ColumnMeta) {
		return databaseBranch(ListDatabasesBranchInvalidResponse, sdkgo.FailureProtocol, "TDengine Cloud returned invalid metadata columns or rows")
	}
	output := ListDatabasesOutput{Databases: make([]DatabaseMetadata, 0, len(wire.Data))}
	for _, row := range wire.Data {
		if len(row) != 3 {
			return databaseBranch(ListDatabasesBranchInvalidResponse, sdkgo.FailureProtocol, "TDengine Cloud returned invalid metadata row")
		}
		var item DatabaseMetadata
		if json.Unmarshal(row[0], &item.Name) != nil || json.Unmarshal(row[1], &item.TableCount) != nil || json.Unmarshal(row[2], &item.Status) != nil || item.Name == "" || item.Status == "" || item.TableCount < 0 || len(item.Name) > 256 || len(item.Status) > 64 || strings.Contains(item.Name, credentials.CloudToken.Reveal()) || strings.Contains(item.Status, credentials.CloudToken.Reveal()) {
			return databaseBranch(ListDatabasesBranchInvalidResponse, sdkgo.FailureProtocol, "TDengine Cloud returned invalid database metadata")
		}
		output.Databases = append(output.Databases, item)
	}
	return sdkgo.NewQueryBranch(ListDatabasesBranchListed, output, nil, sdkgo.Receipt{})
}

func isExpectedColumns(columns [][]json.RawMessage) bool {
	expected := []string{"name", "ntables", "status"}
	for index, column := range columns {
		if len(column) < 1 {
			return false
		}
		var name string
		if json.Unmarshal(column[0], &name) != nil || name != expected[index] {
			return false
		}
	}
	return true
}
func classifyDatabaseStatus(status int) sdkgo.QueryAttempt[ListDatabasesOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return databaseBranch(ListDatabasesBranchProviderRejected, kind, "TDengine Cloud rejected the request with HTTP "+strconv.Itoa(status))
}
func databaseBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListDatabasesOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "taosdata", Operation: "listDatabases", Message: message}
	return sdkgo.NewQueryBranch(branch, ListDatabasesOutput{}, &failure, sdkgo.Receipt{})
}
func databaseRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListDatabasesOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "taosdata", Operation: "listDatabases", Message: message}
	return sdkgo.NewQueryRetry[ListDatabasesOutput](failure, delay)
}
