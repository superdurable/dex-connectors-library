// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package smartsheet

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

const maxSmartsheetID int64 = 9007199254740991

// GetRowInput identifies a row within a sheet.
type GetRowInput struct {
	// SheetID is the positive decimal sheet ID from Sheet Actions > Properties.
	SheetID int64 `json:"sheetId"`
	// RowID is the positive decimal row ID returned by the API.
	RowID int64 `json:"rowId"`
}

// GetRowOperation reads one row.
type GetRowOperation struct{ client *Client }

// GetRow binds the row Query to this client.
func (client *Client) GetRow() GetRowOperation { return GetRowOperation{client} }

// Definition returns the generated row Query contract.
func (GetRowOperation) Definition() sdkgo.QueryDefinition { return GetRowDefinition }

// Invoke reads one row and classifies the provider response.
func (operation GetRowOperation) Invoke(call sdkgo.Call, input GetRowInput) sdkgo.QueryAttempt[SheetRow] {
	if !validSmartsheetID(input.SheetID) || !validSmartsheetID(input.RowID) {
		return failedBranch[SheetRow]("getRow", GetRowBranchDefect, sdkgo.FailureValidation, "sheetId and rowId must be positive safe integers")
	}
	path := "/sheets/" + strconv.FormatInt(input.SheetID, 10) + "/rows/" + strconv.FormatInt(input.RowID, 10)
	return invokeGet(operation.client, call, "getRow", path, GetRowBranchFound, GetRowBranchNotFound, decodeSheetRow)
}

// RowCellInput sets a text cell in a row.
type RowCellInput struct {
	// ColumnID is a column ID from getSheet.
	ColumnID int64 `json:"columnId"`
	// Value is the text value to write, at most 4000 characters.
	Value string `json:"value"`
}

// AddRowInput appends one row to a sheet.
type AddRowInput struct {
	// SheetID is the positive decimal sheet ID.
	SheetID int64 `json:"sheetId"`
	// Cells are text values for distinct columns in that sheet.
	Cells []RowCellInput `json:"cells"`
}

// AddRowOperation appends a row with bounded duplicate risk.
type AddRowOperation struct{ client *Client }

// AddRow binds the append Mutation to this client.
func (client *Client) AddRow() AddRowOperation { return AddRowOperation{client} }

// Definition returns the generated add-row contract.
func (AddRowOperation) Definition() sdkgo.MutationDefinition { return AddRowDefinition }

