// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package onedrive

import (
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const getFileOperationID = "getFile"

// GetFileInput identifies one drive item.
type GetFileInput struct {
	// DriveID is the drive that holds the item; blank means the signed-in
	// user's OneDrive and is invalid for app-only connections.
	DriveID string `json:"driveId,omitempty"`
	// ItemID is the drive item ID, such as a SearchFiles result ID, or root for the drive root.
	ItemID string `json:"itemId"`
}

// GetFileOperation implements the getFile connector operation.
type GetFileOperation struct{ client *Client }

var getFileReadBranches = readBranches{
	operationID: getFileOperationID, notFound: GetFileBranchNotFound, providerRejected: GetFileBranchProviderRejected,
	invalidResponse: GetFileBranchInvalidResponse, defect: GetFileBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (GetFileOperation) Definition() sdkgo.QueryDefinition { return GetFileDefinition }

// Invoke sends one Graph driveItem metadata request and classifies its attempt.
// A malformed drive or item ID selects defect without a provider request.
func (operation GetFileOperation) Invoke(call sdkgo.Call, input GetFileInput) sdkgo.QueryAttempt[DriveItem] {
	client := operation.client
	if err := validateDriveAndFolderIDs(input.DriveID, input.ItemID); err != nil || input.ItemID == "" {
		return sdkgo.NewQueryBranch(GetFileBranchDefect, DriveItem{}, graphFailurePointer(sdkgo.FailureValidation, getFileOperationID, "itemId must be a Microsoft Graph item ID or root, and driveId a drive ID or blank"), sdkgo.Receipt{})
	}
	credential, failure := client.resolveCredential(call, getFileOperationID, input.DriveID)
	if failure != nil {
		return sdkgo.NewQueryBranch(GetFileBranchDefect, DriveItem{}, failure, sdkgo.Receipt{})
	}
	response, outcome := client.sendRead(call, &credential, getFileReadBranches, graphRequest{
		method: http.MethodGet, target: client.itemURL(input.DriveID, input.ItemID) + "?" + buildODataQuery([2]string{"$select", itemFields}),
		responseLimit: client.maxResponseBytes,
	})
	if outcome != nil {
		return queryAttemptFromReadOutcome[DriveItem](outcome)
	}
	receipt := client.receipt(call, response.requestID, input.ItemID)
	// The returned ID can differ: Graph also accepts a SharePoint list item's unique ID here.
	item, err := decodeItemResponse(response.body)
	if err != nil {
		return sdkgo.NewQueryBranch(GetFileBranchInvalidResponse, DriveItem{}, graphFailurePointer(sdkgo.FailureProtocol, getFileOperationID, "provider returned an invalid item response"), receipt)
	}
	receipt.ProviderObjectID = item.ID
	return sdkgo.NewQueryBranch(GetFileBranchFound, item, nil, receipt)
}
