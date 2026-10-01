// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package drive

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	uploadFileOperationID = "uploadFile"
	// idempotencyAppPropertyKey names the private app property that records the Step's idempotency key.
	idempotencyAppPropertyKey = "dexIdempotencyKey"
	// maxIdempotencyKeyBytes keeps the app property within Google's 124-byte key-plus-value limit.
	maxIdempotencyKeyBytes    = 100
	idempotencyLookupPageSize = 10
)

// UploadFileInput describes one new Drive file. Set at most one of TextContent
// and ByteContent; leaving both empty creates an empty file.
type UploadFileInput struct {
	// Name is the new file's title; it must not be blank.
	Name string `json:"name"`
	// ParentFolderID is the destination folder ID; blank creates the file in the My Drive root.
	ParentFolderID string `json:"parentFolderId,omitempty"`
	// MimeType is the content's lowercase MIME type, such as text/plain or
	// text/csv. Google Workspace types are rejected, so Drive never converts the upload.
	MimeType string `json:"mimeType"`
	// TextContent is UTF-8 text content.
	TextContent string `json:"textContent,omitempty"`
	// ByteContent is binary content, encoded as base64 in JSON.
	ByteContent []byte `json:"byteContent,omitempty"`
}

// UploadFileOutput identifies the created file.
type UploadFileOutput struct {
	// File is the created file's metadata.
	File File `json:"file"`
	// IsFromEarlierAttempt reports that an earlier attempt of the same Step
	// execution had already created File, so this attempt uploaded nothing.
	IsFromEarlierAttempt bool `json:"isFromEarlierAttempt,omitempty"`
}

// UploadFileOperation implements the uploadFile connector operation.
type UploadFileOperation struct{ client *Client }

type fileListResponse struct {
	Files            []driveFileResource `json:"files"`
	IncompleteSearch bool                `json:"incompleteSearch"`
}

