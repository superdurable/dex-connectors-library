// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package drive

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptrace"
	"net/textproto"
	"strings"
	"sync/atomic"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
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

type uploadSendAttempt struct {
	client            *Client
	credential        Credentials
	body              []byte
	contentType       string
	response          uploadHTTPResponse
	hasConnectFailure atomic.Bool
	hasConnection     atomic.Bool
}

type uploadHTTPResponse struct {
	status    int
	header    http.Header
	body      []byte
	requestID string
	readErr   error
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

// Invoke fences one upload per attempt and reads back ambiguous outcomes by the
// private app property. An unconfirmed possibly accepted upload remains uncertain.
func (operation UploadFileOperation) Invoke(call sdkgo.Call, input UploadFileInput) sdkgo.MutationAttempt[UploadFileOutput] {
	client := operation.client
	fence, err := singledispatch.NewFence(call, UploadFileDefinition.StepDefaults.ExecuteMethodTimeout)
	if err != nil {
		return sdkgo.NewMutationBranch(UploadFileBranchDefect, UploadFileOutput{}, driveFailurePointer(sdkgo.FailureLocalDefect, uploadFileOperationID, "single-dispatch fence could not be created"), sdkgo.Receipt{})
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return uploadUncertain(call, client, "upload checkpoint cannot be trusted")
	}
	if decision.Kind == singledispatch.ReadBackRequired && decision.Marker.CallID != "" && decision.Marker.CallID != call.ID {
		return uploadUncertain(call, client, "upload checkpoint belongs to another call")
	}
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
		if decision.Kind == singledispatch.SendAllowed {
			return retryUploadBeforeSend(fence, *failure, 0)
		}
		return sdkgo.NewMutationRetry[UploadFileOutput](*failure, 0)
	}
	if decision.Kind == singledispatch.ReadBackRequired && decision.Marker.ProviderID != "" {
		file, response, outcome := client.readBackUploadByID(call, &credential, decision.Marker.ProviderID, idempotencyKey)
		if outcome != nil {
			if outcome.branch == "" {
				return sdkgo.NewMutationRetry[UploadFileOutput](outcome.failure, outcome.retryAfter)
			}
			return uploadUncertain(call, client, "upload ID read-back could not confirm the outcome")
		}
		if file == nil {
			return uploadUncertain(call, client, "upload ID read-back found no matching file")
		}
		return sdkgo.NewMutationBranch(UploadFileBranchUploaded, UploadFileOutput{File: *file, IsFromEarlierAttempt: true}, nil, client.receipt(call, response.requestID, file.ID))
	}
	earlierFile, lookupResponse, lookupOutcome, isIncompleteLookup := client.lookupEarlierUpload(call, &credential, idempotencyKey)
	if lookupOutcome != nil {
		if decision.Kind == singledispatch.SendAllowed && lookupOutcome.branch == "" {
			return retryUploadBeforeSend(fence, lookupOutcome.failure, lookupOutcome.retryAfter)
		}
		if decision.Kind == singledispatch.ReadBackRequired {
			if lookupOutcome.branch == "" {
				return sdkgo.NewMutationRetry[UploadFileOutput](lookupOutcome.failure, lookupOutcome.retryAfter)
			}
			return uploadUncertain(call, client, "upload read-back could not confirm the outcome")
		}
		return mutationAttemptFromReadOutcome[UploadFileOutput](lookupOutcome)
	}
	if earlierFile != nil {
		return sdkgo.NewMutationBranch(UploadFileBranchUploaded, UploadFileOutput{File: *earlierFile, IsFromEarlierAttempt: true}, nil, client.receipt(call, lookupResponse.requestID, earlierFile.ID))
	}
	if isIncompleteLookup {
		failure := driveFailure(sdkgo.FailureAvailability, uploadFileOperationID, "provider did not complete the duplicate lookup")
		if decision.Kind == singledispatch.SendAllowed {
			return retryUploadBeforeSend(fence, failure, 0)
		}
		return sdkgo.NewMutationRetry[UploadFileOutput](failure, 0)
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return uploadUncertain(call, client, "prior upload cannot be confirmed by read-back")
	}
	body, contentType, err := buildMultipartUploadBody(input, content, idempotencyKey)
	if err != nil {
		return sdkgo.NewMutationBranch(UploadFileBranchDefect, UploadFileOutput{}, driveFailurePointer(sdkgo.FailureLocalDefect, uploadFileOperationID, "upload request could not be encoded"), sdkgo.Receipt{})
	}
	attempt := &uploadSendAttempt{client: client, credential: credential, body: body, contentType: contentType}
	dispatchErr := fence.Dispatch(attempt.send)
	if errors.Is(dispatchErr, singledispatch.ErrCheckpoint) || errors.Is(dispatchErr, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[UploadFileOutput](driveFailure(sdkgo.FailureAvailability, uploadFileOperationID, "upload was not sent before the attempt deadline"), 0)
	}
	response := attempt.response
	receipt := client.receipt(call, response.requestID, "")
	if dispatchErr != nil && attempt.isProvablyUndispatched() {
		return retryUploadAfterRejection(fence, sdkgo.FailureTransport, "upload failed before a connection was obtained", 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		failureKind := statusFailureKind(response.status, nil)
		delay := providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), client.now())
		return retryUploadAfterRejection(fence, failureKind, "provider returned a retryable upload status", delay)
	}
	if dispatchErr != nil || response.status >= 500 || response.status == http.StatusConflict ||
		(response.status >= 200 && response.status < 300 && response.readErr != nil) {
		return client.reconcileUpload(call, &credential, idempotencyKey, response.requestID)
	}
	reasons := providerhttp.ReadErrorTokens(response.body, googleErrorTokenPointers)
	if response.status == http.StatusUnauthorized {
		if client.refreshRejectedUploadCredential(call) == nil {
			return retryUploadAfterRejection(fence, sdkgo.FailureAuthentication, "provider rejected the upload credential", 0)
		}
		return sdkgo.NewMutationBranch(UploadFileBranchProviderRejected, UploadFileOutput{}, driveFailurePointer(sdkgo.FailureAuthentication, uploadFileOperationID, "provider rejected the upload credential"), receipt)
	}
	if isRateLimitStatus(response.status, reasons) {
		delay := providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), client.now())
		return retryUploadAfterRejection(fence, sdkgo.FailureRateLimit, "provider returned a retryable upload status", delay)
	}
	if response.status >= 200 && response.status < 300 {
		var resource driveFileResource
		if json.Unmarshal(response.body, &resource) != nil {
			return client.reconcileUpload(call, &credential, idempotencyKey, response.requestID)
		}
		file, err := convertFileResource(resource)
		if err != nil {
			return client.reconcileUpload(call, &credential, idempotencyKey, response.requestID)
		}
		// The accepted result is known even if its extra read-back checkpoint is lost.
		_ = fence.RecordProviderID(file.ID)
		receipt.ProviderObjectID = file.ID
		return sdkgo.NewMutationBranch(UploadFileBranchUploaded, UploadFileOutput{File: file}, nil, receipt)
	}
	return sdkgo.NewMutationBranch(UploadFileBranchProviderRejected, UploadFileOutput{}, driveFailurePointer(statusFailureKind(response.status, reasons), uploadFileOperationID, "provider rejected the upload"), receipt)
}

