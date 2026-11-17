// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package clickhouse

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const listDatabasesSQL = "SELECT name FROM system.databases ORDER BY name LIMIT 101 FORMAT JSON"
const listColumnsSQL = "SELECT name, type FROM system.columns WHERE database = {database:String} AND table = {table:String} ORDER BY position LIMIT 101 FORMAT JSON"

// ListDatabasesInput requests the first bounded metadata page.
type ListDatabasesInput struct{}

// DatabasesPage contains at most 100 sorted database names.
type DatabasesPage struct {
	// Names contains the first 100 database names.
	Names []string `json:"names"`
	// HasMore indicates that another database exists after this page.
	HasMore bool `json:"hasMore"`
}

// ListColumnsInput identifies one ClickHouse table without exposing SQL.
type ListColumnsInput struct {
	// Database is the exact database name shown by SHOW DATABASES.
	Database string `json:"database"`
	// Table is the exact table name shown by SHOW TABLES.
	Table string `json:"table"`
}

// Column describes one table column.
type Column struct {
	// Name is the provider column name.
	Name string `json:"name"`
	// Type is the provider column type expression.
	Type string `json:"type"`
}

// ColumnsPage contains at most 100 columns in table position order.
type ColumnsPage struct {
	// Database is the inspected database.
	Database string `json:"database"`
	// Table is the inspected table.
	Table string `json:"table"`
	// Columns are the first 100 column definitions.
	Columns []Column `json:"columns"`
	// HasMore indicates that another column exists after this page.
	HasMore bool `json:"hasMore"`
}

// ListDatabasesOperation reads the bounded database catalog.
type ListDatabasesOperation struct{ client *Client }

// ListDatabases binds the metadata Query to this client.
func (client *Client) ListDatabases() ListDatabasesOperation { return ListDatabasesOperation{client} }

// Definition returns the manifest-generated Query worker.
func (ListDatabasesOperation) Definition() sdkgo.QueryDefinition { return ListDatabasesDefinition }

// Invoke reads the database catalog and classifies its outcome.
func (operation ListDatabasesOperation) Invoke(call sdkgo.Call, _ ListDatabasesInput) sdkgo.QueryAttempt[DatabasesPage] {
	response := operation.client.getMetadata(call, listDatabasesSQL, url.Values{})
	return metadataQueryAttempt(response, "listDatabases", ListDatabasesBranchListed, ListDatabasesBranchProviderRejected, ListDatabasesBranchInvalidResponse, ListDatabasesBranchDefect, func(body []byte) (DatabasesPage, error) {
		rows, err := decodeMetadataRows[struct {
			Name string `json:"name"`
		}](body)
		if err != nil {
			return DatabasesPage{}, err
		}
		page := DatabasesPage{Names: make([]string, 0, min(len(rows), 100)), HasMore: len(rows) > 100}
		for index, row := range rows {
			if row.Name == "" {
				return DatabasesPage{}, errors.New("database name is missing")
			}
			if index < 100 {
				page.Names = append(page.Names, row.Name)
			}
		}
		return page, nil
	})
}

// ListColumnsOperation reads the bounded schema of one table.
type ListColumnsOperation struct{ client *Client }

// ListColumns binds the metadata Query to this client.
func (client *Client) ListColumns() ListColumnsOperation { return ListColumnsOperation{client} }

// Definition returns the manifest-generated Query worker.
func (ListColumnsOperation) Definition() sdkgo.QueryDefinition { return ListColumnsDefinition }

// Invoke reads table columns and classifies its outcome.
func (operation ListColumnsOperation) Invoke(call sdkgo.Call, input ListColumnsInput) sdkgo.QueryAttempt[ColumnsPage] {
	if !validDatabase(input.Database) || !validDatabase(input.Table) {
		failure := metadataFailure("listColumns", sdkgo.FailureValidation, "database and table must contain 1–128 printable characters")
		return sdkgo.NewQueryBranch(ListColumnsBranchDefect, ColumnsPage{}, &failure, sdkgo.Receipt{})
	}
	response := operation.client.getMetadata(call, listColumnsSQL, url.Values{"param_database": {input.Database}, "param_table": {input.Table}})
	return metadataQueryAttempt(response, "listColumns", ListColumnsBranchListed, ListColumnsBranchProviderRejected, ListColumnsBranchInvalidResponse, ListColumnsBranchDefect, func(body []byte) (ColumnsPage, error) {
		rows, err := decodeMetadataRows[Column](body)
		if err != nil {
			return ColumnsPage{}, err
		}
		page := ColumnsPage{Database: input.Database, Table: input.Table, Columns: make([]Column, 0, min(len(rows), 100)), HasMore: len(rows) > 100}
		for index, row := range rows {
			if row.Name == "" || row.Type == "" {
				return ColumnsPage{}, errors.New("column name or type is missing")
			}
			if index < 100 {
				page.Columns = append(page.Columns, row)
			}
		}
		return page, nil
	})
}

func decodeMetadataRows[T any](body []byte) ([]T, error) {
	var wire struct {
		Data []T `json:"data"`
		Rows int `json:"rows"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, err
	}
	if wire.Data == nil || len(wire.Data) > 101 || wire.Rows != len(wire.Data) {
		return nil, errors.New("metadata page is missing or inconsistent")
	}
	return wire.Data, nil
}

func metadataQueryAttempt[T any](response providerResponse, operation string, listed, rejected, invalid, defect sdkgo.BranchID, decode func([]byte) (T, error)) sdkgo.QueryAttempt[T] {
	if errors.Is(response.err, errCredential) {
		return metadataBranch[T](operation, defect, sdkgo.FailureAuthentication, "ClickHouse credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return metadataRetry[T](operation, sdkgo.FailureTransport, "ClickHouse request failed", response.retryAfter)
	}
	if response.status == http.StatusTooManyRequests {
		return metadataRetry[T](operation, sdkgo.FailureRateLimit, "ClickHouse rate limited the request", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return metadataRetry[T](operation, sdkgo.FailureAvailability, "ClickHouse is temporarily unavailable", response.retryAfter)
	}
	if response.status != http.StatusOK {
		return metadataBranch[T](operation, rejected, sdkgo.FailureProviderRejection, "ClickHouse rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return metadataBranch[T](operation, invalid, sdkgo.FailureResponseTooLarge, "ClickHouse response exceeded the configured limit")
	}
	if response.err != nil {
		return metadataRetry[T](operation, sdkgo.FailureTransport, "ClickHouse response was interrupted", response.retryAfter)
	}
	value, err := decode(response.body)
	if err != nil {
		return metadataBranch[T](operation, invalid, sdkgo.FailureProtocol, "ClickHouse returned invalid metadata JSON")
	}
	return sdkgo.NewQueryBranch(listed, value, nil, sdkgo.Receipt{})
}

func metadataBranch[T any](operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	var zero T
	failure := metadataFailure(operation, kind, message)
	return sdkgo.NewQueryBranch(branch, zero, &failure, sdkgo.Receipt{})
}

func metadataRetry[T any](operation string, kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.QueryAttempt[T] {
	return sdkgo.NewQueryRetry[T](metadataFailure(operation, kind, message), retryAfter)
}

func metadataFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "clickhouse", Operation: operation, Message: message}
}