var uploadFileLookupBranches = readBranches{
	operationID: uploadFileOperationID, notFound: UploadFileBranchProviderRejected,
	tooLarge: UploadFileBranchInvalidResponse, providerRejected: UploadFileBranchProviderRejected,
	invalidResponse: UploadFileBranchInvalidResponse, defect: UploadFileBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (UploadFileOperation) Definition() sdkgo.MutationDefinition { return UploadFileDefinition }

// IdempotencyKey derives the provider key from the stable connector call ID, so
// every attempt of one Step execution shares it.
func (UploadFileOperation) IdempotencyKey(callID sdkgo.CallID, _ UploadFileInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke first looks for a file that an earlier attempt created with the same
// idempotency key and returns it. Otherwise it sends one multipart upload that
// records the key as a private app property. An ambiguous upload outcome
// selects uncertain and is never sent again automatically.
func (operation UploadFileOperation) Invoke(call sdkgo.Call, input UploadFileInput) sdkgo.MutationAttempt[UploadFileOutput] {
	client := operation.client
	content, err := validateUploadFileInput(input, client.maxUploadBytes)
	if err != nil {
		return sdkgo.NewMutationBranch(UploadFileBranchDefect, UploadFileOutput{}, driveFailurePointer(sdkgo.FailureValidation, uploadFileOperationID, err.Error()), sdkgo.Receipt{})
	}
	idempotencyKey := string(call.IdempotencyKey)
	if idempotencyKey == "" || len(idempotencyKey) > maxIdempotencyKeyBytes {
		return sdkgo.NewMutationBranch(UploadFileBranchDefect, UploadFileOutput{}, driveFailurePointer(sdkgo.FailureLocalDefect, uploadFileOperationID, "idempotency key is missing or too long"), sdkgo.Receipt{})
	}
	credential, failure := client.resolveCredential(call, uploadFileOperationID)
	if failure != nil {
		return sdkgo.NewMutationBranch(UploadFileBranchDefect, UploadFileOutput{}, failure, sdkgo.Receipt{})
	}
	lookupQuery := url.Values{
		"q": {earlierUploadQuery(idempotencyKey)}, "fields": {"incompleteSearch,files(" + fullFileFields + ")"},
		"pageSize": {strconv.Itoa(idempotencyLookupPageSize)}, "spaces": {"drive"},
	}
	lookupResponse, outcome := client.sendRead(call, &credential, uploadFileLookupBranches, driveRequest{
		method: http.MethodGet, target: client.filesListURL(lookupQuery), responseLimit: client.maxResponseBytes,
	})
	if outcome != nil {
		return mutationAttemptFromReadOutcome[UploadFileOutput](outcome)
	}
	earlierFile, isIncompleteLookup, err := decodeEarlierUpload(lookupResponse.body)
	if err != nil {
		return sdkgo.NewMutationBranch(UploadFileBranchInvalidResponse, UploadFileOutput{}, driveFailurePointer(sdkgo.FailureProtocol, uploadFileOperationID, "provider returned an invalid duplicate-lookup response"), client.receipt(call, lookupResponse.requestID, ""))
	}
	if earlierFile == nil && isIncompleteLookup {
		// An incomplete lookup cannot prove that no earlier attempt created the file.
		return sdkgo.NewMutationRetry[UploadFileOutput](driveFailure(sdkgo.FailureAvailability, uploadFileOperationID, "provider did not complete the duplicate lookup"), 0)
	}
	if earlierFile != nil {
		return sdkgo.NewMutationBranch(UploadFileBranchUploaded, UploadFileOutput{File: *earlierFile, IsFromEarlierAttempt: true}, nil, client.receipt(call, lookupResponse.requestID, earlierFile.ID))
	}
	body, contentType, err := buildMultipartUploadBody(input, content, idempotencyKey)
	if err != nil {
		return sdkgo.NewMutationBranch(UploadFileBranchDefect, UploadFileOutput{}, driveFailurePointer(sdkgo.FailureLocalDefect, uploadFileOperationID, "upload request could not be encoded"), sdkgo.Receipt{})
	}
	response, err := client.sendRequest(call, &credential, driveRequest{
		method: http.MethodPost, target: client.multipartUploadURL(), body: body, contentType: contentType,
		responseLimit: client.maxResponseBytes,
	})
	receipt := client.receipt(call, response.requestID, "")
	var requestErr *driveRequestError
	if errors.As(err, &requestErr) && requestErr.kind == sdkgo.FailureLocalDefect {
		return sdkgo.NewMutationBranch(UploadFileBranchDefect, UploadFileOutput{}, driveFailurePointer(requestErr.kind, uploadFileOperationID, requestErr.message), receipt)
	}
	if err != nil {
		return sdkgo.NewMutationUncertain(UploadFileOutput{}, driveFailure(sdkgo.FailureTransport, uploadFileOperationID, "provider upload outcome is unknown"), receipt)
	}
	reasons := providerhttp.ReadErrorTokens(response.body, googleErrorTokenPointers)
	switch {
	case response.status >= 200 && response.status < 300:
		var resource driveFileResource
		if err := json.Unmarshal(response.body, &resource); err != nil {
			return sdkgo.NewMutationUncertain(UploadFileOutput{}, driveFailure(sdkgo.FailureProtocol, uploadFileOperationID, "provider returned an invalid upload response"), receipt)
		}
		file, err := convertFileResource(resource)
		if err != nil {
			return sdkgo.NewMutationUncertain(UploadFileOutput{}, driveFailure(sdkgo.FailureProtocol, uploadFileOperationID, "provider returned an invalid upload response"), receipt)
		}
		receipt.ProviderObjectID = file.ID
		return sdkgo.NewMutationBranch(UploadFileBranchUploaded, UploadFileOutput{File: file}, nil, receipt)
	case response.status >= 500:
		return sdkgo.NewMutationUncertain(UploadFileOutput{}, driveFailure(sdkgo.FailureAvailability, uploadFileOperationID, "provider upload outcome is unknown"), receipt)
	case isRateLimitStatus(response.status, reasons):
		// Google rejected the request before creating a file; the next attempt looks up again first.
		delay := providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), client.now())
		return sdkgo.NewMutationRetry[UploadFileOutput](driveFailure(sdkgo.FailureRateLimit, uploadFileOperationID, "provider temporarily rejected the upload"), delay)
	default:
		return sdkgo.NewMutationBranch(UploadFileBranchProviderRejected, UploadFileOutput{}, driveFailurePointer(statusFailureKind(response.status, reasons), uploadFileOperationID, "provider rejected the upload"), receipt)
	}
}

