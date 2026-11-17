// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package s3

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	createFolderOperationID = "createFolder"
	deleteFolderOperationID = "deleteFolder"
)

// CreateFolderInput names a zero-byte folder marker.
type CreateFolderInput struct {
	// Bucket is the bucket; blank uses the connection default.
	Bucket string `json:"bucket,omitempty"`
	// Prefix is the folder path; the connector adds a trailing slash when absent.
	Prefix string `json:"prefix"`
}

// CreatedFolder identifies a folder marker stored in S3.
type CreatedFolder struct {
	// Bucket is the bucket.
	Bucket string `json:"bucket"`
	// Prefix ends with a slash.
	Prefix string `json:"prefix"`
}

// CreateFolderOperation implements createFolder.
type CreateFolderOperation struct{ client *Client }

// Definition returns the generated Mutation contract.
func (CreateFolderOperation) Definition() sdkgo.MutationDefinition { return CreateFolderDefinition }

// IdempotencyKey identifies retries of the same absolute marker replacement.
func (CreateFolderOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateFolderInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke stores an empty object at the folder prefix; repeated replacement converges.
func (operation CreateFolderOperation) Invoke(call sdkgo.Call, input CreateFolderInput) sdkgo.MutationAttempt[CreatedFolder] {
	client := operation.client
	location, err := client.folderLocation(input.Bucket, input.Prefix)
	if err != nil {
		return sdkgo.NewMutationBranch(CreateFolderBranchDefect, CreatedFolder{}, defectFailure(createFolderOperationID, err), sdkgo.Receipt{})
	}
	session, cancel, failure := client.startSession(call, createFolderOperationID)
	if failure != nil {
		return sdkgo.NewMutationBranch(CreateFolderBranchDefect, CreatedFolder{}, failure, sdkgo.Receipt{})
	}
	defer cancel()
	value := CreatedFolder{Bucket: location.bucket, Prefix: location.key}
	response, err := client.exchange(session, s3Request{method: http.MethodPut, location: location, body: []byte{}, header: http.Header{"Content-Type": {"application/x-directory"}}})
	if errors.Is(err, errRequestNotBuilt) {
		return sdkgo.NewMutationBranch(CreateFolderBranchDefect, value, failurePointer(createFolderOperationID, sdkgo.FailureLocalDefect, err.Error()), sdkgo.Receipt{})
	}
	if err != nil {
		return sdkgo.NewMutationRetry[CreatedFolder](transportFailure(createFolderOperationID, "folder marker write"), 0)
	}
	defer closeResponseBody(response)
	if isSuccessStatus(response.StatusCode) {
		return sdkgo.NewMutationBranch(CreateFolderBranchCreated, value, nil, client.receipt(call, safeRequestID(response.Header), location))
	}
	classified := classifyErrorResponse(createFolderOperationID, "folder marker write", client.readErrorResponse(response, session.credentials), false)
	return mutationAttemptForFailure(classified, client.receipt(call, classified.requestID, location), CreateFolderBranchProviderRejected, CreateFolderBranchDefect, value)
}

// DeleteFolderInput names a prefix whose current objects should be removed.
type DeleteFolderInput struct {
	// Bucket is the bucket; blank uses the connection default.
	Bucket string `json:"bucket,omitempty"`
	// Prefix is the folder path; the connector adds a trailing slash when absent.
	Prefix string `json:"prefix"`
}

// DeletedFolder identifies a folder prefix after its current objects are absent.
type DeletedFolder struct {
	// Bucket is the bucket.
	Bucket string `json:"bucket"`
	// Prefix ends with a slash.
	Prefix string `json:"prefix"`
}

// DeleteFolderOperation implements recursive deletion of a folder prefix.
type DeleteFolderOperation struct{ client *Client }

// Definition returns the generated Mutation contract.
func (DeleteFolderOperation) Definition() sdkgo.MutationDefinition { return DeleteFolderDefinition }

// IdempotencyKey identifies retries of the same folder deletion.
func (DeleteFolderOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteFolderInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke lists and deletes 100 keys at a time, restarting the listing after each page.
// Retries resume from the remaining keys after any partial deletion.
func (operation DeleteFolderOperation) Invoke(call sdkgo.Call, input DeleteFolderInput) sdkgo.MutationAttempt[DeletedFolder] {
	client := operation.client
	location, err := client.folderLocation(input.Bucket, input.Prefix)
	if err != nil {
		return sdkgo.NewMutationBranch(DeleteFolderBranchDefect, DeletedFolder{}, defectFailure(deleteFolderOperationID, err), sdkgo.Receipt{})
	}
	session, cancel, failure := client.startSession(call, deleteFolderOperationID)
	if failure != nil {
		return sdkgo.NewMutationBranch(DeleteFolderBranchDefect, DeletedFolder{}, failure, sdkgo.Receipt{})
	}
	defer cancel()
	value := DeletedFolder{Bucket: location.bucket, Prefix: location.key}
	for session.context.Err() == nil {
		query := url.Values{"list-type": {"2"}, "encoding-type": {"url"}, "max-keys": {"100"}, "prefix": {location.key}}
		response, requestErr := client.exchange(session, s3Request{method: http.MethodGet, location: objectLocation{bucket: location.bucket}, query: query})
		if errors.Is(requestErr, errRequestNotBuilt) {
			return sdkgo.NewMutationBranch(DeleteFolderBranchDefect, value, failurePointer(deleteFolderOperationID, sdkgo.FailureLocalDefect, requestErr.Error()), sdkgo.Receipt{})
		}
		if requestErr != nil {
			return sdkgo.NewMutationRetry[DeletedFolder](transportFailure(deleteFolderOperationID, "folder listing"), 0)
		}
		if !isSuccessStatus(response.StatusCode) {
			classified := classifyErrorResponse(deleteFolderOperationID, "folder listing", client.readErrorResponse(response, session.credentials), false)
			closeResponseBody(response)
			return mutationAttemptForFailure(classified, client.receipt(call, classified.requestID, location), DeleteFolderBranchProviderRejected, DeleteFolderBranchDefect, value)
		}
		contents, readErr := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
		requestID := safeRequestID(response.Header)
		closeResponseBody(response)
		if readErr != nil || containsCredential(string(contents), session.credentials, true) {
			return sdkgo.NewMutationBranch(DeleteFolderBranchInvalidResponse, value, failurePointer(deleteFolderOperationID, sdkgo.FailureProtocol, "S3 folder listing is incomplete, oversized, or secret-bearing"), client.receipt(call, requestID, location))
		}
		page, parseErr := decodeListBucketResult(contents)
		if parseErr != nil || (len(page.Objects) == 0 && page.IsTruncated) {
			return sdkgo.NewMutationBranch(DeleteFolderBranchInvalidResponse, value, failurePointer(deleteFolderOperationID, sdkgo.FailureProtocol, "S3 returned an invalid folder listing"), client.receipt(call, requestID, location))
		}
		if len(page.Objects) == 0 {
			return sdkgo.NewMutationBranch(DeleteFolderBranchDeleted, value, nil, client.receipt(call, requestID, location))
		}
		for _, object := range page.Objects {
			if !strings.HasPrefix(object.Key, location.key) {
				return sdkgo.NewMutationBranch(DeleteFolderBranchInvalidResponse, value, failurePointer(deleteFolderOperationID, sdkgo.FailureProtocol, "S3 returned an object outside the folder prefix"), client.receipt(call, requestID, location))
			}
			item := objectLocation{bucket: location.bucket, key: object.Key, hasKey: true}
			deleted, deleteErr := client.exchange(session, s3Request{method: http.MethodDelete, location: item})
			if errors.Is(deleteErr, errRequestNotBuilt) {
				return sdkgo.NewMutationBranch(DeleteFolderBranchDefect, value, failurePointer(deleteFolderOperationID, sdkgo.FailureLocalDefect, deleteErr.Error()), sdkgo.Receipt{})
			}
			if deleteErr != nil {
				return sdkgo.NewMutationRetry[DeletedFolder](transportFailure(deleteFolderOperationID, "folder object deletion"), 0)
			}
			if !isSuccessStatus(deleted.StatusCode) {
				classified := classifyErrorResponse(deleteFolderOperationID, "folder object deletion", client.readErrorResponse(deleted, session.credentials), true)
				closeResponseBody(deleted)
				return mutationAttemptForFailure(classified, client.receipt(call, classified.requestID, item), DeleteFolderBranchProviderRejected, DeleteFolderBranchDefect, value)
			}
			closeResponseBody(deleted)
		}
	}
	return sdkgo.NewMutationRetry[DeletedFolder](transportFailure(deleteFolderOperationID, "folder deletion"), 0)
}

func (client *Client) folderLocation(bucket string, prefix string) (objectLocation, error) {
	if prefix == "" {
		return objectLocation{}, errors.New("folder prefix is required")
	}
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	return client.validateObjectInput(bucket, prefix)
}
