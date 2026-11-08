// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package box

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// UploadFileInput replaces an existing Box file's current content.
type UploadFileInput struct {
	// FileID appears in Box Web's /files/<ID> URL.
	FileID string `json:"fileId"`
	// ContentBase64 holds the complete replacement bytes.
	ContentBase64 string `json:"contentBase64"`
}

// UploadedFile identifies the file with the requested current content.
type UploadedFile struct {
	// FileID is the existing Box file ID.
	FileID string `json:"fileId"`
	// Name is the existing Box file name, preserved during replacement.
	Name string `json:"name"`
	// SHA1 is Box's hexadecimal digest of current content.
	SHA1 string `json:"sha1"`
}

// UploadFileOperation replaces existing file content when it differs.
type UploadFileOperation struct{ client *Client }

// UploadFile binds bounded replacement uploads to this client.
func (client *Client) UploadFile() UploadFileOperation { return UploadFileOperation{client} }

// Definition returns the generated upload Mutation contract.
func (UploadFileOperation) Definition() sdkgo.MutationDefinition { return UploadFileDefinition }

// IdempotencyKey records the stable call identity; Box receives no request key.
func (UploadFileOperation) IdempotencyKey(callID sdkgo.CallID, _ UploadFileInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke compares current content, replaces it once, and reads back ambiguous outcomes.
func (operation UploadFileOperation) Invoke(call sdkgo.Call, input UploadFileInput) sdkgo.MutationAttempt[UploadedFile] {
	if input.FileID == "" || len(input.FileID) > 40 || strings.Trim(input.FileID, "0123456789") != "" {
		return uploadFailure(UploadFileBranchDefect, sdkgo.FailureValidation, "Box file ID must contain only decimal digits")
	}
	if int64(base64.StdEncoding.DecodedLen(len(input.ContentBase64))) > operation.client.maxUploadBytes {
		return uploadFailure(UploadFileBranchDefect, sdkgo.FailureValidation, "Box upload exceeds the configured byte limit")
	}
	content, err := base64.StdEncoding.Strict().DecodeString(input.ContentBase64)
	if err != nil || int64(len(content)) > operation.client.maxUploadBytes {
		return uploadFailure(UploadFileBranchDefect, sdkgo.FailureValidation, "Box upload content must be bounded Base64")
	}
	digestBytes := sha1.Sum(content)
	digest := strings.ToUpper(hex.EncodeToString(digestBytes[:]))
	existingResponse := operation.client.getFileMetadata(call, input.FileID)
	existing, attempt, shouldContinue := classifyInitialFileMetadata(existingResponse, input.FileID)
	if !shouldContinue {
		return attempt
	}
	if existing.matches(input.FileID, digest, int64(len(content))) {
		return uploaded(existing)
	}
	response := operation.client.upload(call, input.FileID, existing.Name, content)
	if errors.Is(response.err, errCredential) && !response.requestSent {
		return uploadFailure(UploadFileBranchDefect, sdkgo.FailureAuthentication, "Box credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRefresh) {
		return uploadRetry(sdkgo.FailureAvailability, "Box credentials could not be refreshed", 0)
	}
	if errors.Is(response.err, errRequest) && !response.requestSent {
		return uploadRetry(sdkgo.FailureTransport, "Box upload did not leave the process", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return uploadRetry(sdkgo.FailureRateLimit, "Box rate limited the upload", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return uploadRetry(sdkgo.FailureAvailability, "Box upload is temporarily unavailable", response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return uploadFailure(UploadFileBranchNotFound, sdkgo.FailureNotFound, "Box did not find the file")
	}
	if response.status != 0 && response.status != http.StatusCreated {
		return uploadFailure(UploadFileBranchProviderRejected, sdkgo.FailureProviderRejection, "Box rejected HTTP "+strconv.Itoa(response.status))
	}
	if response.status == http.StatusCreated && response.err == nil {
		var wire struct {
			Entries []fileMetadata `json:"entries"`
		}
		if json.Unmarshal(response.body, &wire) == nil && len(wire.Entries) == 1 && wire.Entries[0].matches(input.FileID, digest, int64(len(content))) {
			return uploaded(wire.Entries[0])
		}
	}
	readback := operation.client.getFileMetadata(call, input.FileID)
	if readback.status == http.StatusOK && readback.err == nil {
		if metadata, valid := decodeFileMetadata(readback.body, input.FileID); valid && metadata.matches(input.FileID, digest, int64(len(content))) {
			return uploaded(metadata)
		}
	}
	return uploadUncertain("Box may have replaced the file but its current content could not be confirmed")
}

func classifyInitialFileMetadata(response fileResponse, fileID string) (fileMetadata, sdkgo.MutationAttempt[UploadedFile], bool) {
	if errors.Is(response.err, errCredential) {
		return fileMetadata{}, uploadFailure(UploadFileBranchDefect, sdkgo.FailureAuthentication, "Box credentials are unavailable or invalid"), false
	}
	if errors.Is(response.err, errRefresh) {
		return fileMetadata{}, uploadRetry(sdkgo.FailureAvailability, "Box credentials could not be refreshed", 0), false
	}
	if errors.Is(response.err, errRequest) {
		return fileMetadata{}, uploadRetry(sdkgo.FailureTransport, "Box metadata request failed", 0), false
	}
	if response.status == http.StatusTooManyRequests {
		return fileMetadata{}, uploadRetry(sdkgo.FailureRateLimit, "Box rate limited metadata lookup", response.retryAfter), false
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return fileMetadata{}, uploadRetry(sdkgo.FailureAvailability, "Box metadata is temporarily unavailable", response.retryAfter), false
	}
	if response.status == http.StatusNotFound {
		return fileMetadata{}, uploadFailure(UploadFileBranchNotFound, sdkgo.FailureNotFound, "Box did not find the file"), false
	}
	if response.status != http.StatusOK {
		return fileMetadata{}, uploadFailure(UploadFileBranchProviderRejected, sdkgo.FailureProviderRejection, "Box rejected HTTP "+strconv.Itoa(response.status)), false
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return fileMetadata{}, uploadFailure(UploadFileBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Box metadata exceeds the configured limit"), false
	}
	if response.err != nil {
		return fileMetadata{}, uploadRetry(sdkgo.FailureTransport, "Box metadata response was interrupted", 0), false
	}
	metadata, valid := decodeFileMetadata(response.body, fileID)
	if !valid {
		return fileMetadata{}, uploadFailure(UploadFileBranchInvalidResponse, sdkgo.FailureProtocol, "Box returned invalid file metadata"), false
	}
	return metadata, sdkgo.MutationAttempt[UploadedFile]{}, true
}

func uploaded(metadata fileMetadata) sdkgo.MutationAttempt[UploadedFile] {
	return sdkgo.NewMutationBranch(UploadFileBranchUploaded, UploadedFile{FileID: metadata.ID, Name: metadata.Name, SHA1: strings.ToUpper(metadata.SHA1)}, nil, sdkgo.Receipt{})
}

func uploadFailure(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[UploadedFile] {
	failure := sdkgo.Failure{Kind: kind, Provider: "box", Operation: "uploadFile", Message: message}
	return sdkgo.NewMutationBranch(branch, UploadedFile{}, &failure, sdkgo.Receipt{})
}

func uploadRetry(kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[UploadedFile] {
	failure := sdkgo.Failure{Kind: kind, Provider: "box", Operation: "uploadFile", Message: message}
	return sdkgo.NewMutationRetry[UploadedFile](failure, retryAfter)
}

func uploadUncertain(message string) sdkgo.MutationAttempt[UploadedFile] {
	failure := sdkgo.Failure{Kind: sdkgo.FailureTransport, Provider: "box", Operation: "uploadFile", Message: message}
	return sdkgo.NewMutationUncertain(UploadedFile{}, failure, sdkgo.Receipt{})
}