// validateUploadFileInput returns the content to upload for valid input.
func validateUploadFileInput(input UploadFileInput, maxUploadBytes int64) ([]byte, error) {
	if strings.TrimSpace(input.Name) == "" {
		return nil, errors.New("name is required")
	}
	if input.ParentFolderID != "" && !isDriveID(input.ParentFolderID) {
		return nil, errors.New("parentFolderId must be a Drive folder ID or root")
	}
	if !isPlainMediaType(input.MimeType) {
		return nil, errors.New("mimeType must be one lowercase type/subtype without parameters")
	}
	if strings.HasPrefix(input.MimeType, googleWorkspaceMimeTypePrefix) {
		return nil, errors.New("mimeType cannot be a Google Workspace type")
	}
	if input.TextContent != "" && len(input.ByteContent) != 0 {
		return nil, errors.New("set textContent or byteContent, not both")
	}
	content := input.ByteContent
	if input.TextContent != "" {
		content = []byte(input.TextContent)
	}
	if int64(len(content)) > maxUploadBytes {
		return nil, fmt.Errorf("content exceeds the configured upload limit of %d bytes", maxUploadBytes)
	}
	return content, nil
}

// earlierUploadQuery finds files, including trashed ones, that carry the idempotency key.
func earlierUploadQuery(idempotencyKey string) string {
	return "appProperties has { key=" + quoteDriveQueryString(idempotencyAppPropertyKey) +
		" and value=" + quoteDriveQueryString(idempotencyKey) + " }"
}

// decodeEarlierUpload returns the earliest-created matching file, or nil, and
// whether Drive reported the lookup as incomplete.
func decodeEarlierUpload(content []byte) (*File, bool, error) {
	var response fileListResponse
	if err := json.Unmarshal(content, &response); err != nil {
		return nil, false, err
	}
	var earliest *File
	for _, resource := range response.Files {
		file, err := convertFileResource(resource)
		if err != nil {
			return nil, false, err
		}
		if earliest == nil || file.CreatedTime.Before(earliest.CreatedTime) {
			earliest = &file
		}
	}
	return earliest, response.IncompleteSearch, nil
}

// buildMultipartUploadBody encodes Drive's multipart/related metadata and media parts.
func buildMultipartUploadBody(input UploadFileInput, content []byte, idempotencyKey string) ([]byte, string, error) {
	metadata := map[string]any{
		"name": input.Name, "mimeType": input.MimeType,
		"appProperties": map[string]string{idempotencyAppPropertyKey: idempotencyKey},
	}
	if input.ParentFolderID != "" {
		metadata["parents"] = []string{input.ParentFolderID}
	}
	encodedMetadata, err := json.Marshal(metadata)
	if err != nil {
		return nil, "", err
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	metadataPart, err := writer.CreatePart(textproto.MIMEHeader{"Content-Type": {"application/json; charset=UTF-8"}})
	if err != nil {
		return nil, "", err
	}
	if _, err := metadataPart.Write(encodedMetadata); err != nil {
		return nil, "", err
	}
	mediaPart, err := writer.CreatePart(textproto.MIMEHeader{"Content-Type": {input.MimeType}})
	if err != nil {
		return nil, "", err
	}
	if _, err := mediaPart.Write(content); err != nil {
		return nil, "", err
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return body.Bytes(), "multipart/related; boundary=" + writer.Boundary(), nil
}
