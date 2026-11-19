// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package coda

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// Doc identifies one Coda document.
type Doc struct {
	// ID is the stable document identifier.
	ID string `json:"id"`
	// Name is the document's display name.
	Name string `json:"name"`
}

// Table identifies a Coda table or view.
type Table struct {
	// ID is the stable table identifier.
	ID string `json:"id"`
	// Name is the table's display name.
	Name string `json:"name"`
}

// Column identifies a Coda table column.
type Column struct {
	// ID is the stable column identifier.
	ID string `json:"id"`
	// Name is the column's display name.
	Name string `json:"name"`
}

// DocsPage contains one bounded doc page.
type DocsPage struct {
	// Items are the returned docs.
	Items []Doc `json:"items"`
	// NextPageToken requests the following page; blank means no more pages.
	NextPageToken string `json:"nextPageToken"`
}

// TablesPage contains one bounded table page.
type TablesPage struct {
	// Items are the returned tables and views.
	Items []Table `json:"items"`
	// NextPageToken requests the following page; blank means no more pages.
	NextPageToken string `json:"nextPageToken"`
}

// ColumnsPage contains one bounded column page.
type ColumnsPage struct {
	// Items are the returned columns.
	Items []Column `json:"items"`
	// NextPageToken requests the following page; blank means no more pages.
	NextPageToken string `json:"nextPageToken"`
}

// ListDocsInput selects one page of accessible documents.
type ListDocsInput struct {
	// Limit caps returned docs at 500; zero uses the provider default.
	Limit int `json:"limit,omitempty"`
	// PageToken is the opaque token from a prior page.
	PageToken string `json:"pageToken,omitempty"`
}

// GetDocInput identifies one accessible document.
type GetDocInput struct {
	// DocID is the document identifier from its URL.
	DocID string `json:"docId"`
}

// ListTablesInput selects one table page within a doc.
type ListTablesInput struct {
	// DocID identifies the parent doc.
	DocID string `json:"docId"`
	// Limit caps returned tables at 500; zero uses the provider default.
	Limit int `json:"limit,omitempty"`
	// PageToken is the opaque token from a prior page.
	PageToken string `json:"pageToken,omitempty"`
}

// GetTableInput identifies one table or view.
type GetTableInput struct {
	// DocID identifies the parent doc.
	DocID string `json:"docId"`
	// TableID identifies the table or view.
	TableID string `json:"tableId"`
}

// ListColumnsInput selects one column page within a table.
type ListColumnsInput struct {
	// DocID identifies the parent doc.
	DocID string `json:"docId"`
	// TableID identifies the parent table.
	TableID string `json:"tableId"`
	// Limit caps returned columns at 500; zero uses the provider default.
	Limit int `json:"limit,omitempty"`
	// PageToken is the opaque token from a prior page.
	PageToken string `json:"pageToken,omitempty"`
}

// ListDocsOperation reads a bounded doc page.
type ListDocsOperation struct{ client *Client }

// ListDocs binds the Query to this client.
func (client *Client) ListDocs() ListDocsOperation { return ListDocsOperation{client} }

// Definition returns the generated listDocs contract.
func (ListDocsOperation) Definition() sdkgo.QueryDefinition { return ListDocsDefinition }

// Invoke validates pagination and classifies the doc list response.
func (operation ListDocsOperation) Invoke(call sdkgo.Call, input ListDocsInput) sdkgo.QueryAttempt[DocsPage] {
	query, err := metadataPagination(input.Limit, input.PageToken)
	if err != nil {
		return metadataDefect[DocsPage]("listDocs", err)
	}
	response := operation.client.send(call, "listDocs", http.MethodGet, "/docs", query, nil, false, http.StatusOK)
	return metadataQueryAttempt(response, "listDocs", ListDocsBranchListed, ListDocsBranchInvalidResponse, func(body []byte) (DocsPage, error) {
		return decodeMetadataPage[Doc, DocsPage](body, func(item Doc) bool { return item.ID != "" && item.Name != "" }, func(items []Doc, token string) DocsPage { return DocsPage{Items: items, NextPageToken: token} })
	})
}

// GetDocOperation reads one document.
type GetDocOperation struct{ client *Client }

// GetDoc binds the Query to this client.
func (client *Client) GetDoc() GetDocOperation { return GetDocOperation{client} }

// Definition returns the generated getDoc contract.
func (GetDocOperation) Definition() sdkgo.QueryDefinition { return GetDocDefinition }

// Invoke validates identity and classifies the doc response.
func (operation GetDocOperation) Invoke(call sdkgo.Call, input GetDocInput) sdkgo.QueryAttempt[Doc] {
	if !identifier.MatchString(input.DocID) {
		return metadataDefect[Doc]("getDoc", errors.New("docId is invalid"))
	}
	response := operation.client.send(call, "getDoc", http.MethodGet, "/docs/"+input.DocID, nil, nil, true, http.StatusOK)
	return metadataQueryAttempt(response, "getDoc", GetDocBranchFound, GetDocBranchInvalidResponse, func(body []byte) (Doc, error) {
		var item Doc
		if err := json.Unmarshal(body, &item); err != nil {
			return Doc{}, err
		}
		if item.ID == "" || item.Name == "" {
			return Doc{}, errors.New("doc is incomplete")
		}
		return item, nil
	})
}

// ListTablesOperation reads a bounded table and view page.
type ListTablesOperation struct{ client *Client }

// ListTables binds the Query to this client.
func (client *Client) ListTables() ListTablesOperation { return ListTablesOperation{client} }

// Definition returns the generated listTables contract.
func (ListTablesOperation) Definition() sdkgo.QueryDefinition { return ListTablesDefinition }

