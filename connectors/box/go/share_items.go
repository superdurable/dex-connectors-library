// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package box

import "github.com/superdurable/dex-connectors-library/sdkgo"

// ShareBoxFileInput selects a file and desired shared link access.
type ShareBoxFileInput struct {
	// FileID is the decimal ID from Box Web's /files/<ID> URL.
	FileID string `json:"fileId"`
	// Access is open, company, or collaborators.
	Access string `json:"access"`
}

// ShareBoxFolderInput selects a folder and desired shared link access.
type ShareBoxFolderInput struct {
	// FolderID is the decimal ID from Box Web's /folders/<ID> URL.
	FolderID string `json:"folderId"`
	// Access is open, company, or collaborators.
	Access string `json:"access"`
}

// ShareFileOperation creates or updates a file's shared link.
type ShareFileOperation struct{ client *Client }

// ShareFolderOperation creates or updates a folder's shared link.
type ShareFolderOperation struct{ client *Client }

// ShareFile binds file sharing to this client.
func (client *Client) ShareFile() ShareFileOperation { return ShareFileOperation{client} }

// ShareFolder binds folder sharing to this client.
func (client *Client) ShareFolder() ShareFolderOperation { return ShareFolderOperation{client} }

// Definition returns the generated shareFile contract.
func (ShareFileOperation) Definition() sdkgo.MutationDefinition { return ShareFileDefinition }

// Definition returns the generated shareFolder contract.
func (ShareFolderOperation) Definition() sdkgo.MutationDefinition { return ShareFolderDefinition }

// IdempotencyKey correlates absolute shared link updates.
func (ShareFileOperation) IdempotencyKey(callID sdkgo.CallID, _ ShareBoxFileInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates absolute shared link updates.
func (ShareFolderOperation) IdempotencyKey(callID sdkgo.CallID, _ ShareBoxFolderInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates or replaces file shared link access.
func (operation ShareFileOperation) Invoke(call sdkgo.Call, input ShareBoxFileInput) sdkgo.MutationAttempt[UpdatedBoxItem] {
	if !validBoxItemID(input.FileID, false) || input.Access == "" || !validBoxUpdate("", "", input.Access) {
		return boxUpdateFailure("shareFile", ShareFileBranchDefect, sdkgo.FailureValidation, "file ID or shared link access is invalid")
	}
	return runBoxUpdate(operation.client, call, "shareFile", "file", input.FileID, "", "", input.Access, ShareFileBranchShared, ShareFileBranchNotFound, ShareFileBranchProviderRejected, ShareFileBranchInvalidResponse, ShareFileBranchDefect)
}

// Invoke creates or replaces folder shared link access.
func (operation ShareFolderOperation) Invoke(call sdkgo.Call, input ShareBoxFolderInput) sdkgo.MutationAttempt[UpdatedBoxItem] {
	if !validBoxItemID(input.FolderID, false) || input.Access == "" || !validBoxUpdate("", "", input.Access) {
		return boxUpdateFailure("shareFolder", ShareFolderBranchDefect, sdkgo.FailureValidation, "folder ID or shared link access is invalid")
	}
	return runBoxUpdate(operation.client, call, "shareFolder", "folder", input.FolderID, "", "", input.Access, ShareFolderBranchShared, ShareFolderBranchNotFound, ShareFolderBranchProviderRejected, ShareFolderBranchInvalidResponse, ShareFolderBranchDefect)
}
