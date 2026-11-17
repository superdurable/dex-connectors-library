// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package onedrive

import (
	"errors"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const deleteItemOperationID = "deleteItem"

// DeleteItemInput identifies one file or folder to move to the recycle bin.
type DeleteItemInput struct {
	// DriveID is the drive ID; blank selects the signed-in user's OneDrive.
	DriveID string `json:"driveId,omitempty"`
	// ItemID is the file or folder ID; root cannot be deleted.
	ItemID string `json:"itemId"`
}

// DeleteItemOutput identifies the absent item.
type DeleteItemOutput struct {
	// ItemID is the deleted or already absent item ID.
	ItemID string `json:"itemId"`
}

// DeleteItemOperation implements the deleteItem mutation.
type DeleteItemOperation struct{ client *Client }

// Definition returns the mutation definition.
func (DeleteItemOperation) Definition() sdkgo.MutationDefinition { return DeleteItemDefinition }

// IdempotencyKey derives the receipt key from the stable call ID.
func (DeleteItemOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteItemInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke moves the item to the recycle bin; repeated 404 is the desired absent state.
func (operation DeleteItemOperation) Invoke(call sdkgo.Call, input DeleteItemInput) sdkgo.MutationAttempt[DeleteItemOutput] {
	client := operation.client
	if validateDriveAndFolderIDs(input.DriveID, input.ItemID) != nil || input.ItemID == "" || input.ItemID == rootFolderAlias {
		return sdkgo.NewMutationBranch(DeleteItemBranchDefect, DeleteItemOutput{}, graphFailurePointer(sdkgo.FailureValidation, deleteItemOperationID, "itemId must identify a file or folder, and driveId must be a drive ID or blank"), sdkgo.Receipt{})
	}
	credential, failure := client.resolveCredential(call, deleteItemOperationID, input.DriveID)
	if failure != nil {
		return sdkgo.NewMutationBranch(DeleteItemBranchDefect, DeleteItemOutput{}, failure, sdkgo.Receipt{})
	}
	response, err := client.sendRequest(call, &credential, graphRequest{method: http.MethodDelete, target: client.itemURL(input.DriveID, input.ItemID), responseLimit: client.maxResponseBytes})
	receipt := client.receipt(call, response.requestID, input.ItemID)
	var requestError *graphRequestError
	if errors.As(err, &requestError) && requestError.kind == sdkgo.FailureLocalDefect {
		return sdkgo.NewMutationBranch(DeleteItemBranchDefect, DeleteItemOutput{}, graphFailurePointer(sdkgo.FailureLocalDefect, deleteItemOperationID, requestError.message), receipt)
	}
	if err != nil {
		return sdkgo.NewMutationRetry[DeleteItemOutput](graphFailure(sdkgo.FailureTransport, deleteItemOperationID, "item deletion outcome is unknown"), 0)
	}
	if response.status == http.StatusNoContent || response.status == http.StatusNotFound {
		return sdkgo.NewMutationBranch(DeleteItemBranchDeleted, DeleteItemOutput{ItemID: input.ItemID}, nil, receipt)
	}
	codes := providerhttp.ReadErrorTokens(response.body, graphErrorTokenPointers)
	if isRetryableStatus(response.status, codes) {
		return sdkgo.NewMutationRetry[DeleteItemOutput](graphFailure(statusFailureKind(response.status, codes), deleteItemOperationID, "provider temporarily rejected item deletion"), providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), client.now()))
	}
	return sdkgo.NewMutationBranch(DeleteItemBranchProviderRejected, DeleteItemOutput{}, graphFailurePointer(statusFailureKind(response.status, codes), deleteItemOperationID, "provider rejected item deletion"), receipt)
}
