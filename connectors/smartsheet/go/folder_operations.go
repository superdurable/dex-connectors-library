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

// FolderSummary identifies a Smartsheet folder.
type FolderSummary struct {
	// ID is the provider-assigned folder ID.
	ID int64 `json:"id"`
	// Name is the folder display name.
	Name string `json:"name"`
}

// GetFolderInput identifies a folder.
type GetFolderInput struct {
	// FolderID is the positive folder ID.
	FolderID int64 `json:"folderId"`
}

// GetFolderOperation reads folder metadata.
type GetFolderOperation struct{ client *Client }

// GetFolder binds the folder metadata Query.
func (client *Client) GetFolder() GetFolderOperation { return GetFolderOperation{client} }

// Definition returns the generated Query contract.
func (GetFolderOperation) Definition() sdkgo.QueryDefinition { return GetFolderDefinition }

// Invoke reads one folder's metadata.
func (operation GetFolderOperation) Invoke(call sdkgo.Call, input GetFolderInput) sdkgo.QueryAttempt[FolderSummary] {
	if !validSmartsheetID(input.FolderID) {
		return failedBranch[FolderSummary]("getFolder", GetFolderBranchDefect, sdkgo.FailureValidation, "folder ID is invalid")
	}
	return invokeGet(operation.client, call, "getFolder", folderPath(input.FolderID)+"/metadata", GetFolderBranchFound, GetFolderBranchNotFound, decodeFolderSummary)
}

// CreateFolderInWorkspaceInput creates a folder in a workspace.
type CreateFolderInWorkspaceInput struct {
	// WorkspaceID is the positive parent workspace ID.
	WorkspaceID int64 `json:"workspaceId"`
	// Name is the new folder name, at most 50 characters.
	Name string `json:"name"`
}

// CreateFolderInWorkspaceOperation creates one workspace folder.
type CreateFolderInWorkspaceOperation struct{ client *Client }

// CreateFolderInWorkspace binds the workspace folder Mutation.
func (client *Client) CreateFolderInWorkspace() CreateFolderInWorkspaceOperation {
	return CreateFolderInWorkspaceOperation{client}
}

// Definition returns the generated Mutation contract.
func (CreateFolderInWorkspaceOperation) Definition() sdkgo.MutationDefinition {
	return CreateFolderInWorkspaceDefinition
}

