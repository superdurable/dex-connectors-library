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

// CreateSheetInWorkspaceInput creates a sheet at a workspace's top level.
type CreateSheetInWorkspaceInput struct {
	// WorkspaceID is the positive decimal workspace ID from Smartsheet Home.
	WorkspaceID int64 `json:"workspaceId"`
	// Name is the new sheet's display name.
	Name string `json:"name"`
	// PrimaryColumnTitle is the title of its required text primary column.
	PrimaryColumnTitle string `json:"primaryColumnTitle"`
}

// CreateSheetInFolderInput creates a sheet within a folder.
type CreateSheetInFolderInput struct {
	// FolderID is the positive decimal folder ID from Smartsheet Home.
	FolderID int64 `json:"folderId"`
	// Name is the new sheet's display name.
	Name string `json:"name"`
	// PrimaryColumnTitle is the title of its required text primary column.
	PrimaryColumnTitle string `json:"primaryColumnTitle"`
}

// CreateSheetInWorkspaceOperation creates one sheet in a workspace.
type CreateSheetInWorkspaceOperation struct{ client *Client }

// CreateSheetInWorkspace binds the workspace sheet Mutation.
func (client *Client) CreateSheetInWorkspace() CreateSheetInWorkspaceOperation {
	return CreateSheetInWorkspaceOperation{client}
}

// Definition returns the generated operation contract.
func (CreateSheetInWorkspaceOperation) Definition() sdkgo.MutationDefinition {
	return CreateSheetInWorkspaceDefinition
}

