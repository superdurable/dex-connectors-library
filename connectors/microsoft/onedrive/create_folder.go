// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package onedrive

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const createFolderOperationID = "createFolder"

// CreateFolderInput names one folder to ensure in a parent folder.
type CreateFolderInput struct {
	// DriveID is the drive that holds the parent folder; blank means the
	// signed-in user's OneDrive and is invalid for app-only connections.
	DriveID string `json:"driveId,omitempty"`
	// ParentFolderID is the parent folder's item ID, or root; blank means the drive root.
	ParentFolderID string `json:"parentFolderId,omitempty"`
	// Name is the folder name; it must be a valid OneDrive and SharePoint name and cannot end with a period.
	Name string `json:"name"`
}

// CreateFolderOutput identifies the ensured folder, or the conflicting file for nameConflict.
type CreateFolderOutput struct {
	// Folder is the folder's metadata, or the conflicting file's metadata for nameConflict.
	Folder DriveItem `json:"folder"`
	// IsExistingFolder reports that a folder with the name already existed, such
	// as one an earlier attempt of this Step created, so nothing was created.
	IsExistingFolder bool `json:"isExistingFolder,omitempty"`
}

// CreateFolderOperation implements the createFolder connector operation.
type CreateFolderOperation struct{ client *Client }

var createFolderReadBackBranches = readBranches{
	operationID: createFolderOperationID, notFound: "", providerRejected: CreateFolderBranchProviderRejected,
	invalidResponse: CreateFolderBranchInvalidResponse, defect: CreateFolderBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (CreateFolderOperation) Definition() sdkgo.MutationDefinition { return CreateFolderDefinition }

// IdempotencyKey derives the receipt key from the stable connector call ID.
// Graph has no idempotency key; the fail conflict behavior and read-back make repeats converge.
func (CreateFolderOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateFolderInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke posts the folder with @microsoft.graph.conflictBehavior fail. A
// nameAlreadyExists conflict reads the item at the path back: a folder selects
// created with IsExistingFolder, and a file selects nameConflict. Graph never
// creates a second folder with one name, so every ambiguous outcome returns Retry.
func (operation CreateFolderOperation) Invoke(call sdkgo.Call, input CreateFolderInput) sdkgo.MutationAttempt[CreateFolderOutput] {
	client := operation.client
	if err := validateCreateFolderInput(input); err != nil {
		return sdkgo.NewMutationBranch(CreateFolderBranchDefect, CreateFolderOutput{}, graphFailurePointer(sdkgo.FailureValidation, createFolderOperationID, err.Error()), sdkgo.Receipt{})
	}
	credential, failure := client.resolveCredential(call, createFolderOperationID, input.DriveID)
	if failure != nil {
		return sdkgo.NewMutationBranch(CreateFolderBranchDefect, CreateFolderOutput{}, failure, sdkgo.Receipt{})
	}
	body, err := json.Marshal(map[string]any{"name": input.Name, "folder": map[string]any{}, conflictBehaviorParameter: "fail"})
	if err != nil {
		return sdkgo.NewMutationBranch(CreateFolderBranchDefect, CreateFolderOutput{}, graphFailurePointer(sdkgo.FailureLocalDefect, createFolderOperationID, "folder request could not be encoded"), sdkgo.Receipt{})
	}
	// The create-folder page sets the conflict behavior in the body and the driveItem page in the URL; send both.
	response, err := client.sendRequest(call, &credential, graphRequest{
		method: http.MethodPost, body: body, contentType: "application/json", responseLimit: client.maxResponseBytes,
		target: client.itemURL(input.DriveID, input.ParentFolderID) + "/children?" + buildODataQuery([2]string{conflictBehaviorParameter, "fail"}),
	})
	receipt := client.receipt(call, response.requestID, "")
	var requestErr *graphRequestError
	if errors.As(err, &requestErr) && requestErr.kind == sdkgo.FailureLocalDefect {
		return sdkgo.NewMutationBranch(CreateFolderBranchDefect, CreateFolderOutput{}, graphFailurePointer(requestErr.kind, createFolderOperationID, requestErr.message), receipt)
	}
	if err != nil {
		// A lost response may follow a created folder; the next attempt finds it by name.
		return sdkgo.NewMutationRetry[CreateFolderOutput](graphFailure(sdkgo.FailureTransport, createFolderOperationID, "provider folder outcome is unknown"), 0)
	}
	codes := providerhttp.ReadErrorTokens(response.body, graphErrorTokenPointers)
	switch {
	case response.status >= 200 && response.status < 300:
		folder, err := decodeItemResponse(response.body)
		if err != nil || !folder.IsFolder {
			return sdkgo.NewMutationRetry[CreateFolderOutput](graphFailure(sdkgo.FailureProtocol, createFolderOperationID, "provider returned an invalid folder response"), 0)
		}
		receipt.ProviderObjectID = folder.ID
		return sdkgo.NewMutationBranch(CreateFolderBranchCreated, CreateFolderOutput{Folder: folder}, nil, receipt)
	case response.status == http.StatusConflict && hasCode(codes, "nameAlreadyExists"):
		return operation.readBackExistingFolder(call, &credential, input)
	case isRetryableStatus(response.status, codes):
		delay := providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), client.now())
		return sdkgo.NewMutationRetry[CreateFolderOutput](graphFailure(statusFailureKind(response.status, codes), createFolderOperationID, "provider temporarily rejected the folder creation"), delay)
	default:
		return sdkgo.NewMutationBranch(CreateFolderBranchProviderRejected, CreateFolderOutput{}, graphFailurePointer(statusFailureKind(response.status, codes), createFolderOperationID, "provider rejected the folder creation"), receipt)
	}
}

