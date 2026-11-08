// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package dropbox

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// UploadFileInput replaces one Dropbox path with Base64-decoded content.
type UploadFileInput struct {
	// Path is the absolute destination path, including the file name.
	Path string `json:"path"`
	// ContentBase64 contains the complete replacement file content.
	ContentBase64 string `json:"contentBase64"`
}

// UploadedFile identifies the stored replacement without returning its content.
type UploadedFile struct {
	// File is the stored Dropbox file identity.
	File EntryMetadata `json:"file"`
}

// UploadFileOperation overwrites one bounded Dropbox file.
type UploadFileOperation struct{ client *Client }

// UploadFile binds bounded replacement uploads to this client.
func (client *Client) UploadFile() UploadFileOperation { return UploadFileOperation{client} }

// Definition returns the generated upload Mutation contract.
func (UploadFileOperation) Definition() sdkgo.MutationDefinition { return UploadFileDefinition }

// IdempotencyKey records stable call identity; Dropbox uses path overwrite instead of a request key.
func (UploadFileOperation) IdempotencyKey(callID sdkgo.CallID, _ UploadFileInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces a path with the same bytes on every retry.
func (operation UploadFileOperation) Invoke(call sdkgo.Call, input UploadFileInput) sdkgo.MutationAttempt[UploadedFile] {
	if !strings.HasPrefix(input.Path, "/") || !validDropboxPath(input.Path, false) {
		return uploadFailure(UploadFileBranchDefect, sdkgo.FailureValidation, "upload path must be absolute")
	}
	if int64(base64.StdEncoding.DecodedLen(len(input.ContentBase64))) > operation.client.maxUploadBytes {
		return uploadFailure(UploadFileBranchDefect, sdkgo.FailureValidation, "upload exceeds the configured byte limit")
	}
	content, err := base64.StdEncoding.Strict().DecodeString(input.ContentBase64)
	if err != nil || int64(len(content)) > operation.client.maxUploadBytes {
		return uploadFailure(UploadFileBranchDefect, sdkgo.FailureValidation, "upload content must be bounded Base64")
	}
	response := operation.client.upload(call, input.Path, content)
	if errors.Is(response.err, errCredential) {
		return uploadFailure(UploadFileBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRefresh) {
		return uploadRetry(sdkgo.FailureAvailability, "Dropbox credentials could not be refreshed", 0)
	}
	if errors.Is(response.err, errRequest) {
		return uploadRetry(sdkgo.FailureTransport, "Dropbox upload request failed", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return uploadRetry(sdkgo.FailureRateLimit, "Dropbox rate limited the upload", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return uploadRetry(sdkgo.FailureAvailability, "Dropbox upload is temporarily unavailable", response.retryAfter)
	}
	if response.status != http.StatusOK {
		return uploadFailure(UploadFileBranchProviderRejected, sdkgo.FailureProviderRejection, "Dropbox rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return uploadFailure(UploadFileBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Dropbox upload metadata exceeded the configured limit")
	}
	if response.err != nil {
		return uploadRetry(sdkgo.FailureTransport, "Dropbox upload response was interrupted", 0)
	}
	var wire struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		PathDisplay string `json:"path_display"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.ID == "" || wire.Name == "" || wire.PathDisplay == "" {
		return uploadFailure(UploadFileBranchInvalidResponse, sdkgo.FailureProtocol, "Dropbox returned invalid upload metadata")
	}
	output := UploadedFile{File: EntryMetadata{ID: wire.ID, Name: wire.Name, PathDisplay: wire.PathDisplay, Kind: "file"}}
	return sdkgo.NewMutationBranch(UploadFileBranchUploaded, output, nil, sdkgo.Receipt{})
}

func uploadFailure(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[UploadedFile] {
	failure := sdkgo.Failure{Kind: kind, Provider: "dropbox", Operation: "uploadFile", Message: message}
	return sdkgo.NewMutationBranch(branch, UploadedFile{}, &failure, sdkgo.Receipt{})
}

func uploadRetry(kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[UploadedFile] {
	failure := sdkgo.Failure{Kind: kind, Provider: "dropbox", Operation: "uploadFile", Message: message}
	return sdkgo.NewMutationRetry[UploadedFile](failure, retryAfter)
}
