// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package smartsheet

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// ColumnSummary identifies a column and its provider type.
type ColumnSummary struct {
	// ID is the provider-assigned column ID.
	ID int64 `json:"id"`
	// Title is the column display name.
	Title string `json:"title"`
	// Type is the Smartsheet column type.
	Type string `json:"type"`
}

// ColumnPage is one page of columns.
type ColumnPage struct {
	// Columns holds the returned column definitions.
	Columns []ColumnSummary `json:"columns"`
	// PageNumber is the returned 1-based page number.
	PageNumber int `json:"pageNumber"`
	// TotalCount is the number of columns in the sheet.
	TotalCount int `json:"totalCount"`
}

// ListColumnsInput selects one page of a sheet's columns.
type ListColumnsInput struct {
	// SheetID is the positive sheet ID.
	SheetID int64 `json:"sheetId"`
	// Page is 1-based; zero selects the first page.
	Page int `json:"page,omitempty"`
}

// ListColumnsOperation reads one column page.
type ListColumnsOperation struct{ client *Client }

// ListColumns binds the column listing Query.
func (client *Client) ListColumns() ListColumnsOperation { return ListColumnsOperation{client} }

// Definition returns the generated Query contract.
func (ListColumnsOperation) Definition() sdkgo.QueryDefinition { return ListColumnsDefinition }

// Invoke lists columns from the requested sheet.
func (operation ListColumnsOperation) Invoke(call sdkgo.Call, input ListColumnsInput) sdkgo.QueryAttempt[ColumnPage] {
	if !validSmartsheetID(input.SheetID) || input.Page < 0 {
		return failedBranch[ColumnPage]("listColumns", ListColumnsBranchDefect, sdkgo.FailureValidation, "sheet ID or page is invalid")
	}
	page := input.Page
	if page == 0 {
		page = 1
	}
	path := columnCollectionPath(input.SheetID) + "?page=" + strconv.Itoa(page)
	return invokeGet(operation.client, call, "listColumns", path, ListColumnsBranchListed, ListColumnsBranchProviderRejected, decodeColumnPage)
}

// GetColumnInput identifies one column.
type GetColumnInput struct {
	// SheetID is the positive sheet ID.
	SheetID int64 `json:"sheetId"`
	// ColumnID is the positive column ID.
	ColumnID int64 `json:"columnId"`
}

// GetColumnOperation reads one column definition.
type GetColumnOperation struct{ client *Client }

// GetColumn binds the column read Query.
func (client *Client) GetColumn() GetColumnOperation { return GetColumnOperation{client} }

// Definition returns the generated Query contract.
func (GetColumnOperation) Definition() sdkgo.QueryDefinition { return GetColumnDefinition }

// Invoke reads one column definition.
func (operation GetColumnOperation) Invoke(call sdkgo.Call, input GetColumnInput) sdkgo.QueryAttempt[ColumnSummary] {
	if !validSmartsheetID(input.SheetID) || !validSmartsheetID(input.ColumnID) {
		return failedBranch[ColumnSummary]("getColumn", GetColumnBranchDefect, sdkgo.FailureValidation, "sheet or column ID is invalid")
	}
	return invokeGet(operation.client, call, "getColumn", columnPath(input.SheetID, input.ColumnID), GetColumnBranchFound, GetColumnBranchNotFound, decodeColumnSummary)
}

// AddColumnInput adds a text/number column to a sheet.
type AddColumnInput struct {
	// SheetID is the positive sheet ID.
	SheetID int64 `json:"sheetId"`
	// Title is the new column title, at most 50 characters.
	Title string `json:"title"`
	// Index is the 0-based insertion position; zero inserts first.
	Index int `json:"index"`
}

// AddColumnOperation creates one column with bounded repeat risk.
type AddColumnOperation struct{ client *Client }

// AddColumn binds the column create Mutation.
func (client *Client) AddColumn() AddColumnOperation { return AddColumnOperation{client} }

// Definition returns the generated Mutation contract.
func (AddColumnOperation) Definition() sdkgo.MutationDefinition { return AddColumnDefinition }