// IdempotencyKey identifies the Dex call; Smartsheet does not document row-create deduplication.
func (AddRowOperation) IdempotencyKey(callID sdkgo.CallID, _ AddRowInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke appends one row through a bounded single-dispatch fence.
func (operation AddRowOperation) Invoke(call sdkgo.Call, input AddRowInput) sdkgo.MutationAttempt[SheetRow] {
	if !validSmartsheetID(input.SheetID) || !validRowCells(input.Cells) {
		return rowMutationBranch(AddRowBranchDefect, "addRow", sdkgo.FailureValidation, "sheet ID or row cells are invalid")
	}
	payload, err := json.Marshal([]any{map[string]any{"cells": input.Cells}})
	if err != nil {
		return rowMutationBranch(AddRowBranchDefect, "addRow", sdkgo.FailureLocalDefect, "row request could not be encoded")
	}
	fence, err := singledispatch.NewFence(call, AddRowDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return rowMutationBranch(AddRowBranchDefect, "addRow", sdkgo.FailureLocalDefect, "row checkpoint could not be initialized")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return rowMutationBranch(AddRowBranchDefect, "addRow", sdkgo.FailureLocalDefect, "row checkpoint is invalid")
	}
	path := "/sheets/" + strconv.FormatInt(input.SheetID, 10) + "/rows"
	if decision.Marker.ProviderID != "" {
		lookup := operation.client.request(call, call.Context, http.MethodGet, path+"/"+decision.Marker.ProviderID, nil, false)
		if lookup.status == http.StatusOK && lookup.err == nil {
			row, decodeErr := decodeSheetRow(lookup.body)
			if decodeErr == nil && rowCellsMatch(row, input.Cells) {
				return sdkgo.NewMutationBranch(AddRowBranchAdded, row, nil, rowReceipt(call, decision.Marker.ProviderID))
			}
		}
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(SheetRow{}, rowFailure("addRow", sdkgo.FailureAvailability, "Smartsheet row creation cannot be confirmed"), rowReceipt(call, ""))
	}
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.request(call, requestContext, http.MethodPost, path, payload, true)
		return nil
	})
	if err != nil {
		return sdkgo.NewMutationRetry[SheetRow](rowFailure("addRow", sdkgo.FailureAvailability, "row dispatch could not be checkpointed"), 0)
	}
	if errors.Is(response.err, errCredential) {
		return rowMutationBranch(AddRowBranchDefect, "addRow", sdkgo.FailureAuthentication, "Smartsheet credentials are unavailable")
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		_ = fence.RecordCanResend() // A lost checkpoint fails closed on the next attempt.
		return sdkgo.NewMutationRetry[SheetRow](rowFailure("addRow", sdkgo.FailureRateLimit, "Smartsheet temporarily refused the row"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 || response.err != nil {
		return sdkgo.NewMutationRetry[SheetRow](rowFailure("addRow", sdkgo.FailureAvailability, "Smartsheet row creation outcome is unknown"), response.retryAfter)
	}
	if response.status != http.StatusOK && response.status != http.StatusCreated {
		return rowMutationBranch(AddRowBranchProviderRejected, "addRow", sdkgo.FailureProviderRejection, "Smartsheet rejected the row")
	}
	row, decodeErr := decodeRowResult(response.body)
	if decodeErr != nil {
		return sdkgo.NewMutationRetry[SheetRow](rowFailure("addRow", sdkgo.FailureProtocol, "Smartsheet accepted the row but returned no usable ID"), 0)
	}
	_ = fence.RecordProviderID(strconv.FormatInt(row.ID, 10)) // A lost checkpoint forbids a resend.
	return sdkgo.NewMutationBranch(AddRowBranchAdded, row, nil, rowReceipt(call, strconv.FormatInt(row.ID, 10)))
}

// UpdateRowInput replaces text values in one row's cells.
type UpdateRowInput struct {
	// SheetID is the positive decimal sheet ID.
	SheetID int64 `json:"sheetId"`
	// RowID is the positive decimal row ID.
	RowID int64 `json:"rowId"`
	// Cells are text values for distinct columns.
	Cells []RowCellInput `json:"cells"`
}

// UpdateRowOperation sets absolute cell values.
type UpdateRowOperation struct{ client *Client }

// UpdateRow binds the row update Mutation.
func (client *Client) UpdateRow() UpdateRowOperation { return UpdateRowOperation{client} }

// Definition returns the generated update-row contract.
func (UpdateRowOperation) Definition() sdkgo.MutationDefinition { return UpdateRowDefinition }

// IdempotencyKey identifies one Dex call; replacing cells with the same values is repeatable.
func (UpdateRowOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateRowInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke updates one row's cells.
func (operation UpdateRowOperation) Invoke(call sdkgo.Call, input UpdateRowInput) sdkgo.MutationAttempt[SheetRow] {
	if !validSmartsheetID(input.SheetID) || !validSmartsheetID(input.RowID) || !validRowCells(input.Cells) {
		return rowMutationBranch(UpdateRowBranchDefect, "updateRow", sdkgo.FailureValidation, "sheet ID, row ID, or cells are invalid")
	}
	payload, err := json.Marshal([]any{map[string]any{"id": input.RowID, "cells": input.Cells}})
	if err != nil {
		return rowMutationBranch(UpdateRowBranchDefect, "updateRow", sdkgo.FailureLocalDefect, "row update could not be encoded")
	}
	path := "/sheets/" + strconv.FormatInt(input.SheetID, 10) + "/rows"
	response := operation.client.request(call, call.Context, http.MethodPut, path, payload, false)
	if isRowRetry(response) {
		return sdkgo.NewMutationRetry[SheetRow](rowFailure("updateRow", sdkgo.FailureAvailability, "Smartsheet row update is unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, errCredential) {
		return rowMutationBranch(UpdateRowBranchDefect, "updateRow", sdkgo.FailureAuthentication, "Smartsheet credentials are unavailable")
	}
	if response.status == http.StatusNotFound {
		return rowMutationBranch(UpdateRowBranchNotFound, "updateRow", sdkgo.FailureNotFound, "Smartsheet did not find the row")
	}
	if response.status != http.StatusOK {
		return rowMutationBranch(UpdateRowBranchProviderRejected, "updateRow", sdkgo.FailureProviderRejection, "Smartsheet rejected the update")
	}
	row, decodeErr := decodeRowResult(response.body)
	if decodeErr != nil {
		return rowMutationBranch(UpdateRowBranchInvalidResponse, "updateRow", sdkgo.FailureProtocol, "Smartsheet returned an invalid row")
	}
	return sdkgo.NewMutationBranch(UpdateRowBranchUpdated, row, nil, rowReceipt(call, strconv.FormatInt(row.ID, 10)))
}

// DeleteRowInput identifies a row to remove.
type DeleteRowInput struct {
	// SheetID is the positive decimal sheet ID.
	SheetID int64 `json:"sheetId"`
	// RowID is the positive decimal row ID.
	RowID int64 `json:"rowId"`
}

// DeleteRowOutput confirms the removed row ID.
type DeleteRowOutput struct {
	// RowID is the removed row ID.
	RowID int64 `json:"rowId"`
}

// DeleteRowOperation removes one row.
type DeleteRowOperation struct{ client *Client }

// DeleteRow binds the delete Mutation.
func (client *Client) DeleteRow() DeleteRowOperation { return DeleteRowOperation{client} }

// Definition returns the generated delete-row contract.
func (DeleteRowOperation) Definition() sdkgo.MutationDefinition { return DeleteRowDefinition }

// IdempotencyKey identifies one Dex call; deletion is safe to repeat.
func (DeleteRowOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteRowInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes one row and confirms the provider success envelope.
func (operation DeleteRowOperation) Invoke(call sdkgo.Call, input DeleteRowInput) sdkgo.MutationAttempt[DeleteRowOutput] {
	if !validSmartsheetID(input.SheetID) || !validSmartsheetID(input.RowID) {
		return deleteRowBranch(DeleteRowBranchDefect, sdkgo.FailureValidation, "sheet ID or row ID is invalid")
	}
	path := "/sheets/" + strconv.FormatInt(input.SheetID, 10) + "/rows?ids=" + strconv.FormatInt(input.RowID, 10)
	response := operation.client.request(call, call.Context, http.MethodDelete, path, nil, false)
	if isRowRetry(response) {
		return sdkgo.NewMutationRetry[DeleteRowOutput](rowFailure("deleteRow", sdkgo.FailureAvailability, "Smartsheet row delete is unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, errCredential) {
		return deleteRowBranch(DeleteRowBranchDefect, sdkgo.FailureAuthentication, "Smartsheet credentials are unavailable")
	}
	if response.status == http.StatusNotFound && call.Context.Attempt() == 1 {
		return deleteRowBranch(DeleteRowBranchNotFound, sdkgo.FailureNotFound, "Smartsheet did not find the row")
	}
	if response.status == http.StatusNotFound && call.Context.Attempt() > 1 {
		return sdkgo.NewMutationBranch(DeleteRowBranchDeleted, DeleteRowOutput{RowID: input.RowID}, nil, rowReceipt(call, strconv.FormatInt(input.RowID, 10)))
	}
	if response.status != http.StatusOK {
		return deleteRowBranch(DeleteRowBranchProviderRejected, sdkgo.FailureProviderRejection, "Smartsheet rejected row deletion")
	}
	if !validSuccessEnvelope(response.body) {
		return deleteRowBranch(DeleteRowBranchInvalidResponse, sdkgo.FailureProtocol, "Smartsheet returned invalid delete confirmation")
	}
	return sdkgo.NewMutationBranch(DeleteRowBranchDeleted, DeleteRowOutput{RowID: input.RowID}, nil, rowReceipt(call, strconv.FormatInt(input.RowID, 10)))
}

func validSmartsheetID(id int64) bool { return id > 0 && id <= maxSmartsheetID }

func validRowCells(cells []RowCellInput) bool {
	if len(cells) == 0 || len(cells) > 1000 {
		return false
	}
	seen := make(map[int64]bool, len(cells))
	for _, cell := range cells {
		if !validSmartsheetID(cell.ColumnID) || seen[cell.ColumnID] || cell.Value == "" || len(cell.Value) > 4000 {
			return false
		}
		seen[cell.ColumnID] = true
	}
	return true
}

func rowCellsMatch(row SheetRow, cells []RowCellInput) bool {
	if row.ID <= 0 {
		return false
	}
	values := map[int64]string{}
	for _, raw := range row.Cells {
		var cell RowCellInput
		if json.Unmarshal(raw, &cell) == nil {
			values[cell.ColumnID] = cell.Value
		}
	}
	for _, cell := range cells {
		value, exists := values[cell.ColumnID]
		if !exists || value != cell.Value {
			return false
		}
	}
	return true
}

func decodeSheetRow(body []byte) (SheetRow, error) {
	var row SheetRow
	if err := json.Unmarshal(body, &row); err != nil {
		return SheetRow{}, err
	}
	if !validSmartsheetID(row.ID) || row.Cells == nil {
		return SheetRow{}, errors.New("invalid Smartsheet row")
	}
	return row, nil
}

func decodeRowResult(body []byte) (SheetRow, error) {
	var envelope struct {
		ResultCode int               `json:"resultCode"`
		Result     []json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.ResultCode != 0 || len(envelope.Result) != 1 {
		return SheetRow{}, errors.New("invalid Smartsheet row result")
	}
	return decodeSheetRow(envelope.Result[0])
}

func validSuccessEnvelope(body []byte) bool {
	var envelope struct {
		ResultCode *int   `json:"resultCode"`
		Message    string `json:"message"`
	}
	return json.Unmarshal(body, &envelope) == nil && envelope.ResultCode != nil && *envelope.ResultCode == 0 && envelope.Message == "SUCCESS"
}

func isRowRetry(response providerResponse) bool {
	return response.status == http.StatusTooManyRequests || response.status == http.StatusRequestTimeout || response.status == http.StatusConflict || response.status >= 500 || errors.Is(response.err, errProviderRequest)
}

func rowFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "smartsheet", Operation: operation, Message: message}
}

func rowReceipt(call sdkgo.Call, rowID string) sdkgo.Receipt {
	return sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "smartsheet", ProviderObjectID: rowID}
}

func rowMutationBranch(branch sdkgo.BranchID, operation string, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[SheetRow] {
	failure := rowFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, SheetRow{}, &failure, sdkgo.Receipt{})
}

func deleteRowBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[DeleteRowOutput] {
	failure := rowFailure("deleteRow", kind, message)
	return sdkgo.NewMutationBranch(branch, DeleteRowOutput{}, &failure, sdkgo.Receipt{})
}
