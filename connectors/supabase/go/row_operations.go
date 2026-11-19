// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package supabase

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// GetRowInput identifies one row by a column equality filter.
type GetRowInput struct {
	// Table is an exposed table name.
	Table string `json:"table"`
	// Column is the column used to identify the row.
	Column string `json:"column"`
	// Value is the nonempty equality value.
	Value string `json:"value"`
}

// GetRowOutput holds the selected row.
type GetRowOutput struct {
	// Data is the selected JSON object.
	Data json.RawMessage `json:"data"`
}

// UpdateRowInput replaces named columns on rows matching the equality filter.
type UpdateRowInput struct {
	// Table is an exposed table name.
	Table string `json:"table"`
	// Column is the equality filter column.
	Column string `json:"column"`
	// Value is the nonempty equality value.
	Value string `json:"value"`
	// Values are literal replacement column values; expressions are not evaluated.
	Values map[string]any `json:"values"`
}

// UpdateRowOutput holds the rows returned after replacement.
type UpdateRowOutput struct {
	// Data is the JSON row array.
	Data json.RawMessage `json:"data"`
}

// DeleteRowInput identifies rows to delete by a required equality filter.
type DeleteRowInput struct {
	// Table is an exposed table name.
	Table string `json:"table"`
	// Column is the equality filter column.
	Column string `json:"column"`
	// Value is the nonempty equality value.
	Value string `json:"value"`
}

// DeleteRowOutput holds deleted rows returned by PostgREST.
type DeleteRowOutput struct {
	// Data is the JSON row array, empty if no row matched.
	Data json.RawMessage `json:"data"`
}

// GetRowOperation reads one row from a table.
type GetRowOperation struct{ client *Client }

// GetRow returns this client's typed row query.
func (client *Client) GetRow() GetRowOperation { return GetRowOperation{client} }

// Definition returns the generated operation contract.
func (GetRowOperation) Definition() sdkgo.QueryDefinition { return GetRowDefinition }