// IdempotencyKey identifies the call; Smartsheet does not deduplicate creates.
func (AddColumnOperation) IdempotencyKey(callID sdkgo.CallID, _ AddColumnInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke adds one text/number column through a bounded dispatch fence.
func (operation AddColumnOperation) Invoke(call sdkgo.Call, input AddColumnInput) sdkgo.MutationAttempt[ColumnSummary] {
	title := strings.TrimSpace(input.Title)
	if !validSmartsheetID(input.SheetID) || title == "" || len(title) > 50 || input.Index < 0 {
		return columnMutationBranch(AddColumnBranchDefect, "addColumn", sdkgo.FailureValidation, "column input is invalid")
	}
	payload, err := json.Marshal([]any{map[string]any{"title": title, "type": "TEXT_NUMBER", "index": input.Index}})
	if err != nil {
		return columnMutationBranch(AddColumnBranchDefect, "addColumn", sdkgo.FailureLocalDefect, "column request could not be encoded")
	}
	fence, err := singledispatch.NewFence(call, AddColumnDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return columnMutationBranch(AddColumnBranchDefect, "addColumn", sdkgo.FailureLocalDefect, "column checkpoint could not be initialized")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return columnMutationBranch(AddColumnBranchDefect, "addColumn", sdkgo.FailureLocalDefect, "column checkpoint is invalid")
	}
	if decision.Marker.ProviderID != "" {
		lookup := operation.client.request(call, call.Context, http.MethodGet, columnCollectionPath(input.SheetID)+"/"+decision.Marker.ProviderID, nil, false)
		if lookup.status == http.StatusOK && lookup.err == nil {
			column, decodeErr := decodeColumnSummary(lookup.body)
			if decodeErr == nil && column.Title == title && strconv.FormatInt(column.ID, 10) == decision.Marker.ProviderID {
				return sdkgo.NewMutationBranch(AddColumnBranchAdded, column, nil, sheetReceipt(call, decision.Marker.ProviderID))
			}
		}
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(ColumnSummary{}, sheetFailure("addColumn", sdkgo.FailureAvailability, "Smartsheet column creation cannot be confirmed"), sheetReceipt(call, ""))
	}
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.request(call, requestContext, http.MethodPost, columnCollectionPath(input.SheetID), payload, true)
		return nil
	})
	if err != nil {
		return sdkgo.NewMutationRetry[ColumnSummary](sheetFailure("addColumn", sdkgo.FailureAvailability, "column dispatch could not be checkpointed"), 0)
	}
	if errors.Is(response.err, errCredential) {
		return columnMutationBranch(AddColumnBranchDefect, "addColumn", sdkgo.FailureAuthentication, "Smartsheet credentials are unavailable")
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		_ = fence.RecordCanResend() // A lost checkpoint fails closed on the next attempt.
		return sdkgo.NewMutationRetry[ColumnSummary](sheetFailure("addColumn", sdkgo.FailureRateLimit, "Smartsheet temporarily refused the column"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 || response.err != nil {
		return sdkgo.NewMutationRetry[ColumnSummary](sheetFailure("addColumn", sdkgo.FailureAvailability, "column creation outcome is unknown"), response.retryAfter)
	}
	if response.status != http.StatusOK && response.status != http.StatusCreated {
		return columnMutationBranch(AddColumnBranchProviderRejected, "addColumn", sdkgo.FailureProviderRejection, "Smartsheet rejected column creation")
	}
	column, decodeErr := decodeColumnResult(response.body, true)
	if decodeErr != nil {
		return sdkgo.NewMutationRetry[ColumnSummary](sheetFailure("addColumn", sdkgo.FailureProtocol, "Smartsheet accepted the column but returned no usable ID"), 0)
	}
	_ = fence.RecordProviderID(strconv.FormatInt(column.ID, 10)) // A lost checkpoint forbids resend.
	return sdkgo.NewMutationBranch(AddColumnBranchAdded, column, nil, sheetReceipt(call, strconv.FormatInt(column.ID, 10)))
}

// UpdateColumnInput renames a column.
type UpdateColumnInput struct {
	// SheetID is the positive sheet ID.
	SheetID int64 `json:"sheetId"`
	// ColumnID is the positive column ID.
	ColumnID int64 `json:"columnId"`
	// Title is the replacement title.
	Title string `json:"title"`
}

// UpdateColumnOperation renames a column.
type UpdateColumnOperation struct{ client *Client }

// UpdateColumn binds the column rename Mutation.
func (client *Client) UpdateColumn() UpdateColumnOperation { return UpdateColumnOperation{client} }

// Definition returns the generated Mutation contract.
func (UpdateColumnOperation) Definition() sdkgo.MutationDefinition { return UpdateColumnDefinition }

// IdempotencyKey identifies one repeatable absolute rename.
func (UpdateColumnOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateColumnInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke renames one column.
func (operation UpdateColumnOperation) Invoke(call sdkgo.Call, input UpdateColumnInput) sdkgo.MutationAttempt[ColumnSummary] {
	title := strings.TrimSpace(input.Title)
	if !validSmartsheetID(input.SheetID) || !validSmartsheetID(input.ColumnID) || title == "" || len(title) > 50 {
		return columnMutationBranch(UpdateColumnBranchDefect, "updateColumn", sdkgo.FailureValidation, "column input is invalid")
	}
	payload, err := json.Marshal(map[string]string{"title": title})
	if err != nil {
		return columnMutationBranch(UpdateColumnBranchDefect, "updateColumn", sdkgo.FailureLocalDefect, "column rename could not be encoded")
	}
	response := operation.client.request(call, call.Context, http.MethodPut, columnPath(input.SheetID, input.ColumnID), payload, false)
	if isRowRetry(response) {
		return sdkgo.NewMutationRetry[ColumnSummary](sheetFailure("updateColumn", sdkgo.FailureAvailability, "Smartsheet column update is unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, errCredential) {
		return columnMutationBranch(UpdateColumnBranchDefect, "updateColumn", sdkgo.FailureAuthentication, "Smartsheet credentials are unavailable")
	}
	if response.status == http.StatusNotFound {
		return columnMutationBranch(UpdateColumnBranchNotFound, "updateColumn", sdkgo.FailureNotFound, "Smartsheet did not find the column")
	}
	if response.status != http.StatusOK {
		return columnMutationBranch(UpdateColumnBranchProviderRejected, "updateColumn", sdkgo.FailureProviderRejection, "Smartsheet rejected the column update")
	}
	column, decodeErr := decodeColumnResult(response.body, false)
	if decodeErr != nil {
		return columnMutationBranch(UpdateColumnBranchInvalidResponse, "updateColumn", sdkgo.FailureProtocol, "Smartsheet returned invalid column JSON")
	}
	return sdkgo.NewMutationBranch(UpdateColumnBranchUpdated, column, nil, sheetReceipt(call, strconv.FormatInt(column.ID, 10)))
}

// DeleteColumnInput identifies a column to remove.
type DeleteColumnInput struct {
	// SheetID is the positive sheet ID.
	SheetID int64 `json:"sheetId"`
	// ColumnID is the positive column ID.
	ColumnID int64 `json:"columnId"`
}

// DeleteColumnOutput confirms the deleted column ID.
type DeleteColumnOutput struct {
	// ColumnID is the deleted column ID.
	ColumnID int64 `json:"columnId"`
}

// DeleteColumnOperation removes one column.
type DeleteColumnOperation struct{ client *Client }

// DeleteColumn binds the column deletion Mutation.
func (client *Client) DeleteColumn() DeleteColumnOperation { return DeleteColumnOperation{client} }

// Definition returns the generated Mutation contract.
func (DeleteColumnOperation) Definition() sdkgo.MutationDefinition { return DeleteColumnDefinition }

// IdempotencyKey identifies one repeatable column deletion.
func (DeleteColumnOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteColumnInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes the column and confirms Smartsheet's success envelope.
func (operation DeleteColumnOperation) Invoke(call sdkgo.Call, input DeleteColumnInput) sdkgo.MutationAttempt[DeleteColumnOutput] {
	if !validSmartsheetID(input.SheetID) || !validSmartsheetID(input.ColumnID) {
		return columnDeleteBranch(DeleteColumnBranchDefect, sdkgo.FailureValidation, "column ID is invalid")
	}
	response := operation.client.request(call, call.Context, http.MethodDelete, columnPath(input.SheetID, input.ColumnID), nil, false)
	if isRowRetry(response) {
		return sdkgo.NewMutationRetry[DeleteColumnOutput](sheetFailure("deleteColumn", sdkgo.FailureAvailability, "Smartsheet column deletion is unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, errCredential) {
		return columnDeleteBranch(DeleteColumnBranchDefect, sdkgo.FailureAuthentication, "Smartsheet credentials are unavailable")
	}
	if response.status == http.StatusNotFound && call.Context.Attempt() == 1 {
		return columnDeleteBranch(DeleteColumnBranchNotFound, sdkgo.FailureNotFound, "Smartsheet did not find the column")
	}
	if response.status == http.StatusNotFound && call.Context.Attempt() > 1 {
		return sdkgo.NewMutationBranch(DeleteColumnBranchDeleted, DeleteColumnOutput{ColumnID: input.ColumnID}, nil, sheetReceipt(call, strconv.FormatInt(input.ColumnID, 10)))
	}
	if response.status != http.StatusOK {
		return columnDeleteBranch(DeleteColumnBranchProviderRejected, sdkgo.FailureProviderRejection, "Smartsheet rejected column deletion")
	}
	if !validSuccessEnvelope(response.body) {
		return columnDeleteBranch(DeleteColumnBranchInvalidResponse, sdkgo.FailureProtocol, "Smartsheet returned invalid deletion confirmation")
	}
	return sdkgo.NewMutationBranch(DeleteColumnBranchDeleted, DeleteColumnOutput{ColumnID: input.ColumnID}, nil, sheetReceipt(call, strconv.FormatInt(input.ColumnID, 10)))
}

func columnCollectionPath(sheetID int64) string {
	return "/sheets/" + strconv.FormatInt(sheetID, 10) + "/columns"
}
func columnPath(sheetID, columnID int64) string {
	return columnCollectionPath(sheetID) + "/" + strconv.FormatInt(columnID, 10)
}

func decodeColumnSummary(body []byte) (ColumnSummary, error) {
	var column ColumnSummary
	if err := json.Unmarshal(body, &column); err != nil || !validSmartsheetID(column.ID) || column.Title == "" || column.Type == "" {
		return ColumnSummary{}, errors.New("invalid Smartsheet column")
	}
	return column, nil
}

func decodeColumnPage(body []byte) (ColumnPage, error) {
	var response struct {
		Data       []ColumnSummary `json:"data"`
		PageNumber int             `json:"pageNumber"`
		TotalCount int             `json:"totalCount"`
	}
	if err := json.Unmarshal(body, &response); err != nil || response.Data == nil || response.PageNumber < 1 || response.TotalCount < 0 {
		return ColumnPage{}, errors.New("invalid Smartsheet column page")
	}
	for _, column := range response.Data {
		if !validSmartsheetID(column.ID) || column.Title == "" || column.Type == "" {
			return ColumnPage{}, errors.New("invalid Smartsheet column")
		}
	}
	return ColumnPage{Columns: response.Data, PageNumber: response.PageNumber, TotalCount: response.TotalCount}, nil
}

func decodeColumnResult(body []byte, isArray bool) (ColumnSummary, error) {
	var envelope struct {
		ResultCode *int            `json:"resultCode"`
		Message    string          `json:"message"`
		Result     json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.ResultCode == nil || *envelope.ResultCode != 0 || envelope.Message != "SUCCESS" {
		return ColumnSummary{}, errors.New("invalid Smartsheet column result")
	}
	if !isArray {
		return decodeColumnSummary(envelope.Result)
	}
	var columns []ColumnSummary
	if err := json.Unmarshal(envelope.Result, &columns); err != nil || len(columns) != 1 {
		return ColumnSummary{}, errors.New("invalid Smartsheet column array")
	}
	if !validSmartsheetID(columns[0].ID) || columns[0].Title == "" || columns[0].Type == "" {
		return ColumnSummary{}, errors.New("invalid Smartsheet column")
	}
	return columns[0], nil
}

func columnMutationBranch(branch sdkgo.BranchID, operation string, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[ColumnSummary] {
	failure := sheetFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, ColumnSummary{}, &failure, sdkgo.Receipt{})
}
func columnDeleteBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[DeleteColumnOutput] {
	failure := sheetFailure("deleteColumn", kind, message)
	return sdkgo.NewMutationBranch(branch, DeleteColumnOutput{}, &failure, sdkgo.Receipt{})
}
