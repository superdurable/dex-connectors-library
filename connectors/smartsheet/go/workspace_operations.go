// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package smartsheet

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// WorkspaceSummary identifies a Smartsheet workspace.
type WorkspaceSummary struct {
	// ID is the provider-assigned workspace ID.
	ID int64 `json:"id"`
	// Name is the workspace display name.
	Name string `json:"name"`
}

// WorkspacePage is one token-paginated workspace result.
type WorkspacePage struct {
	// Workspaces holds accessible workspace identities.
	Workspaces []WorkspaceSummary `json:"workspaces"`
	// LastKey is the token for the next page; blank means there is no next page.
	LastKey string `json:"lastKey"`
}

// ListWorkspacesInput selects a workspace page.
type ListWorkspacesInput struct {
	// MaxItems is the page size, from 100 to 1000; zero uses 100.
	MaxItems int `json:"maxItems,omitempty"`
	// LastKey is the opaque token returned by the preceding page.
	LastKey string `json:"lastKey,omitempty"`
}

// ListWorkspacesOperation reads accessible workspaces.
type ListWorkspacesOperation struct{ client *Client }

// ListWorkspaces binds the workspace listing Query.
func (client *Client) ListWorkspaces() ListWorkspacesOperation {
	return ListWorkspacesOperation{client}
}

// Definition returns the generated Query contract.
func (ListWorkspacesOperation) Definition() sdkgo.QueryDefinition { return ListWorkspacesDefinition }

// Invoke reads one token-paginated workspace page.
func (operation ListWorkspacesOperation) Invoke(call sdkgo.Call, input ListWorkspacesInput) sdkgo.QueryAttempt[WorkspacePage] {
	maxItems := input.MaxItems
	if maxItems == 0 {
		maxItems = 100
	}
	if maxItems < 100 || maxItems > 1000 || len(input.LastKey) > 1024 {
		return failedBranch[WorkspacePage]("listWorkspaces", ListWorkspacesBranchDefect, sdkgo.FailureValidation, "workspace page selection is invalid")
	}
	path := "/workspaces?paginationType=token&maxItems=" + strconv.Itoa(maxItems)
	if input.LastKey != "" {
		path += "&lastKey=" + url.QueryEscape(input.LastKey)
	}
	return invokeGet(operation.client, call, "listWorkspaces", path, ListWorkspacesBranchListed, ListWorkspacesBranchProviderRejected, decodeWorkspacePage)
}

// GetWorkspaceInput identifies a workspace.
type GetWorkspaceInput struct {
	// WorkspaceID is the positive workspace ID.
	WorkspaceID int64 `json:"workspaceId"`
}

// GetWorkspaceOperation reads workspace metadata.
type GetWorkspaceOperation struct{ client *Client }

// GetWorkspace binds the workspace metadata Query.
func (client *Client) GetWorkspace() GetWorkspaceOperation { return GetWorkspaceOperation{client} }

// Definition returns the generated Query contract.
func (GetWorkspaceOperation) Definition() sdkgo.QueryDefinition { return GetWorkspaceDefinition }

// Invoke reads one workspace's metadata.
func (operation GetWorkspaceOperation) Invoke(call sdkgo.Call, input GetWorkspaceInput) sdkgo.QueryAttempt[WorkspaceSummary] {
	if !validSmartsheetID(input.WorkspaceID) {
		return failedBranch[WorkspaceSummary]("getWorkspace", GetWorkspaceBranchDefect, sdkgo.FailureValidation, "workspace ID is invalid")
	}
	return invokeGet(operation.client, call, "getWorkspace", workspacePath(input.WorkspaceID)+"/metadata", GetWorkspaceBranchFound, GetWorkspaceBranchNotFound, decodeWorkspaceSummary)
}

// CreateWorkspaceInput creates a workspace.
type CreateWorkspaceInput struct {
	// Name is the workspace display name, at most 255 characters.
	Name string `json:"name"`
}

// CreateWorkspaceOperation creates one workspace.
type CreateWorkspaceOperation struct{ client *Client }

// CreateWorkspace binds the workspace create Mutation.
func (client *Client) CreateWorkspace() CreateWorkspaceOperation {
	return CreateWorkspaceOperation{client}
}