// Invoke requests a single row and distinguishes an empty result.
func (operation GetRowOperation) Invoke(call sdkgo.Call, input GetRowInput) sdkgo.QueryAttempt[GetRowOutput] {
	path, query, err := rowFilter(input.Table, input.Column, input.Value)
	if err != nil {
		return sdkgo.NewQueryBranch(GetRowBranchDefect, GetRowOutput{}, failure("getRow", sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	query.Set("select", "*")
	query.Set("limit", "1")
	response := operation.client.send(call, "getRow", http.MethodGet, path, query, nil, false, http.StatusOK)
	if response.retry {
		return sdkgo.NewQueryRetry[GetRowOutput](*response.failure, response.retryAfter)
	}
	if response.failure != nil {
		return sdkgo.NewQueryBranch(response.branch, GetRowOutput{}, response.failure, sdkgo.Receipt{})
	}
	var rows []json.RawMessage
	if json.Unmarshal(response.body, &rows) != nil || rows == nil || len(rows) > 1 {
		return sdkgo.NewQueryBranch(GetRowBranchInvalidResponse, GetRowOutput{}, failure("getRow", sdkgo.FailureProtocol, "provider returned an invalid row array"), sdkgo.Receipt{})
	}
	if len(rows) == 0 {
		return sdkgo.NewQueryBranch(GetRowBranchNotFound, GetRowOutput{}, failure("getRow", sdkgo.FailureNotFound, "row was not found"), sdkgo.Receipt{})
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(rows[0], &object) != nil || object == nil {
		return sdkgo.NewQueryBranch(GetRowBranchInvalidResponse, GetRowOutput{}, failure("getRow", sdkgo.FailureProtocol, "provider returned an invalid row"), sdkgo.Receipt{})
	}
	return sdkgo.NewQueryBranch(GetRowBranchFound, GetRowOutput{Data: rows[0]}, nil, sdkgo.Receipt{})
}

// UpdateRowOperation replaces columns on matching rows.
type UpdateRowOperation struct{ client *Client }

// UpdateRow returns this client's typed row mutation.
func (client *Client) UpdateRow() UpdateRowOperation { return UpdateRowOperation{client} }

// Definition returns the generated operation contract.
func (UpdateRowOperation) Definition() sdkgo.MutationDefinition { return UpdateRowDefinition }

// IdempotencyKey derives stable call identity for an absolute replacement.
func (UpdateRowOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateRowInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends one absolute replacement request.
func (operation UpdateRowOperation) Invoke(call sdkgo.Call, input UpdateRowInput) sdkgo.MutationAttempt[UpdateRowOutput] {
	path, query, err := rowFilter(input.Table, input.Column, input.Value)
	if err != nil || len(input.Values) == 0 {
		return sdkgo.NewMutationBranch(UpdateRowBranchDefect, UpdateRowOutput{}, failure("updateRow", sdkgo.FailureValidation, "table, filter, and replacement values are required"), sdkgo.Receipt{})
	}
	for column := range input.Values {
		if !identifier.MatchString(column) {
			return sdkgo.NewMutationBranch(UpdateRowBranchDefect, UpdateRowOutput{}, failure("updateRow", sdkgo.FailureValidation, "replacement column is invalid"), sdkgo.Receipt{})
		}
	}
	body, err := json.Marshal(input.Values)
	if err != nil {
		return sdkgo.NewMutationBranch(UpdateRowBranchDefect, UpdateRowOutput{}, failure("updateRow", sdkgo.FailureValidation, "replacement values are not JSON serializable"), sdkgo.Receipt{})
	}
	response := operation.client.send(call, "updateRow", http.MethodPatch, path, query, body, false, http.StatusOK)
	if response.retry {
		return sdkgo.NewMutationRetry[UpdateRowOutput](*response.failure, response.retryAfter)
	}
	if response.failure != nil {
		return sdkgo.NewMutationBranch(response.branch, UpdateRowOutput{}, response.failure, sdkgo.Receipt{})
	}
	if !hasResponseShape(response.body, "array") {
		return sdkgo.NewMutationBranch(UpdateRowBranchInvalidResponse, UpdateRowOutput{}, failure("updateRow", sdkgo.FailureProtocol, "provider returned an invalid row array"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(UpdateRowBranchUpdated, UpdateRowOutput{Data: response.body}, nil, sdkgo.Receipt{})
}

// DeleteRowOperation deletes rows matching a required equality filter.
type DeleteRowOperation struct{ client *Client }

// DeleteRow returns this client's typed row mutation.
func (client *Client) DeleteRow() DeleteRowOperation { return DeleteRowOperation{client} }

// Definition returns the generated operation contract.
func (DeleteRowOperation) Definition() sdkgo.MutationDefinition { return DeleteRowDefinition }

// IdempotencyKey derives stable call identity for repeatable deletion.
func (DeleteRowOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteRowInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes matching rows without permitting an unfiltered table delete.
func (operation DeleteRowOperation) Invoke(call sdkgo.Call, input DeleteRowInput) sdkgo.MutationAttempt[DeleteRowOutput] {
	path, query, err := rowFilter(input.Table, input.Column, input.Value)
	if err != nil {
		return sdkgo.NewMutationBranch(DeleteRowBranchDefect, DeleteRowOutput{}, failure("deleteRow", sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	response := operation.client.send(call, "deleteRow", http.MethodDelete, path, query, nil, false, http.StatusOK)
	if response.retry {
		return sdkgo.NewMutationRetry[DeleteRowOutput](*response.failure, response.retryAfter)
	}
	if response.failure != nil {
		return sdkgo.NewMutationBranch(response.branch, DeleteRowOutput{}, response.failure, sdkgo.Receipt{})
	}
	if !hasResponseShape(response.body, "array") {
		return sdkgo.NewMutationBranch(DeleteRowBranchInvalidResponse, DeleteRowOutput{}, failure("deleteRow", sdkgo.FailureProtocol, "provider returned an invalid row array"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(DeleteRowBranchDeleted, DeleteRowOutput{Data: response.body}, nil, sdkgo.Receipt{})
}

func rowFilter(table, column, value string) (string, url.Values, error) {
	if !identifier.MatchString(table) || !identifier.MatchString(column) || strings.TrimSpace(value) == "" {
		return "", nil, fmt.Errorf("table, filter column, and nonempty value are required")
	}
	return "/" + table, url.Values{column: {"eq." + value}}, nil
}
