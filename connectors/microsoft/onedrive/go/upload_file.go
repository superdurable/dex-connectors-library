// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package onedrive

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const uploadFileOperationID = "uploadFile"

// ConflictBehavior selects what uploadFile does when the parent folder already
// has an item with the requested name.
type ConflictBehavior string

const (
	// ConflictBehaviorFail writes nothing over an existing item: a file with
	// different content or a folder selects alreadyExists, and a file with
	// exactly this content, such as an earlier attempt's, selects uploaded.
	ConflictBehaviorFail ConflictBehavior = "fail"
	// ConflictBehaviorReplace replaces an existing file's content, adding a new
	// version. A repeated attempt writes the same bytes again.
	ConflictBehaviorReplace ConflictBehavior = "replace"
)

// UploadFileInput describes one file written by parent folder and name. Set at
// most one of TextContent and ByteContent; leaving both empty writes an empty file.
type UploadFileInput struct {
	// DriveID is the destination drive; blank means the signed-in user's
	// OneDrive and is invalid for app-only connections.
	DriveID string `json:"driveId,omitempty"`
	// ParentFolderID is the destination folder's item ID, or root; blank means the drive root.
	ParentFolderID string `json:"parentFolderId,omitempty"`
	// Name is the file name, including its extension; it must be a valid OneDrive and SharePoint name.
	Name string `json:"name"`
	// ConflictBehavior is fail or replace and is required. Graph's own rename
	// behavior is not offered, because a repeated attempt would create a second file.
	ConflictBehavior ConflictBehavior `json:"conflictBehavior"`
	// MimeType is the content's lowercase media type, such as text/plain or text/csv.
	MimeType string `json:"mimeType"`
	// TextContent is UTF-8 text content.
	TextContent string `json:"textContent,omitempty"`
	// ByteContent is binary content, encoded as base64 in JSON.
	ByteContent []byte `json:"byteContent,omitempty"`
}

// UploadFileOutput identifies the written file, or the existing item for alreadyExists.
type UploadFileOutput struct {
	// Item is the written file's metadata, or the conflicting item's metadata for alreadyExists.
	Item DriveItem `json:"item"`
	// IsExistingFileIdentical reports that a file with exactly this size and
	// QuickXorHash already existed at the path, such as one an earlier attempt
	// of this Step wrote, so this attempt wrote nothing.
	IsExistingFileIdentical bool `json:"isExistingFileIdentical,omitempty"`
}

// UploadFileOperation implements the uploadFile connector operation.
type UploadFileOperation struct{ client *Client }

// uploadRequest is one validated upload with its precomputed content hash.
type uploadRequest struct {
	input        UploadFileInput
	content      []byte
	quickXorHash string
}