// IdempotencyKey identifies the call; Smartsheet does not deduplicate folder creates.
func (CreateFolderInWorkspaceOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateFolderInWorkspaceInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a workspace folder under a bounded send cap.
func (operation CreateFolderInWorkspaceOperation) Invoke(call sdkgo.Call, input CreateFolderInWorkspaceInput) sdkgo.MutationAttempt[FolderSummary] {
	if !validSmartsheetID(input.WorkspaceID) {
		return folderMutationBranch(CreateFolderInWorkspaceBranchDefect, "createFolderInWorkspace", sdkgo.FailureValidation, "workspace ID is invalid")
	}
	path := "/workspaces/" + strconv.FormatInt(input.WorkspaceID, 10) + "/folders"
	return createFolder(call, operation.client, path, input.Name, CreateFolderInWorkspaceDefinition, "createFolderInWorkspace", CreateFolderInWorkspaceBranchCreated, CreateFolderInWorkspaceBranchProviderRejected, CreateFolderInWorkspaceBranchDefect)
}

// CreateFolderInFolderInput creates a nested folder.
type CreateFolderInFolderInput struct {
	// FolderID is the positive parent folder ID.
	FolderID int64 `json:"folderId"`
	// Name is the new folder name, at most 50 characters.
	Name string `json:"name"`
}

// CreateFolderInFolderOperation creates one nested folder.
type CreateFolderInFolderOperation struct{ client *Client }

// CreateFolderInFolder binds the nested folder Mutation.
func (client *Client) CreateFolderInFolder() CreateFolderInFolderOperation {
	return CreateFolderInFolderOperation{client}
}

// Definition returns the generated Mutation contract.
func (CreateFolderInFolderOperation) Definition() sdkgo.MutationDefinition {
	return CreateFolderInFolderDefinition
}

// IdempotencyKey identifies the call; Smartsheet does not deduplicate folder creates.
func (CreateFolderInFolderOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateFolderInFolderInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a nested folder under a bounded send cap.
func (operation CreateFolderInFolderOperation) Invoke(call sdkgo.Call, input CreateFolderInFolderInput) sdkgo.MutationAttempt[FolderSummary] {
	if !validSmartsheetID(input.FolderID) {
		return folderMutationBranch(CreateFolderInFolderBranchDefect, "createFolderInFolder", sdkgo.FailureValidation, "parent folder ID is invalid")
	}
	path := folderPath(input.FolderID) + "/folders"
	return createFolder(call, operation.client, path, input.Name, CreateFolderInFolderDefinition, "createFolderInFolder", CreateFolderInFolderBranchCreated, CreateFolderInFolderBranchProviderRejected, CreateFolderInFolderBranchDefect)
}

func createFolder(call sdkgo.Call, client *Client, path, name string, definition sdkgo.MutationDefinition, operationID string, createdBranch, rejectedBranch, defectBranch sdkgo.BranchID) sdkgo.MutationAttempt[FolderSummary] {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 50 {
		return folderMutationBranch(defectBranch, operationID, sdkgo.FailureValidation, "folder name is invalid")
	}
	payload, err := json.Marshal(map[string]string{"name": name})
	if err != nil {
		return folderMutationBranch(defectBranch, operationID, sdkgo.FailureLocalDefect, "folder request could not be encoded")
	}
	fence, err := singledispatch.NewFence(call, definition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return folderMutationBranch(defectBranch, operationID, sdkgo.FailureLocalDefect, "folder checkpoint could not be initialized")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return folderMutationBranch(defectBranch, operationID, sdkgo.FailureLocalDefect, "folder checkpoint is invalid")
	}
	if decision.Marker.ProviderID != "" {
		lookup := client.request(call, call.Context, http.MethodGet, "/folders/"+decision.Marker.ProviderID+"/metadata", nil, false)
		if lookup.status == http.StatusOK && lookup.err == nil {
			folder, decodeErr := decodeFolderSummary(lookup.body)
			if decodeErr == nil && folder.Name == name && strconv.FormatInt(folder.ID, 10) == decision.Marker.ProviderID {
				return sdkgo.NewMutationBranch(createdBranch, folder, nil, sheetReceipt(call, decision.Marker.ProviderID))
			}
		}
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(FolderSummary{}, sheetFailure(operationID, sdkgo.FailureAvailability, "Smartsheet folder creation cannot be confirmed"), sheetReceipt(call, ""))
	}
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = client.request(call, requestContext, http.MethodPost, path, payload, true)
		return nil
	})
	if err != nil {
		return sdkgo.NewMutationRetry[FolderSummary](sheetFailure(operationID, sdkgo.FailureAvailability, "folder dispatch could not be checkpointed"), 0)
	}
	if errors.Is(response.err, errCredential) {
		return folderMutationBranch(defectBranch, operationID, sdkgo.FailureAuthentication, "Smartsheet credentials are unavailable")
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		_ = fence.RecordCanResend() // A lost checkpoint fails closed on the next attempt.
		return sdkgo.NewMutationRetry[FolderSummary](sheetFailure(operationID, sdkgo.FailureRateLimit, "Smartsheet temporarily refused the folder"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 || response.err != nil {
		return sdkgo.NewMutationRetry[FolderSummary](sheetFailure(operationID, sdkgo.FailureAvailability, "folder creation outcome is unknown"), response.retryAfter)
	}
	if response.status != http.StatusOK && response.status != http.StatusCreated {
		return folderMutationBranch(rejectedBranch, operationID, sdkgo.FailureProviderRejection, "Smartsheet rejected folder creation")
	}
	folder, decodeErr := decodeFolderResult(response.body)
	if decodeErr != nil {
		return sdkgo.NewMutationRetry[FolderSummary](sheetFailure(operationID, sdkgo.FailureProtocol, "Smartsheet accepted the folder but returned no usable ID"), 0)
	}
	_ = fence.RecordProviderID(strconv.FormatInt(folder.ID, 10)) // A lost checkpoint forbids resend.
	return sdkgo.NewMutationBranch(createdBranch, folder, nil, sheetReceipt(call, strconv.FormatInt(folder.ID, 10)))
}

// UpdateFolderInput renames a folder.
type UpdateFolderInput struct {
	// FolderID is the positive folder ID.
	FolderID int64 `json:"folderId"`
	// Name is the replacement name, at most 50 characters.
	Name string `json:"name"`
}

// UpdateFolderOperation renames one folder.
type UpdateFolderOperation struct{ client *Client }

// UpdateFolder binds the folder rename Mutation.
func (client *Client) UpdateFolder() UpdateFolderOperation { return UpdateFolderOperation{client} }

// Definition returns the generated Mutation contract.
func (UpdateFolderOperation) Definition() sdkgo.MutationDefinition { return UpdateFolderDefinition }

// IdempotencyKey identifies one repeatable absolute rename.
func (UpdateFolderOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateFolderInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke renames one folder.
func (operation UpdateFolderOperation) Invoke(call sdkgo.Call, input UpdateFolderInput) sdkgo.MutationAttempt[FolderSummary] {
	name := strings.TrimSpace(input.Name)
	if !validSmartsheetID(input.FolderID) || name == "" || len(name) > 50 {
		return folderMutationBranch(UpdateFolderBranchDefect, "updateFolder", sdkgo.FailureValidation, "folder input is invalid")
	}
	payload, err := json.Marshal(map[string]string{"name": name})
	if err != nil {
		return folderMutationBranch(UpdateFolderBranchDefect, "updateFolder", sdkgo.FailureLocalDefect, "folder rename could not be encoded")
	}
	response := operation.client.request(call, call.Context, http.MethodPut, folderPath(input.FolderID), payload, false)
	if isRowRetry(response) {
		return sdkgo.NewMutationRetry[FolderSummary](sheetFailure("updateFolder", sdkgo.FailureAvailability, "Smartsheet folder update is unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, errCredential) {
		return folderMutationBranch(UpdateFolderBranchDefect, "updateFolder", sdkgo.FailureAuthentication, "Smartsheet credentials are unavailable")
	}
	if response.status == http.StatusNotFound {
		return folderMutationBranch(UpdateFolderBranchNotFound, "updateFolder", sdkgo.FailureNotFound, "Smartsheet did not find the folder")
	}
	if response.status != http.StatusOK {
		return folderMutationBranch(UpdateFolderBranchProviderRejected, "updateFolder", sdkgo.FailureProviderRejection, "Smartsheet rejected folder update")
	}
	folder, decodeErr := decodeFolderResult(response.body)
	if decodeErr != nil {
		return folderMutationBranch(UpdateFolderBranchInvalidResponse, "updateFolder", sdkgo.FailureProtocol, "Smartsheet returned invalid folder JSON")
	}
	return sdkgo.NewMutationBranch(UpdateFolderBranchUpdated, folder, nil, sheetReceipt(call, strconv.FormatInt(folder.ID, 10)))
}

// DeleteFolderInput identifies a folder to remove.
type DeleteFolderInput struct {
	// FolderID is the positive folder ID.
	FolderID int64 `json:"folderId"`
}

// DeleteFolderOutput confirms the deleted folder ID.
type DeleteFolderOutput struct {
	// FolderID is the deleted folder ID.
	FolderID int64 `json:"folderId"`
}

// DeleteFolderOperation removes one folder.
type DeleteFolderOperation struct{ client *Client }

// DeleteFolder binds the folder deletion Mutation.
func (client *Client) DeleteFolder() DeleteFolderOperation { return DeleteFolderOperation{client} }

// Definition returns the generated Mutation contract.
func (DeleteFolderOperation) Definition() sdkgo.MutationDefinition { return DeleteFolderDefinition }

// IdempotencyKey identifies one repeatable folder deletion.
func (DeleteFolderOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteFolderInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes one folder and confirms the provider response.
func (operation DeleteFolderOperation) Invoke(call sdkgo.Call, input DeleteFolderInput) sdkgo.MutationAttempt[DeleteFolderOutput] {
	if !validSmartsheetID(input.FolderID) {
		return folderDeleteBranch(DeleteFolderBranchDefect, sdkgo.FailureValidation, "folder ID is invalid")
	}
	response := operation.client.request(call, call.Context, http.MethodDelete, folderPath(input.FolderID), nil, false)
	if isRowRetry(response) {
		return sdkgo.NewMutationRetry[DeleteFolderOutput](sheetFailure("deleteFolder", sdkgo.FailureAvailability, "Smartsheet folder deletion is unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, errCredential) {
		return folderDeleteBranch(DeleteFolderBranchDefect, sdkgo.FailureAuthentication, "Smartsheet credentials are unavailable")
	}
	if response.status == http.StatusNotFound && call.Context.Attempt() == 1 {
		return folderDeleteBranch(DeleteFolderBranchNotFound, sdkgo.FailureNotFound, "Smartsheet did not find the folder")
	}
	if response.status == http.StatusNotFound && call.Context.Attempt() > 1 {
		return sdkgo.NewMutationBranch(DeleteFolderBranchDeleted, DeleteFolderOutput{FolderID: input.FolderID}, nil, sheetReceipt(call, strconv.FormatInt(input.FolderID, 10)))
	}
	if response.status != http.StatusOK {
		return folderDeleteBranch(DeleteFolderBranchProviderRejected, sdkgo.FailureProviderRejection, "Smartsheet rejected folder deletion")
	}
	if !validSuccessEnvelope(response.body) {
		return folderDeleteBranch(DeleteFolderBranchInvalidResponse, sdkgo.FailureProtocol, "Smartsheet returned invalid deletion confirmation")
	}
	return sdkgo.NewMutationBranch(DeleteFolderBranchDeleted, DeleteFolderOutput{FolderID: input.FolderID}, nil, sheetReceipt(call, strconv.FormatInt(input.FolderID, 10)))
}

func folderPath(folderID int64) string { return "/folders/" + strconv.FormatInt(folderID, 10) }

func decodeFolderSummary(body []byte) (FolderSummary, error) {
	var folder FolderSummary
	if err := json.Unmarshal(body, &folder); err != nil || !validSmartsheetID(folder.ID) || folder.Name == "" {
		return FolderSummary{}, errors.New("invalid Smartsheet folder")
	}
	return folder, nil
}

func decodeFolderResult(body []byte) (FolderSummary, error) {
	var envelope struct {
		ResultCode *int            `json:"resultCode"`
		Message    string          `json:"message"`
		Result     json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.ResultCode == nil || *envelope.ResultCode != 0 || envelope.Message != "SUCCESS" {
		return FolderSummary{}, errors.New("invalid Smartsheet folder result")
	}
	return decodeFolderSummary(envelope.Result)
}

func folderMutationBranch(branch sdkgo.BranchID, operation string, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[FolderSummary] {
	failure := sheetFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, FolderSummary{}, &failure, sdkgo.Receipt{})
}
func folderDeleteBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[DeleteFolderOutput] {
	failure := sheetFailure("deleteFolder", kind, message)
	return sdkgo.NewMutationBranch(branch, DeleteFolderOutput{}, &failure, sdkgo.Receipt{})
}
