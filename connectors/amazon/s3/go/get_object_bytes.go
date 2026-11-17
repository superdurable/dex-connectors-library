// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package s3

import (
	"errors"
	"net/http"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const getObjectBytesOperationID = "getObjectBytes"

// GetObjectBytesInput identifies an object whose complete bytes fit the connection response limit.
type GetObjectBytesInput struct {
	// Bucket is the bucket; blank uses the connection default.
	Bucket string `json:"bucket,omitempty"`
	// Key is the object key.
	Key string `json:"key"`
}

// ObjectBytes contains one bounded binary object, encoded as base64 in Flow JSON state.
type ObjectBytes struct {
	// Bucket is the object's bucket.
	Bucket string `json:"bucket"`
	// Key is the object key.
	Key string `json:"key"`
	// ContentType is the stored media type.
	ContentType string `json:"contentType,omitempty"`
	// ETag is S3's quoted entity tag.
	ETag string `json:"eTag,omitempty"`
	// VersionID is set for a versioned object.
	VersionID string `json:"versionId,omitempty"`
	// LastModified is the last modification time when supplied.
	LastModified time.Time `json:"lastModified,omitzero"`
	// SizeBytes is the full object size.
	SizeBytes int64 `json:"sizeBytes"`
	// Bytes are the complete object content; nil on non-success branches.
	Bytes []byte `json:"bytes,omitempty"`
}

// GetObjectBytesOperation implements the getObjectBytes Query.
type GetObjectBytesOperation struct{ client *Client }

// Definition returns the generated Query contract.
func (GetObjectBytesOperation) Definition() sdkgo.QueryDefinition { return GetObjectBytesDefinition }

// Invoke reads at most maxResponseBytes and refuses an incomplete or secret-bearing response.
func (operation GetObjectBytesOperation) Invoke(call sdkgo.Call, input GetObjectBytesInput) sdkgo.QueryAttempt[ObjectBytes] {
	client := operation.client
	location, err := client.validateObjectInput(input.Bucket, input.Key)
	if err != nil {
		return sdkgo.NewQueryBranch(GetObjectBytesBranchDefect, ObjectBytes{}, defectFailure(getObjectBytesOperationID, err), sdkgo.Receipt{})
	}
	session, cancel, failure := client.startSession(call, getObjectBytesOperationID)
	if failure != nil {
		return sdkgo.NewQueryBranch(GetObjectBytesBranchDefect, ObjectBytes{}, failure, sdkgo.Receipt{})
	}
	defer cancel()
	output := ObjectBytes{Bucket: location.bucket, Key: location.key}
	response, err := client.exchange(session, s3Request{method: http.MethodGet, location: location})
	if errors.Is(err, errRequestNotBuilt) {
		return sdkgo.NewQueryBranch(GetObjectBytesBranchDefect, output, failurePointer(getObjectBytesOperationID, sdkgo.FailureLocalDefect, err.Error()), sdkgo.Receipt{})
	}
	if err != nil {
		return sdkgo.NewQueryRetry[ObjectBytes](transportFailure(getObjectBytesOperationID, "object bytes read"), 0)
	}
	defer closeResponseBody(response)
	if !isSuccessStatus(response.StatusCode) {
		classified := classifyErrorResponse(getObjectBytesOperationID, "object bytes read", client.readErrorResponse(response, session.credentials), true)
		return queryAttemptForFailure(classified, client.receipt(call, classified.requestID, location), GetObjectBytesBranchNotFound, GetObjectBytesBranchProviderRejected, GetObjectBytesBranchDefect, output)
	}
	receipt := client.receipt(call, safeRequestID(response.Header), location)
	output.ContentType = safeHeaderValue(response.Header.Get("Content-Type"), session.credentials)
	output.ETag = safeHeaderValue(response.Header.Get("ETag"), session.credentials)
	output.VersionID = safeHeaderValue(response.Header.Get(versionIDHeader), session.credentials)
	if modified := response.Header.Get("Last-Modified"); modified != "" {
		if parsed, parseErr := http.ParseTime(modified); parseErr == nil {
			output.LastModified = parsed
		}
	}
	if response.ContentLength > client.maxResponseBytes {
		output.SizeBytes = response.ContentLength
		return sdkgo.NewQueryBranch(GetObjectBytesBranchTooLarge, output, failurePointer(getObjectBytesOperationID, sdkgo.FailureResponseTooLarge, "object exceeds maxResponseBytes"), receipt)
	}
	contents, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return sdkgo.NewQueryBranch(GetObjectBytesBranchTooLarge, output, failurePointer(getObjectBytesOperationID, sdkgo.FailureResponseTooLarge, "object exceeds maxResponseBytes"), receipt)
	}
	if err != nil {
		return sdkgo.NewQueryRetry[ObjectBytes](transportFailure(getObjectBytesOperationID, "object bytes read"), 0)
	}
	if response.ContentLength >= 0 && int64(len(contents)) != response.ContentLength {
		return sdkgo.NewQueryBranch(GetObjectBytesBranchInvalidResponse, output, failurePointer(getObjectBytesOperationID, sdkgo.FailureProtocol, "S3 returned an incomplete object"), receipt)
	}
	if containsCredential(string(contents), session.credentials, false) {
		return sdkgo.NewQueryBranch(GetObjectBytesBranchInvalidResponse, output, failurePointer(getObjectBytesOperationID, sdkgo.FailureProtocol, "object contains connection credentials"), receipt)
	}
	output.SizeBytes = int64(len(contents))
	output.Bytes = contents
	return sdkgo.NewQueryBranch(GetObjectBytesBranchRead, output, nil, receipt)
}