var uploadFileReadBackBranches = readBranches{
	operationID: uploadFileOperationID, notFound: "", providerRejected: UploadFileBranchProviderRejected,
	invalidResponse: UploadFileBranchInvalidResponse, defect: UploadFileBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (UploadFileOperation) Definition() sdkgo.MutationDefinition { return UploadFileDefinition }

// IdempotencyKey derives the receipt key from the stable connector call ID.
// Graph has no idempotency key; the path and conflict behavior make repeats converge.
func (UploadFileOperation) IdempotencyKey(callID sdkgo.CallID, _ UploadFileInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends one PUT by parent folder and name with the requested
// @microsoft.graph.conflictBehavior. A PUT by path never creates a second
// file, so every ambiguous outcome returns Retry. A 409 or 423 reads the item
// at the path back: identical size and QuickXorHash select uploaded with
// IsExistingFileIdentical, and other content selects alreadyExists for fail
// or providerRejected for replace.
func (operation UploadFileOperation) Invoke(call sdkgo.Call, input UploadFileInput) sdkgo.MutationAttempt[UploadFileOutput] {
	client := operation.client
	request, err := validateUploadFileInput(input, client.maxUploadBytes)
	if err != nil {
		return sdkgo.NewMutationBranch(UploadFileBranchDefect, UploadFileOutput{}, graphFailurePointer(sdkgo.FailureValidation, uploadFileOperationID, err.Error()), sdkgo.Receipt{})
	}
	credential, failure := client.resolveCredential(call, uploadFileOperationID, input.DriveID)
	if failure != nil {
		return sdkgo.NewMutationBranch(UploadFileBranchDefect, UploadFileOutput{}, failure, sdkgo.Receipt{})
	}
	target := client.childPathURL(input.DriveID, input.ParentFolderID, input.Name) + ":/content?" +
		buildODataQuery([2]string{conflictBehaviorParameter, string(input.ConflictBehavior)})
	response, err := client.sendRequest(call, &credential, graphRequest{
		method: http.MethodPut, target: target, body: request.content, contentType: input.MimeType,
		responseLimit: client.maxResponseBytes,
	})
	receipt := client.receipt(call, response.requestID, "")
	var requestErr *graphRequestError
	if errors.As(err, &requestErr) && requestErr.kind == sdkgo.FailureLocalDefect {
		return sdkgo.NewMutationBranch(UploadFileBranchDefect, UploadFileOutput{}, graphFailurePointer(requestErr.kind, uploadFileOperationID, requestErr.message), receipt)
	}
	if err != nil {
		// A lost or oversized response may follow a stored file; the repeated PUT converges.
		return sdkgo.NewMutationRetry[UploadFileOutput](graphFailure(sdkgo.FailureTransport, uploadFileOperationID, "provider upload outcome is unknown"), 0)
	}
	codes := providerhttp.ReadErrorTokens(response.body, graphErrorTokenPointers)
	switch {
	case response.status >= 200 && response.status < 300:
		item, err := decodeItemResponse(response.body)
		if err != nil || item.IsFolder {
			return sdkgo.NewMutationRetry[UploadFileOutput](graphFailure(sdkgo.FailureProtocol, uploadFileOperationID, "provider returned an invalid upload response"), 0)
		}
		receipt.ProviderObjectID = item.ID
		return sdkgo.NewMutationBranch(UploadFileBranchUploaded, UploadFileOutput{Item: item}, nil, receipt)
	case response.status == http.StatusConflict || response.status == http.StatusLocked:
		return operation.readBackConflictingFile(call, &credential, request, codes)
	case isRetryableStatus(response.status, codes):
		delay := providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), client.now())
		return sdkgo.NewMutationRetry[UploadFileOutput](graphFailure(statusFailureKind(response.status, codes), uploadFileOperationID, "provider temporarily rejected the upload"), delay)
	default:
		return sdkgo.NewMutationBranch(UploadFileBranchProviderRejected, UploadFileOutput{}, graphFailurePointer(statusFailureKind(response.status, codes), uploadFileOperationID, "provider rejected the upload"), receipt)
	}
}