// Definition returns the generated Mutation contract.
func (CreateWorkspaceOperation) Definition() sdkgo.MutationDefinition {
	return CreateWorkspaceDefinition
}

// IdempotencyKey identifies the call; Smartsheet does not deduplicate creates.
func (CreateWorkspaceOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateWorkspaceInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates one workspace under a bounded send cap.
func (operation CreateWorkspaceOperation) Invoke(call sdkgo.Call, input CreateWorkspaceInput) sdkgo.MutationAttempt[WorkspaceSummary] {
	name := strings.TrimSpace(input.Name)
	if name == "" || len(name) > 255 {
		return workspaceMutationBranch(CreateWorkspaceBranchDefect, "createWorkspace", sdkgo.FailureValidation, "workspace name is invalid")
	}
	payload, err := json.Marshal(map[string]string{"name": name})
	if err != nil {
		return workspaceMutationBranch(CreateWorkspaceBranchDefect, "createWorkspace", sdkgo.FailureLocalDefect, "workspace request could not be encoded")
	}
	fence, err := singledispatch.NewFence(call, CreateWorkspaceDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return workspaceMutationBranch(CreateWorkspaceBranchDefect, "createWorkspace", sdkgo.FailureLocalDefect, "workspace checkpoint could not be initialized")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return workspaceMutationBranch(CreateWorkspaceBranchDefect, "createWorkspace", sdkgo.FailureLocalDefect, "workspace checkpoint is invalid")
	}
	if decision.Marker.ProviderID != "" {
		lookup := operation.client.request(call, call.Context, http.MethodGet, "/workspaces/"+decision.Marker.ProviderID+"/metadata", nil, false)
		if lookup.status == http.StatusOK && lookup.err == nil {
			workspace, decodeErr := decodeWorkspaceSummary(lookup.body)
			if decodeErr == nil && workspace.Name == name && strconv.FormatInt(workspace.ID, 10) == decision.Marker.ProviderID {
				return sdkgo.NewMutationBranch(CreateWorkspaceBranchCreated, workspace, nil, sheetReceipt(call, decision.Marker.ProviderID))
			}
		}
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(WorkspaceSummary{}, sheetFailure("createWorkspace", sdkgo.FailureAvailability, "Smartsheet workspace creation cannot be confirmed"), sheetReceipt(call, ""))
	}
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.request(call, requestContext, http.MethodPost, "/workspaces", payload, true)
		return nil
	})
	if err != nil {
		return sdkgo.NewMutationRetry[WorkspaceSummary](sheetFailure("createWorkspace", sdkgo.FailureAvailability, "workspace dispatch could not be checkpointed"), 0)
	}
	if errors.Is(response.err, errCredential) {
		return workspaceMutationBranch(CreateWorkspaceBranchDefect, "createWorkspace", sdkgo.FailureAuthentication, "Smartsheet credentials are unavailable")
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		_ = fence.RecordCanResend() // A lost checkpoint fails closed on the next attempt.
		return sdkgo.NewMutationRetry[WorkspaceSummary](sheetFailure("createWorkspace", sdkgo.FailureRateLimit, "Smartsheet temporarily refused the workspace"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 || response.err != nil {
		return sdkgo.NewMutationRetry[WorkspaceSummary](sheetFailure("createWorkspace", sdkgo.FailureAvailability, "workspace creation outcome is unknown"), response.retryAfter)
	}
	if response.status != http.StatusOK && response.status != http.StatusCreated {
		return workspaceMutationBranch(CreateWorkspaceBranchProviderRejected, "createWorkspace", sdkgo.FailureProviderRejection, "Smartsheet rejected workspace creation")
	}
	workspace, decodeErr := decodeWorkspaceResult(response.body)
	if decodeErr != nil {
		return sdkgo.NewMutationRetry[WorkspaceSummary](sheetFailure("createWorkspace", sdkgo.FailureProtocol, "Smartsheet accepted the workspace but returned no usable ID"), 0)
	}
	_ = fence.RecordProviderID(strconv.FormatInt(workspace.ID, 10)) // A lost checkpoint forbids resend.
	return sdkgo.NewMutationBranch(CreateWorkspaceBranchCreated, workspace, nil, sheetReceipt(call, strconv.FormatInt(workspace.ID, 10)))
}

// UpdateWorkspaceInput renames a workspace.
type UpdateWorkspaceInput struct {
	// WorkspaceID is the positive workspace ID.
	WorkspaceID int64 `json:"workspaceId"`
	// Name is the replacement workspace name.
	Name string `json:"name"`
}

// UpdateWorkspaceOperation renames one workspace.
type UpdateWorkspaceOperation struct{ client *Client }

// UpdateWorkspace binds the workspace rename Mutation.
func (client *Client) UpdateWorkspace() UpdateWorkspaceOperation {
	return UpdateWorkspaceOperation{client}
}

// Definition returns the generated Mutation contract.
func (UpdateWorkspaceOperation) Definition() sdkgo.MutationDefinition {
	return UpdateWorkspaceDefinition
}

// IdempotencyKey identifies one repeatable absolute rename.
func (UpdateWorkspaceOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateWorkspaceInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke renames one workspace.
func (operation UpdateWorkspaceOperation) Invoke(call sdkgo.Call, input UpdateWorkspaceInput) sdkgo.MutationAttempt[WorkspaceSummary] {
	name := strings.TrimSpace(input.Name)
	if !validSmartsheetID(input.WorkspaceID) || name == "" || len(name) > 255 {
		return workspaceMutationBranch(UpdateWorkspaceBranchDefect, "updateWorkspace", sdkgo.FailureValidation, "workspace input is invalid")
	}
	payload, err := json.Marshal(map[string]string{"name": name})
	if err != nil {
		return workspaceMutationBranch(UpdateWorkspaceBranchDefect, "updateWorkspace", sdkgo.FailureLocalDefect, "workspace rename could not be encoded")
	}
	response := operation.client.request(call, call.Context, http.MethodPut, workspacePath(input.WorkspaceID), payload, false)
	if isRowRetry(response) {
		return sdkgo.NewMutationRetry[WorkspaceSummary](sheetFailure("updateWorkspace", sdkgo.FailureAvailability, "Smartsheet workspace update is unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, errCredential) {
		return workspaceMutationBranch(UpdateWorkspaceBranchDefect, "updateWorkspace", sdkgo.FailureAuthentication, "Smartsheet credentials are unavailable")
	}
	if response.status == http.StatusNotFound {
		return workspaceMutationBranch(UpdateWorkspaceBranchNotFound, "updateWorkspace", sdkgo.FailureNotFound, "Smartsheet did not find the workspace")
	}
	if response.status != http.StatusOK {
		return workspaceMutationBranch(UpdateWorkspaceBranchProviderRejected, "updateWorkspace", sdkgo.FailureProviderRejection, "Smartsheet rejected workspace update")
	}
	workspace, decodeErr := decodeWorkspaceResult(response.body)
	if decodeErr != nil {
		return workspaceMutationBranch(UpdateWorkspaceBranchInvalidResponse, "updateWorkspace", sdkgo.FailureProtocol, "Smartsheet returned invalid workspace JSON")
	}
	return sdkgo.NewMutationBranch(UpdateWorkspaceBranchUpdated, workspace, nil, sheetReceipt(call, strconv.FormatInt(workspace.ID, 10)))
}

// DeleteWorkspaceInput identifies a workspace to remove.
type DeleteWorkspaceInput struct {
	// WorkspaceID is the positive workspace ID.
	WorkspaceID int64 `json:"workspaceId"`
}

// DeleteWorkspaceOutput confirms the deleted workspace ID.
type DeleteWorkspaceOutput struct {
	// WorkspaceID is the deleted workspace ID.
	WorkspaceID int64 `json:"workspaceId"`
}

// DeleteWorkspaceOperation removes one workspace.
type DeleteWorkspaceOperation struct{ client *Client }

// DeleteWorkspace binds the workspace deletion Mutation.
func (client *Client) DeleteWorkspace() DeleteWorkspaceOperation {
	return DeleteWorkspaceOperation{client}
}

// Definition returns the generated Mutation contract.
func (DeleteWorkspaceOperation) Definition() sdkgo.MutationDefinition {
	return DeleteWorkspaceDefinition
}

// IdempotencyKey identifies one repeatable workspace deletion.
func (DeleteWorkspaceOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteWorkspaceInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes a workspace and confirms the provider response.
func (operation DeleteWorkspaceOperation) Invoke(call sdkgo.Call, input DeleteWorkspaceInput) sdkgo.MutationAttempt[DeleteWorkspaceOutput] {
	if !validSmartsheetID(input.WorkspaceID) {
		return workspaceDeleteBranch(DeleteWorkspaceBranchDefect, sdkgo.FailureValidation, "workspace ID is invalid")
	}
	response := operation.client.request(call, call.Context, http.MethodDelete, workspacePath(input.WorkspaceID), nil, false)
	if isRowRetry(response) {
		return sdkgo.NewMutationRetry[DeleteWorkspaceOutput](sheetFailure("deleteWorkspace", sdkgo.FailureAvailability, "Smartsheet workspace deletion is unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, errCredential) {
		return workspaceDeleteBranch(DeleteWorkspaceBranchDefect, sdkgo.FailureAuthentication, "Smartsheet credentials are unavailable")
	}
	if response.status == http.StatusNotFound && call.Context.Attempt() == 1 {
		return workspaceDeleteBranch(DeleteWorkspaceBranchNotFound, sdkgo.FailureNotFound, "Smartsheet did not find the workspace")
	}
	if response.status == http.StatusNotFound && call.Context.Attempt() > 1 {
		return sdkgo.NewMutationBranch(DeleteWorkspaceBranchDeleted, DeleteWorkspaceOutput{WorkspaceID: input.WorkspaceID}, nil, sheetReceipt(call, strconv.FormatInt(input.WorkspaceID, 10)))
	}
	if response.status != http.StatusOK {
		return workspaceDeleteBranch(DeleteWorkspaceBranchProviderRejected, sdkgo.FailureProviderRejection, "Smartsheet rejected workspace deletion")
	}
	if !validSuccessEnvelope(response.body) {
		return workspaceDeleteBranch(DeleteWorkspaceBranchInvalidResponse, sdkgo.FailureProtocol, "Smartsheet returned invalid deletion confirmation")
	}
	return sdkgo.NewMutationBranch(DeleteWorkspaceBranchDeleted, DeleteWorkspaceOutput{WorkspaceID: input.WorkspaceID}, nil, sheetReceipt(call, strconv.FormatInt(input.WorkspaceID, 10)))
}

func workspacePath(workspaceID int64) string {
	return "/workspaces/" + strconv.FormatInt(workspaceID, 10)
}

func decodeWorkspaceSummary(body []byte) (WorkspaceSummary, error) {
	var workspace WorkspaceSummary
	if err := json.Unmarshal(body, &workspace); err != nil || !validSmartsheetID(workspace.ID) || workspace.Name == "" {
		return WorkspaceSummary{}, errors.New("invalid Smartsheet workspace")
	}
	return workspace, nil
}

func decodeWorkspaceResult(body []byte) (WorkspaceSummary, error) {
	var envelope struct {
		ResultCode *int            `json:"resultCode"`
		Message    string          `json:"message"`
		Result     json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.ResultCode == nil || *envelope.ResultCode != 0 || envelope.Message != "SUCCESS" {
		return WorkspaceSummary{}, errors.New("invalid Smartsheet workspace result")
	}
	return decodeWorkspaceSummary(envelope.Result)
}

func decodeWorkspacePage(body []byte) (WorkspacePage, error) {
	var response struct {
		Data    []WorkspaceSummary `json:"data"`
		LastKey string             `json:"lastKey"`
	}
	if err := json.Unmarshal(body, &response); err != nil || response.Data == nil {
		return WorkspacePage{}, errors.New("invalid Smartsheet workspace page")
	}
	for _, workspace := range response.Data {
		if !validSmartsheetID(workspace.ID) || workspace.Name == "" {
			return WorkspacePage{}, errors.New("invalid Smartsheet workspace")
		}
	}
	return WorkspacePage{Workspaces: response.Data, LastKey: response.LastKey}, nil
}

func workspaceMutationBranch(branch sdkgo.BranchID, operation string, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[WorkspaceSummary] {
	failure := sheetFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, WorkspaceSummary{}, &failure, sdkgo.Receipt{})
}
func workspaceDeleteBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[DeleteWorkspaceOutput] {
	failure := sheetFailure("deleteWorkspace", kind, message)
	return sdkgo.NewMutationBranch(branch, DeleteWorkspaceOutput{}, &failure, sdkgo.Receipt{})
}
