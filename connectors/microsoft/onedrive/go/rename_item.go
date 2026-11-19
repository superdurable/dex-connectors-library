// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package onedrive

import "github.com/superdurable/dex-connectors-library/sdkgo"

const renameItemOperationID = "renameItem"

// RenameItemInput sets the absolute name of one file or folder.
type RenameItemInput struct {
	// DriveID is the drive ID; blank selects the signed-in user's OneDrive.
	DriveID string `json:"driveId,omitempty"`
	// ItemID is the file or folder ID; root cannot be renamed.
	ItemID string `json:"itemId"`
	// Name is the desired file or folder name.
	Name string `json:"name"`
}

// RenameItemOutput holds the renamed item's metadata.
type RenameItemOutput struct {
	// Item is the renamed item.
	Item DriveItem `json:"item"`
}

// RenameItemOperation implements the renameItem mutation.
type RenameItemOperation struct{ client *Client }

// Definition returns the mutation definition.
func (RenameItemOperation) Definition() sdkgo.MutationDefinition { return RenameItemDefinition }

// IdempotencyKey derives the receipt key from the stable call ID.
func (RenameItemOperation) IdempotencyKey(callID sdkgo.CallID, _ RenameItemInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sets the item's name absolutely, so an unknown result can repeat safely.
func (operation RenameItemOperation) Invoke(call sdkgo.Call, input RenameItemInput) sdkgo.MutationAttempt[RenameItemOutput] {
	if validateDriveAndFolderIDs(input.DriveID, input.ItemID) != nil || input.ItemID == "" || input.ItemID == rootFolderAlias || validateItemName(input.Name, false) != nil {
		return sdkgo.NewMutationBranch(RenameItemBranchDefect, RenameItemOutput{}, graphFailurePointer(sdkgo.FailureValidation, renameItemOperationID, "item ID or name is invalid"), sdkgo.Receipt{})
	}
	return invokeItemPatch(operation.client, call, input.DriveID, input.ItemID, renameItemOperationID,
		map[string]string{"name": input.Name}, itemPatchBranches{RenameItemBranchRenamed, RenameItemBranchNotFound, RenameItemBranchProviderRejected, RenameItemBranchDefect},
		func(item DriveItem) RenameItemOutput { return RenameItemOutput{Item: item} })
}