// readBackConflictingFile decides a 409 or 423 from the item now at the path.
func (operation UploadFileOperation) readBackConflictingFile(
	call sdkgo.Call,
	credential *Credentials,
	request uploadRequest,
	conflictCodes []string,
) sdkgo.MutationAttempt[UploadFileOutput] {
	client := operation.client
	input := request.input
	response, outcome := client.sendRead(call, credential, uploadFileReadBackBranches, graphRequest{
		method: http.MethodGet, target: client.childPathURL(input.DriveID, input.ParentFolderID, input.Name) + "?" + buildODataQuery([2]string{"$select", itemFields}),
		responseLimit: client.maxResponseBytes,
	})
	if outcome != nil && outcome.branch == "" && outcome.failure.Kind == sdkgo.FailureNotFound {
		// A conflicting write may still be committing, so the next attempt tries the PUT again.
		return sdkgo.NewMutationRetry[UploadFileOutput](graphFailure(sdkgo.FailureConflict, uploadFileOperationID, "conflicting item is not readable yet"), 0)
	}
	if outcome != nil {
		return mutationAttemptFromReadOutcome[UploadFileOutput](outcome)
	}
	receipt := client.receipt(call, response.requestID, "")
	item, err := decodeItemResponse(response.body)
	if err != nil {
		return sdkgo.NewMutationBranch(UploadFileBranchInvalidResponse, UploadFileOutput{}, graphFailurePointer(sdkgo.FailureProtocol, uploadFileOperationID, "provider returned an invalid item response"), receipt)
	}
	receipt.ProviderObjectID = item.ID
	if !item.IsFolder && !item.IsPackage && item.QuickXorHash == "" {
		return sdkgo.NewMutationRetry[UploadFileOutput](graphFailure(sdkgo.FailureAvailability, uploadFileOperationID, "provider has not reported the existing file's content hash yet"), 0)
	}
	if isIdenticalFile(item, request) {
		return sdkgo.NewMutationBranch(UploadFileBranchUploaded, UploadFileOutput{Item: item, IsExistingFileIdentical: true}, nil, receipt)
	}
	if input.ConflictBehavior == ConflictBehaviorFail && hasCode(conflictCodes, "nameAlreadyExists") {
		return sdkgo.NewMutationBranch(UploadFileBranchAlreadyExists, UploadFileOutput{Item: item}, graphFailurePointer(sdkgo.FailureConflict, uploadFileOperationID, "an item with this name already exists"), receipt)
	}
	return sdkgo.NewMutationBranch(UploadFileBranchProviderRejected, UploadFileOutput{}, graphFailurePointer(sdkgo.FailureConflict, uploadFileOperationID, "provider rejected the upload because the item is locked or conflicts"), receipt)
}

// validateUploadFileInput returns the content and its QuickXorHash for valid input.
func validateUploadFileInput(input UploadFileInput, maxUploadBytes int64) (uploadRequest, error) {
	if err := validateDriveAndFolderIDs(input.DriveID, input.ParentFolderID); err != nil {
		return uploadRequest{}, err
	}
	if err := validateItemName(input.Name, false); err != nil {
		return uploadRequest{}, err
	}
	switch input.ConflictBehavior {
	case ConflictBehaviorFail, ConflictBehaviorReplace:
	default:
		return uploadRequest{}, errors.New("conflictBehavior must be fail or replace")
	}
	if !isPlainMediaType(input.MimeType) {
		return uploadRequest{}, errors.New("mimeType must be one lowercase type/subtype without parameters")
	}
	if input.TextContent != "" && len(input.ByteContent) != 0 {
		return uploadRequest{}, errors.New("set textContent or byteContent, not both")
	}
	content := input.ByteContent
	if input.TextContent != "" {
		content = []byte(input.TextContent)
	}
	if content == nil {
		content = []byte{}
	}
	if int64(len(content)) > maxUploadBytes {
		return uploadRequest{}, fmt.Errorf("content exceeds the configured upload limit of %s", formatByteCount(maxUploadBytes))
	}
	return uploadRequest{input: input, content: content, quickXorHash: computeQuickXorHash(content)}, nil
}

// isIdenticalFile reports whether item is a file whose size and QuickXorHash match the upload.
func isIdenticalFile(item DriveItem, request uploadRequest) bool {
	return !item.IsFolder && !item.IsPackage && strings.EqualFold(item.Name, request.input.Name) &&
		item.SizeBytes == int64(len(request.content)) && item.QuickXorHash == request.quickXorHash
}

// mutationAttemptFromReadOutcome converts a failed read into a Mutation Retry or branch.
func mutationAttemptFromReadOutcome[T any](outcome *readOutcome) sdkgo.MutationAttempt[T] {
	if outcome.branch == "" {
		return sdkgo.NewMutationRetry[T](outcome.failure, outcome.retryAfter)
	}
	var zero T
	failure := outcome.failure
	return sdkgo.NewMutationBranch(outcome.branch, zero, &failure, outcome.receipt)
}
