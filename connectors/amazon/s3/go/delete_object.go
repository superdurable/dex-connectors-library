// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package s3

import (
	"errors"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const deleteObjectOperationID = "deleteObject"

// DeleteObjectInput identifies the current object to remove.
type DeleteObjectInput struct {
	// Bucket is the bucket name; blank uses the connection's defaultBucket.
	Bucket string `json:"bucket,omitempty"`
	// Key is the object key to remove.
	Key string `json:"key"`
}

// DeletedObject identifies the key whose deletion S3 accepted.
type DeletedObject struct {
	// Bucket is the bucket containing the key.
	Bucket string `json:"bucket"`
	// Key is the removed object's key.
	Key string `json:"key"`
}

// DeleteObjectOperation implements the deleteObject connector operation.
type DeleteObjectOperation struct{ client *Client }

// Definition returns the generated mutation contract.
func (DeleteObjectOperation) Definition() sdkgo.MutationDefinition { return DeleteObjectDefinition }

// IdempotencyKey derives a stable key for retries of the same Step.
func (DeleteObjectOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteObjectInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends a signed DELETE. An absent current key is also a successful deletion.
func (operation DeleteObjectOperation) Invoke(call sdkgo.Call, input DeleteObjectInput) sdkgo.MutationAttempt[DeletedObject] {
	client := operation.client
	location, err := client.validateObjectInput(input.Bucket, input.Key)
	if err != nil {
		return sdkgo.NewMutationBranch(DeleteObjectBranchDefect, DeletedObject{}, defectFailure(deleteObjectOperationID, err), sdkgo.Receipt{})
	}
	session, cancel, failure := client.startSession(call, deleteObjectOperationID)
	if failure != nil {
		return sdkgo.NewMutationBranch(DeleteObjectBranchDefect, DeletedObject{}, failure, sdkgo.Receipt{})
	}
	defer cancel()
	deleted := DeletedObject{Bucket: location.bucket, Key: location.key}
	response, err := client.exchange(session, s3Request{method: http.MethodDelete, location: location})
	if errors.Is(err, errRequestNotBuilt) {
		return sdkgo.NewMutationBranch(DeleteObjectBranchDefect, deleted, failurePointer(deleteObjectOperationID, sdkgo.FailureLocalDefect, err.Error()), sdkgo.Receipt{})
	}
	if err != nil {
		return sdkgo.NewMutationRetry[DeletedObject](transportFailure(deleteObjectOperationID, "object deletion"), 0)
	}
	defer closeResponseBody(response)
	if isSuccessStatus(response.StatusCode) {
		return sdkgo.NewMutationBranch(DeleteObjectBranchDeleted, deleted, nil, client.receipt(call, safeRequestID(response.Header), location))
	}
	classified := classifyErrorResponse(deleteObjectOperationID, "object deletion", client.readErrorResponse(response, session.credentials), true)
	return mutationAttemptForFailure(classified, client.receipt(call, classified.requestID, location), DeleteObjectBranchProviderRejected, DeleteObjectBranchDefect, deleted)
}