// Invoke validates identity and pagination, then classifies the table page.
func (operation ListTablesOperation) Invoke(call sdkgo.Call, input ListTablesInput) sdkgo.QueryAttempt[TablesPage] {
	if !identifier.MatchString(input.DocID) {
		return metadataDefect[TablesPage]("listTables", errors.New("docId is invalid"))
	}
	query, err := metadataPagination(input.Limit, input.PageToken)
	if err != nil {
		return metadataDefect[TablesPage]("listTables", err)
	}
	response := operation.client.send(call, "listTables", http.MethodGet, "/docs/"+input.DocID+"/tables", query, nil, true, http.StatusOK)
	return metadataQueryAttempt(response, "listTables", ListTablesBranchListed, ListTablesBranchInvalidResponse, func(body []byte) (TablesPage, error) {
		return decodeMetadataPage[Table, TablesPage](body, func(item Table) bool { return item.ID != "" && item.Name != "" }, func(items []Table, token string) TablesPage { return TablesPage{Items: items, NextPageToken: token} })
	})
}

// GetTableOperation reads one table or view.
type GetTableOperation struct{ client *Client }

// GetTable binds the Query to this client.
func (client *Client) GetTable() GetTableOperation { return GetTableOperation{client} }

// Definition returns the generated getTable contract.
func (GetTableOperation) Definition() sdkgo.QueryDefinition { return GetTableDefinition }

// Invoke validates identity and classifies the table response.
func (operation GetTableOperation) Invoke(call sdkgo.Call, input GetTableInput) sdkgo.QueryAttempt[Table] {
	if !identifier.MatchString(input.DocID) || !identifier.MatchString(input.TableID) {
		return metadataDefect[Table]("getTable", errors.New("docId or tableId is invalid"))
	}
	response := operation.client.send(call, "getTable", http.MethodGet, "/docs/"+input.DocID+"/tables/"+input.TableID, nil, nil, true, http.StatusOK)
	return metadataQueryAttempt(response, "getTable", GetTableBranchFound, GetTableBranchInvalidResponse, func(body []byte) (Table, error) {
		var item Table
		if err := json.Unmarshal(body, &item); err != nil {
			return Table{}, err
		}
		if item.ID == "" || item.Name == "" {
			return Table{}, errors.New("table is incomplete")
		}
		return item, nil
	})
}

// ListColumnsOperation reads one bounded column page.
type ListColumnsOperation struct{ client *Client }

// ListColumns binds the Query to this client.
func (client *Client) ListColumns() ListColumnsOperation { return ListColumnsOperation{client} }

// Definition returns the generated listColumns contract.
func (ListColumnsOperation) Definition() sdkgo.QueryDefinition { return ListColumnsDefinition }

// Invoke validates identity and pagination, then classifies the column page.
func (operation ListColumnsOperation) Invoke(call sdkgo.Call, input ListColumnsInput) sdkgo.QueryAttempt[ColumnsPage] {
	if !identifier.MatchString(input.DocID) || !identifier.MatchString(input.TableID) {
		return metadataDefect[ColumnsPage]("listColumns", errors.New("docId or tableId is invalid"))
	}
	query, err := metadataPagination(input.Limit, input.PageToken)
	if err != nil {
		return metadataDefect[ColumnsPage]("listColumns", err)
	}
	response := operation.client.send(call, "listColumns", http.MethodGet, "/docs/"+input.DocID+"/tables/"+input.TableID+"/columns", query, nil, true, http.StatusOK)
	return metadataQueryAttempt(response, "listColumns", ListColumnsBranchListed, ListColumnsBranchInvalidResponse, func(body []byte) (ColumnsPage, error) {
		return decodeMetadataPage[Column, ColumnsPage](body, func(item Column) bool { return item.ID != "" && item.Name != "" }, func(items []Column, token string) ColumnsPage { return ColumnsPage{Items: items, NextPageToken: token} })
	})
}

func metadataPagination(limit int, pageToken string) (url.Values, error) {
	if limit < 0 || limit > 500 || len(pageToken) > 2048 {
		return nil, errors.New("limit or pageToken is invalid")
	}
	query := url.Values{}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	if pageToken != "" {
		query.Set("pageToken", pageToken)
	}
	return query, nil
}

func decodeMetadataPage[Item, Page any](body []byte, valid func(Item) bool, makePage func([]Item, string) Page) (Page, error) {
	var zero Page
	var wire struct {
		Items         []Item `json:"items"`
		NextPageToken string `json:"nextPageToken"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return zero, err
	}
	if wire.Items == nil {
		return zero, errors.New("metadata page is missing items")
	}
	for _, item := range wire.Items {
		if !valid(item) {
			return zero, errors.New("metadata item is incomplete")
		}
	}
	return makePage(wire.Items, wire.NextPageToken), nil
}

func metadataQueryAttempt[T any](response providerResponse, operation string, success, invalid sdkgo.BranchID, decode func([]byte) (T, error)) sdkgo.QueryAttempt[T] {
	var zero T
	if response.retry {
		return sdkgo.NewQueryRetry[T](*response.failure, response.retryAfter)
	}
	if response.failure != nil {
		return sdkgo.NewQueryBranch(response.branch, zero, response.failure, sdkgo.Receipt{})
	}
	value, err := decode(response.body)
	if err != nil {
		return sdkgo.NewQueryBranch(invalid, zero, failure(operation, sdkgo.FailureProtocol, "provider returned an incomplete metadata response"), sdkgo.Receipt{})
	}
	return sdkgo.NewQueryBranch(success, value, nil, sdkgo.Receipt{})
}

func metadataDefect[T any](operation string, err error) sdkgo.QueryAttempt[T] {
	var zero T
	return sdkgo.NewQueryBranch(sdkgo.DefectBranchID, zero, failure(operation, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
}
