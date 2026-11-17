// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package onedrive

import "github.com/superdurable/dex-connectors-library/sdkgo"

const moveItemOperationID = "moveItem"

// MoveItemInput sets the absolute parent folder of one file or folder.
type MoveItemInput struct {
	// DriveID is the drive ID; blank selects the signed-in user's OneDrive.
	DriveID string `json:"driveId,omitempty"`
	// ItemID is the file or folder ID; root cannot be moved.
	ItemID string `json:"itemId"`
	// ParentFolderID is the destination folder ID, or root.
	ParentFolderID string `json:"parentFolderId"`
}

// MoveItemOutput holds the moved item's metadata.
type MoveItemOutput struct {
	// Item is the moved item.
	Item DriveItem `json:"item"`
}

// MoveItemOperation implements the moveItem mutation.
type MoveItemOperation struct{ client *Client }

// Definition returns the mutation definition.
func (MoveItemOperation) Definition() sdkgo.MutationDefinition { return MoveItemDefinition }

// IdempotencyKey derives the receipt key from the stable call ID.
func (MoveItemOperation) IdempotencyKey(callID sdkgo.CallID, _ MoveItemInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sets the item's parent absolutely, so an unknown result can repeat safely.
func (operation MoveItemOperation) Invoke(call sdkgo.Call, input MoveItemInput) sdkgo.MutationAttempt[MoveItemOutput] {
	if validateDriveAndFolderIDs(input.DriveID, input.ItemID) != nil || input.ItemID == "" || input.ItemID == rootFolderAlias ||
		validateDriveAndFolderIDs(input.DriveID, input.ParentFolderID) != nil || input.ParentFolderID == "" || input.ItemID == input.ParentFolderID {
		return sdkgo.NewMutationBranch(MoveItemBranchDefect, MoveItemOutput{}, graphFailurePointer(sdkgo.FailureValidation, moveItemOperationID, "item ID or destination folder ID is invalid"), sdkgo.Receipt{})
	}
	return invokeItemPatch(operation.client, call, input.DriveID, input.ItemID, moveItemOperationID,
		map[string]any{"parentReference": map[string]string{"id": input.ParentFolderID}},
		itemPatchBranches{MoveItemBranchMoved, MoveItemBranchNotFound, MoveItemBranchProviderRejected, MoveItemBranchDefect},
		func(item DriveItem) MoveItemOutput { return MoveItemOutput{Item: item} })
}
