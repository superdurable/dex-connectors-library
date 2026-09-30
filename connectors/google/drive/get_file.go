// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package drive

import (
	"encoding/json"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const getFileOperationID = "getFile"

// GetFileInput identifies one Drive file.
type GetFileInput struct {
	// FileID is the Drive file ID, such as a SearchFiles result ID.
	FileID string `json:"fileId"`
}

// GetFileOperation implements the getFile connector operation.
type GetFileOperation struct{ client *Client }

var getFileReadBranches = readBranches{
	operationID: getFileOperationID, notFound: GetFileBranchNotFound, tooLarge: GetFileBranchInvalidResponse,
	providerRejected: GetFileBranchProviderRejected, invalidResponse: GetFileBranchInvalidResponse,
	defect: GetFileBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (GetFileOperation) Definition() sdkgo.QueryDefinition { return GetFileDefinition }

// Invoke sends one Drive files.get metadata request and classifies its attempt.
// A malformed file ID selects defect without a provider request.
func (operation GetFileOperation) Invoke(call sdkgo.Call, input GetFileInput) sdkgo.QueryAttempt[File] {
	client := operation.client
	if !isDriveID(input.FileID) {
		return sdkgo.NewQueryBranch(GetFileBranchDefect, File{}, driveFailurePointer(sdkgo.FailureValidation, getFileOperationID, "fileId must be a Drive file ID"), sdkgo.Receipt{})
	}
	credential, failure := client.resolveCredential(call, getFileOperationID)
	if failure != nil {
		return sdkgo.NewQueryBranch(GetFileBranchDefect, File{}, failure, sdkgo.Receipt{})
	}
	response, outcome := client.sendRead(call, &credential, getFileReadBranches, driveRequest{
		method: http.MethodGet, target: client.fileMetadataURL(input.FileID, fullFileFields), responseLimit: client.maxResponseBytes,
	})
	if outcome != nil {
		return queryAttemptFromReadOutcome[File](outcome)
	}
	receipt := client.receipt(call, response.requestID, input.FileID)
	var resource driveFileResource
	if err := json.Unmarshal(response.body, &resource); err != nil {
		return sdkgo.NewQueryBranch(GetFileBranchInvalidResponse, File{}, driveFailurePointer(sdkgo.FailureProtocol, getFileOperationID, "provider returned an invalid file response"), receipt)
	}
	file, err := convertFileResource(resource)
	if err != nil || (file.ID != input.FileID && input.FileID != driveRootFolderAlias) {
		return sdkgo.NewQueryBranch(GetFileBranchInvalidResponse, File{}, driveFailurePointer(sdkgo.FailureProtocol, getFileOperationID, "provider returned an invalid file response"), receipt)
	}
	return sdkgo.NewQueryBranch(GetFileBranchFound, file, nil, receipt)
}