// IdempotencyKey identifies one call; Smartsheet does not document create deduplication.
func (CreateSheetInWorkspaceOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateSheetInWorkspaceInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a sheet under a bounded send cap.
func (operation CreateSheetInWorkspaceOperation) Invoke(call sdkgo.Call, input CreateSheetInWorkspaceInput) sdkgo.MutationAttempt[SheetSummary] {
	if !validSmartsheetID(input.WorkspaceID) {
		return sheetSummaryBranch(CreateSheetInWorkspaceBranchDefect, "createSheetInWorkspace", sdkgo.FailureValidation, "workspace ID is invalid")
	}
	path := "/workspaces/" + strconv.FormatInt(input.WorkspaceID, 10) + "/sheets"
	return createSheet(call, operation.client, input.Name, input.PrimaryColumnTitle, path, CreateSheetInWorkspaceDefinition,
		"createSheetInWorkspace", CreateSheetInWorkspaceBranchCreated, CreateSheetInWorkspaceBranchProviderRejected, CreateSheetInWorkspaceBranchDefect)
}

// CreateSheetInFolderOperation creates one sheet in a folder.
type CreateSheetInFolderOperation struct{ client *Client }

// CreateSheetInFolder binds the folder sheet Mutation.
func (client *Client) CreateSheetInFolder() CreateSheetInFolderOperation {
	return CreateSheetInFolderOperation{client}
}

// Definition returns the generated operation contract.
func (CreateSheetInFolderOperation) Definition() sdkgo.MutationDefinition {
	return CreateSheetInFolderDefinition
}

// IdempotencyKey identifies one call; Smartsheet does not document create deduplication.
func (CreateSheetInFolderOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateSheetInFolderInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a sheet under a bounded send cap.
func (operation CreateSheetInFolderOperation) Invoke(call sdkgo.Call, input CreateSheetInFolderInput) sdkgo.MutationAttempt[SheetSummary] {
	if !validSmartsheetID(input.FolderID) {
		return sheetSummaryBranch(CreateSheetInFolderBranchDefect, "createSheetInFolder", sdkgo.FailureValidation, "folder ID is invalid")
	}
	path := "/folders/" + strconv.FormatInt(input.FolderID, 10) + "/sheets"
	return createSheet(call, operation.client, input.Name, input.PrimaryColumnTitle, path, CreateSheetInFolderDefinition,
		"createSheetInFolder", CreateSheetInFolderBranchCreated, CreateSheetInFolderBranchProviderRejected, CreateSheetInFolderBranchDefect)
}

func createSheet(call sdkgo.Call, client *Client, name, primaryColumnTitle, path string, definition sdkgo.MutationDefinition,
	operationID string, createdBranch, rejectedBranch, defectBranch sdkgo.BranchID) sdkgo.MutationAttempt[SheetSummary] {
	name = strings.TrimSpace(name)
	primaryColumnTitle = strings.TrimSpace(primaryColumnTitle)
	if name == "" || primaryColumnTitle == "" || len(name) > 255 || len(primaryColumnTitle) > 255 {
		return sheetSummaryBranch(defectBranch, operationID, sdkgo.FailureValidation, "sheet name or primary column title is invalid")
	}
	payload, err := json.Marshal(map[string]any{"name": name, "columns": []any{map[string]any{"title": primaryColumnTitle, "primary": true, "type": "TEXT_NUMBER"}}})
	if err != nil {
		return sheetSummaryBranch(defectBranch, operationID, sdkgo.FailureLocalDefect, "sheet request could not be encoded")
	}
	fence, err := singledispatch.NewFence(call, definition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return sheetSummaryBranch(defectBranch, operationID, sdkgo.FailureLocalDefect, "sheet checkpoint could not be initialized")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return sheetSummaryBranch(defectBranch, operationID, sdkgo.FailureLocalDefect, "sheet checkpoint is invalid")
	}
	if decision.Marker.ProviderID != "" {
		lookup := client.request(call, call.Context, http.MethodGet, "/sheets/"+decision.Marker.ProviderID, nil, false)
		if lookup.status == http.StatusOK && lookup.err == nil {
			summary, decodeErr := decodeSheetSummary(lookup.body)
			if decodeErr == nil && summary.Name == name && strconv.FormatInt(summary.ID, 10) == decision.Marker.ProviderID {
				return sdkgo.NewMutationBranch(createdBranch, summary, nil, sheetReceipt(call, decision.Marker.ProviderID))
			}
		}
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(SheetSummary{}, sheetFailure(operationID, sdkgo.FailureAvailability, "Smartsheet sheet creation cannot be confirmed"), sheetReceipt(call, ""))
	}
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = client.request(call, requestContext, http.MethodPost, path, payload, true)
		return nil
	})
	if err != nil {
		return sdkgo.NewMutationRetry[SheetSummary](sheetFailure(operationID, sdkgo.FailureAvailability, "sheet dispatch could not be checkpointed"), 0)
	}
	if errors.Is(response.err, errCredential) {
		return sheetSummaryBranch(defectBranch, operationID, sdkgo.FailureAuthentication, "Smartsheet credentials are unavailable")
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		_ = fence.RecordCanResend() // A lost checkpoint fails closed on the next attempt.
		return sdkgo.NewMutationRetry[SheetSummary](sheetFailure(operationID, sdkgo.FailureRateLimit, "Smartsheet temporarily refused the sheet"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 || response.err != nil {
		return sdkgo.NewMutationRetry[SheetSummary](sheetFailure(operationID, sdkgo.FailureAvailability, "Smartsheet sheet creation outcome is unknown"), response.retryAfter)
	}
	if response.status != http.StatusOK && response.status != http.StatusCreated {
		return sheetSummaryBranch(rejectedBranch, operationID, sdkgo.FailureProviderRejection, "Smartsheet rejected sheet creation")
	}
	summary, decodeErr := decodeSheetSummaryResult(response.body)
	if decodeErr != nil {
		return sdkgo.NewMutationRetry[SheetSummary](sheetFailure(operationID, sdkgo.FailureProtocol, "Smartsheet accepted the sheet but returned no usable ID"), 0)
	}
	_ = fence.RecordProviderID(strconv.FormatInt(summary.ID, 10)) // A lost checkpoint forbids resend.
	return sdkgo.NewMutationBranch(createdBranch, summary, nil, sheetReceipt(call, strconv.FormatInt(summary.ID, 10)))
}

// UpdateSheetInput replaces a sheet's name.
type UpdateSheetInput struct {
	// SheetID is the positive decimal sheet ID.
	SheetID int64 `json:"sheetId"`
	// Name is the replacement display name.
	Name string `json:"name"`
}

// UpdateSheetOperation renames one sheet.
type UpdateSheetOperation struct{ client *Client }

// UpdateSheet binds the sheet rename Mutation.
func (client *Client) UpdateSheet() UpdateSheetOperation { return UpdateSheetOperation{client} }

// Definition returns the generated update contract.
func (UpdateSheetOperation) Definition() sdkgo.MutationDefinition { return UpdateSheetDefinition }

// IdempotencyKey identifies a repeatable absolute rename.
func (UpdateSheetOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateSheetInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke renames the sheet and returns its new identity summary.
func (operation UpdateSheetOperation) Invoke(call sdkgo.Call, input UpdateSheetInput) sdkgo.MutationAttempt[SheetSummary] {
	name := strings.TrimSpace(input.Name)
	if !validSmartsheetID(input.SheetID) || name == "" || len(name) > 255 {
		return sheetSummaryBranch(UpdateSheetBranchDefect, "updateSheet", sdkgo.FailureValidation, "sheet ID or name is invalid")
	}
	payload, err := json.Marshal(map[string]string{"name": name})
	if err != nil {
		return sheetSummaryBranch(UpdateSheetBranchDefect, "updateSheet", sdkgo.FailureLocalDefect, "sheet rename could not be encoded")
	}
	response := operation.client.request(call, call.Context, http.MethodPut, "/sheets/"+strconv.FormatInt(input.SheetID, 10), payload, false)
	if isRowRetry(response) {
		return sdkgo.NewMutationRetry[SheetSummary](sheetFailure("updateSheet", sdkgo.FailureAvailability, "Smartsheet sheet update is unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, errCredential) {
		return sheetSummaryBranch(UpdateSheetBranchDefect, "updateSheet", sdkgo.FailureAuthentication, "Smartsheet credentials are unavailable")
	}
	if response.status == http.StatusNotFound {
		return sheetSummaryBranch(UpdateSheetBranchNotFound, "updateSheet", sdkgo.FailureNotFound, "Smartsheet did not find the sheet")
	}
	if response.status != http.StatusOK {
		return sheetSummaryBranch(UpdateSheetBranchProviderRejected, "updateSheet", sdkgo.FailureProviderRejection, "Smartsheet rejected the sheet update")
	}
	summary, decodeErr := decodeSheetSummaryResult(response.body)
	if decodeErr != nil {
		return sheetSummaryBranch(UpdateSheetBranchInvalidResponse, "updateSheet", sdkgo.FailureProtocol, "Smartsheet returned invalid sheet JSON")
	}
	return sdkgo.NewMutationBranch(UpdateSheetBranchUpdated, summary, nil, sheetReceipt(call, strconv.FormatInt(summary.ID, 10)))
}

// DeleteSheetInput identifies a sheet to remove.
type DeleteSheetInput struct {
	// SheetID is the positive decimal sheet ID.
	SheetID int64 `json:"sheetId"`
}

// DeleteSheetOutput confirms the removed sheet ID.
type DeleteSheetOutput struct {
	// SheetID is the removed sheet ID.
	SheetID int64 `json:"sheetId"`
}

// DeleteSheetOperation removes a sheet.
type DeleteSheetOperation struct{ client *Client }

// DeleteSheet binds the delete Mutation.
func (client *Client) DeleteSheet() DeleteSheetOperation { return DeleteSheetOperation{client} }

// Definition returns the generated delete contract.
func (DeleteSheetOperation) Definition() sdkgo.MutationDefinition { return DeleteSheetDefinition }

// IdempotencyKey identifies a repeatable deletion.
func (DeleteSheetOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteSheetInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes a sheet and confirms the provider success envelope.
func (operation DeleteSheetOperation) Invoke(call sdkgo.Call, input DeleteSheetInput) sdkgo.MutationAttempt[DeleteSheetOutput] {
	if !validSmartsheetID(input.SheetID) {
		return deleteSheetBranch(DeleteSheetBranchDefect, sdkgo.FailureValidation, "sheet ID is invalid")
	}
	response := operation.client.request(call, call.Context, http.MethodDelete, "/sheets/"+strconv.FormatInt(input.SheetID, 10), nil, false)
	if isRowRetry(response) {
		return sdkgo.NewMutationRetry[DeleteSheetOutput](sheetFailure("deleteSheet", sdkgo.FailureAvailability, "Smartsheet sheet deletion is unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, errCredential) {
		return deleteSheetBranch(DeleteSheetBranchDefect, sdkgo.FailureAuthentication, "Smartsheet credentials are unavailable")
	}
	if response.status == http.StatusNotFound && call.Context.Attempt() == 1 {
		return deleteSheetBranch(DeleteSheetBranchNotFound, sdkgo.FailureNotFound, "Smartsheet did not find the sheet")
	}
	if response.status == http.StatusNotFound && call.Context.Attempt() > 1 {
		return sdkgo.NewMutationBranch(DeleteSheetBranchDeleted, DeleteSheetOutput{SheetID: input.SheetID}, nil, sheetReceipt(call, strconv.FormatInt(input.SheetID, 10)))
	}
	if response.status != http.StatusOK {
		return deleteSheetBranch(DeleteSheetBranchProviderRejected, sdkgo.FailureProviderRejection, "Smartsheet rejected sheet deletion")
	}
	if !validSuccessEnvelope(response.body) {
		return deleteSheetBranch(DeleteSheetBranchInvalidResponse, sdkgo.FailureProtocol, "Smartsheet returned invalid deletion confirmation")
	}
	return sdkgo.NewMutationBranch(DeleteSheetBranchDeleted, DeleteSheetOutput{SheetID: input.SheetID}, nil, sheetReceipt(call, strconv.FormatInt(input.SheetID, 10)))
}

func decodeSheetSummary(body []byte) (SheetSummary, error) {
	var summary SheetSummary
	if err := json.Unmarshal(body, &summary); err != nil || !validSmartsheetID(summary.ID) || summary.Name == "" {
		return SheetSummary{}, errors.New("invalid Smartsheet sheet summary")
	}
	return summary, nil
}

func decodeSheetSummaryResult(body []byte) (SheetSummary, error) {
	var envelope struct {
		ResultCode *int            `json:"resultCode"`
		Message    string          `json:"message"`
		Result     json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.ResultCode == nil || *envelope.ResultCode != 0 || envelope.Message != "SUCCESS" {
		return SheetSummary{}, errors.New("invalid Smartsheet sheet result")
	}
	return decodeSheetSummary(envelope.Result)
}

func sheetFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "smartsheet", Operation: operation, Message: message}
}
func sheetReceipt(call sdkgo.Call, sheetID string) sdkgo.Receipt {
	return sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "smartsheet", ProviderObjectID: sheetID}
}
func sheetSummaryBranch(branch sdkgo.BranchID, operation string, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[SheetSummary] {
	failure := sheetFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, SheetSummary{}, &failure, sdkgo.Receipt{})
}
func deleteSheetBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[DeleteSheetOutput] {
	failure := sheetFailure("deleteSheet", kind, message)
	return sdkgo.NewMutationBranch(branch, DeleteSheetOutput{}, &failure, sdkgo.Receipt{})
}