// readBackExistingFolder decides a nameAlreadyExists conflict from the item now at the path.
func (operation CreateFolderOperation) readBackExistingFolder(call sdkgo.Call, credential *Credentials, input CreateFolderInput) sdkgo.MutationAttempt[CreateFolderOutput] {
	client := operation.client
	response, outcome := client.sendRead(call, credential, createFolderReadBackBranches, graphRequest{
		method: http.MethodGet, target: client.childPathURL(input.DriveID, input.ParentFolderID, input.Name) + "?" + buildODataQuery([2]string{"$select", itemFields}),
		responseLimit: client.maxResponseBytes,
	})
	if outcome != nil && outcome.branch == "" && outcome.failure.Kind == sdkgo.FailureNotFound {
		// A conflicting creation may still be committing, so the next attempt posts again.
		return sdkgo.NewMutationRetry[CreateFolderOutput](graphFailure(sdkgo.FailureConflict, createFolderOperationID, "conflicting item is not readable yet"), 0)
	}
	if outcome != nil {
		return mutationAttemptFromReadOutcome[CreateFolderOutput](outcome)
	}
	receipt := client.receipt(call, response.requestID, "")
	item, err := decodeItemResponse(response.body)
	if err != nil {
		return sdkgo.NewMutationBranch(CreateFolderBranchInvalidResponse, CreateFolderOutput{}, graphFailurePointer(sdkgo.FailureProtocol, createFolderOperationID, "provider returned an invalid item response"), receipt)
	}
	receipt.ProviderObjectID = item.ID
	if !item.IsFolder {
		return sdkgo.NewMutationBranch(CreateFolderBranchNameConflict, CreateFolderOutput{Folder: item}, graphFailurePointer(sdkgo.FailureConflict, createFolderOperationID, "a file already has this name"), receipt)
	}
	return sdkgo.NewMutationBranch(CreateFolderBranchCreated, CreateFolderOutput{Folder: item, IsExistingFolder: true}, nil, receipt)
}

func validateCreateFolderInput(input CreateFolderInput) error {
	if err := validateDriveAndFolderIDs(input.DriveID, input.ParentFolderID); err != nil {
		return err
	}
	return validateItemName(input.Name, true)
}