func retryUploadBeforeSend(fence *singledispatch.Fence, failure sdkgo.Failure, delay time.Duration) sdkgo.MutationAttempt[UploadFileOutput] {
	if err := fence.RecordNotSent(); err != nil {
		failure = driveFailure(sdkgo.FailureAvailability, uploadFileOperationID, "upload was not sent, but its checkpoint failed")
	}
	return sdkgo.NewMutationRetry[UploadFileOutput](failure, delay)
}

func retryUploadAfterRejection(fence *singledispatch.Fence, kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.MutationAttempt[UploadFileOutput] {
	if err := fence.RecordCanResend(); err != nil {
		return sdkgo.NewMutationRetry[UploadFileOutput](driveFailure(sdkgo.FailureAvailability, uploadFileOperationID, "provider returned a retryable status, but its retry checkpoint failed"), delay)
	}
	return sdkgo.NewMutationRetry[UploadFileOutput](driveFailure(kind, uploadFileOperationID, message), delay)
}

func uploadUncertain(call sdkgo.Call, client *Client, message string) sdkgo.MutationAttempt[UploadFileOutput] {
	return sdkgo.NewMutationUncertain(UploadFileOutput{}, driveFailure(sdkgo.FailureAvailability, uploadFileOperationID, message), client.receipt(call, "", ""))
}

func (attempt *uploadSendAttempt) send(requestContext context.Context) error {
	trace := &httptrace.ClientTrace{
		DNSDone: func(info httptrace.DNSDoneInfo) {
			if info.Err != nil {
				attempt.hasConnectFailure.Store(true)
			}
		},
		ConnectDone: func(_, _ string, err error) {
			if err != nil {
				attempt.hasConnectFailure.Store(true)
			}
		},
		TLSHandshakeDone: func(_ tls.ConnectionState, err error) {
			if err != nil {
				attempt.hasConnectFailure.Store(true)
			}
		},
		GotConn: func(httptrace.GotConnInfo) { attempt.hasConnection.Store(true) },
	}
	requestContext = httptrace.WithClientTrace(requestContext, trace)
	request, err := http.NewRequestWithContext(requestContext, http.MethodPost, attempt.client.multipartUploadURL(), bytes.NewReader(attempt.body))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+attempt.credential.AccessToken.Reveal())
	request.Header.Set("Content-Type", attempt.contentType)
	if err := singledispatch.PrepareHTTPWrite(request); err != nil {
		return err
	}
	httpResponse, err := attempt.client.httpClient.Do(request)
	if err != nil {
		return err
	}
	limit := max(attempt.client.maxResponseBytes, providerhttp.MaxErrorBodyBytes)
	content, readErr := providerhttp.ReadBoundedBody(httpResponse.Body, limit)
	closeErr := httpResponse.Body.Close()
	attempt.response = uploadHTTPResponse{
		status: httpResponse.StatusCode, header: httpResponse.Header, body: content,
		requestID: googleRequestID(httpResponse.Header), readErr: errors.Join(readErr, closeErr),
	}
	if attempt.response.status >= 200 && attempt.response.status < 300 && int64(len(content)) > attempt.client.maxResponseBytes {
		attempt.response.readErr = providerhttp.ErrBodyTooLarge
	}
	if attempt.response.readErr != nil {
		attempt.response.body = nil
	}
	return nil
}

func (attempt *uploadSendAttempt) isProvablyUndispatched() bool {
	return attempt.hasConnectFailure.Load() && !attempt.hasConnection.Load()
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
