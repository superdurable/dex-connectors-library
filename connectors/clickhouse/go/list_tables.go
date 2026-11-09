// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package clickhouse

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListTablesInput selects the non-secret ClickHouse database to inspect.
type ListTablesInput struct {
	// Database is the exact database name shown by SHOW DATABASES.
	Database string `json:"database"`
}

// TablesPage contains at most 100 sorted table names.
type TablesPage struct {
	// Database is the database that was inspected.
	Database string `json:"database"`
	// Names contains the first 100 table names in sorted order.
	Names []string `json:"names"`
	// HasMore reports whether another table was present after the first 100.
	HasMore bool `json:"hasMore"`
}

// ListTablesOperation reads table names through the read-only ClickHouse HTTP interface.
type ListTablesOperation struct{ client *Client }

// ListTables binds the Query to this client.
func (client *Client) ListTables() ListTablesOperation { return ListTablesOperation{client} }

// Definition returns the manifest-generated Query worker.
func (ListTablesOperation) Definition() sdkgo.QueryDefinition { return ListTablesDefinition }

// Invoke reads one bounded table page and classifies retryable and definitive outcomes.
func (operation ListTablesOperation) Invoke(call sdkgo.Call, input ListTablesInput) sdkgo.QueryAttempt[TablesPage] {
	if !validDatabase(input.Database) {
		return tablesBranch(ListTablesBranchDefect, sdkgo.FailureValidation, "database must contain 1–128 printable characters")
	}
	response := operation.client.get(call, input.Database)
	if errors.Is(response.err, errCredential) {
		return tablesBranch(ListTablesBranchDefect, sdkgo.FailureAuthentication, "ClickHouse credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[TablesPage](tablesFailure(sdkgo.FailureTransport, "ClickHouse request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[TablesPage](tablesFailure(sdkgo.FailureRateLimit, "ClickHouse rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[TablesPage](tablesFailure(sdkgo.FailureAvailability, "ClickHouse is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return tablesBranch(ListTablesBranchProviderRejected, sdkgo.FailureProviderRejection, "ClickHouse rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return tablesBranch(ListTablesBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "ClickHouse response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[TablesPage](tablesFailure(sdkgo.FailureTransport, "ClickHouse response was interrupted"), 0)
	}
	page, err := decodeTables(response.body, input.Database)
	if err != nil {
		return tablesBranch(ListTablesBranchInvalidResponse, sdkgo.FailureProtocol, "ClickHouse returned invalid table-list JSON")
	}
	return sdkgo.NewQueryBranch(ListTablesBranchListed, page, nil, sdkgo.Receipt{})
}

func validDatabase(database string) bool {
	if len(database) == 0 || len(database) > 128 || !utf8.ValidString(database) || strings.TrimSpace(database) != database {
		return false
	}
	for _, character := range database {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

func decodeTables(body []byte, database string) (TablesPage, error) {
	var wire struct {
		Data []struct {
			Name string `json:"name"`
		} `json:"data"`
		Rows int `json:"rows"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return TablesPage{}, err
	}
	if wire.Data == nil || len(wire.Data) > 101 || wire.Rows != len(wire.Data) {
		return TablesPage{}, fmt.Errorf("table page is missing or inconsistent")
	}
	page := TablesPage{Database: database, Names: make([]string, 0, min(len(wire.Data), 100)), HasMore: len(wire.Data) > 100}
	for _, table := range wire.Data {
		if table.Name == "" {
			return TablesPage{}, fmt.Errorf("table name is missing")
		}
		if len(page.Names) < 100 {
			page.Names = append(page.Names, table.Name)
		}
	}
	return page, nil
}

func tablesFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "clickhouse", Operation: "listTables", Message: message}
}

func tablesBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[TablesPage] {
	failure := tablesFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, TablesPage{}, &failure, sdkgo.Receipt{})
}
