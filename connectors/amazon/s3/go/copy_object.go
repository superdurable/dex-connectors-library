// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package s3

import (
	"encoding/xml"
	"errors"
	"net/http"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

const copyObjectOperationID = "copyObject"

// CopyObjectInput copies one source object to a destination key, replacing its metadata.
type CopyObjectInput struct {
	// SourceBucket is the source bucket; blank uses the connection default.
	SourceBucket string `json:"sourceBucket,omitempty"`
	// SourceKey is the source object key.
	SourceKey string `json:"sourceKey"`
	// DestinationBucket is the destination bucket; blank uses the connection default.
	DestinationBucket string `json:"destinationBucket,omitempty"`
	// DestinationKey is the key that receives the copy.
	DestinationKey string `json:"destinationKey"`
	// ContentType is the media type of the copied object; other source metadata is not retained.
	ContentType string `json:"contentType"`
	// SourceETag, when set, makes S3 copy only the source version with that ETag.
	SourceETag string `json:"sourceETag,omitempty"`
	// Metadata replaces source user metadata; the connector also adds its idempotency marker.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// CopiedObject identifies the destination after S3 accepted or a read-back confirmed the copy.
type CopiedObject struct {
	// SourceBucket is the source bucket.
	SourceBucket string `json:"sourceBucket"`
	// SourceKey is the source key.
	SourceKey string `json:"sourceKey"`
	// DestinationBucket is the destination bucket.
	DestinationBucket string `json:"destinationBucket"`
	// DestinationKey is the destination key.
	DestinationKey string `json:"destinationKey"`
	// ETag is the destination ETag, including quotes when supplied.
	ETag string `json:"eTag,omitempty"`
	// VersionID is the destination version when versioning is enabled.
	VersionID string `json:"versionId,omitempty"`
	// IsFromEarlierAttempt reports that a destination marker confirmed an earlier send.
	IsFromEarlierAttempt bool `json:"isFromEarlierAttempt,omitempty"`
}

// CopyObjectOperation implements bounded S3 object copying.
type CopyObjectOperation struct{ client *Client }

// Definition returns the generated Mutation contract.
func (CopyObjectOperation) Definition() sdkgo.MutationDefinition { return CopyObjectDefinition }

// IdempotencyKey derives a stable marker for one Dex Call.
func (CopyObjectOperation) IdempotencyKey(callID sdkgo.CallID, _ CopyObjectInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke copies a source to a destination, checking its marker before a possible resend after an unknown result.
func (operation CopyObjectOperation) Invoke(call sdkgo.Call, input CopyObjectInput) sdkgo.MutationAttempt[CopiedObject] {
	client := operation.client
	source, err := client.validateObjectInput(input.SourceBucket, input.SourceKey)
	if err != nil {
		return copyDefect(err)
	}
	destination, err := client.validateObjectInput(input.DestinationBucket, input.DestinationKey)
	if err != nil {
		return copyDefect(err)
	}
	if err := validateContentType(input.ContentType); err != nil {
		return copyDefect(err)
	}
	if err := validateUserMetadata(input.Metadata); err != nil {
		return copyDefect(err)
	}
	if input.SourceETag != "" && (!isPrintableHeaderValue(input.SourceETag) || len(input.SourceETag) > 128 || !strings.HasPrefix(input.SourceETag, `"`) || !strings.HasSuffix(input.SourceETag, `"`)) {
		return copyDefect(errors.New("sourceETag must be a quoted S3 ETag"))
	}
	marker := string(call.IdempotencyKey)
	if !providerhttp.IsHeaderSafeCredential(marker) || len(marker) > maxIdempotencyKeyBytes {
		return copyDefect(errors.New("idempotency key is missing or not header-safe"))
	}
	value := CopiedObject{SourceBucket: source.bucket, SourceKey: source.key, DestinationBucket: destination.bucket, DestinationKey: destination.key}
	fence, err := singledispatch.NewFence(call, CopyObjectDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return copyDefect(errors.New("copy fence could not be initialized"))
	}
	switch fence.Decide().Kind {
	case singledispatch.FailClosed:
		return sdkgo.NewMutationUncertain(value, newFailure(copyObjectOperationID, sdkgo.FailureTransport, "S3 copy checkpoint cannot be trusted"), sdkgo.Receipt{})
	case singledispatch.ReadBackRequired, singledispatch.ReadBackThenResendAllowed:
		if metadata, isConfirmed := operation.readBack(call, destination, marker); isConfirmed {
			value.ETag, value.VersionID, value.IsFromEarlierAttempt = metadata.ETag, metadata.VersionID, true
			return sdkgo.NewMutationBranch(CopyObjectBranchCopied, value, nil, client.receipt(call, "", destination))
		}
		if fence.Decide().Kind == singledispatch.ReadBackRequired {
			return sdkgo.NewMutationUncertain(value, newFailure(copyObjectOperationID, sdkgo.FailureTransport, "S3 copy could not be confirmed after the send cap"), sdkgo.Receipt{})
		}
	}
	session, cancel, failure := client.startSession(call, copyObjectOperationID)
	if failure != nil {
		if fence.RecordNotSent() != nil {
			return sdkgo.NewMutationUncertain(value, newFailure(copyObjectOperationID, sdkgo.FailureTransport, "S3 unsent checkpoint could not be recorded"), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationBranch(CopyObjectBranchDefect, value, failure, sdkgo.Receipt{})
	}
	defer cancel()
	header := http.Header{}
	header.Set("X-Amz-Copy-Source", escapeS3Path("/"+source.bucket+"/"+source.key))
	header.Set("X-Amz-Metadata-Directive", "REPLACE")
	header.Set("Content-Type", input.ContentType)
	header.Set(metadataHeaderPrefix+IdempotencyMetadataKey, marker)
	if input.SourceETag != "" {
		header.Set("X-Amz-Copy-Source-If-Match", input.SourceETag)
	}
	for name, text := range input.Metadata {
		header.Set(metadataHeaderPrefix+name, text)
	}
	dispatch := &bucketCreateDispatch{client: client, session: session, request: s3Request{method: http.MethodPut, location: destination, header: header}}
	if fence.Dispatch(dispatch.send) != nil {
		return sdkgo.NewMutationUncertain(value, newFailure(copyObjectOperationID, sdkgo.FailureTransport, "S3 copy dispatch could not be confirmed"), sdkgo.Receipt{})
	}
	response, err := dispatch.response, dispatch.err
	if errors.Is(err, errRequestNotBuilt) {
		return copyDefect(err)
	}
	if err != nil {
		return sdkgo.NewMutationRetry[CopiedObject](transportFailure(copyObjectOperationID, "object copy"), 0)
	}
	defer closeResponseBody(response)
	if !isSuccessStatus(response.StatusCode) {
		classified := classifyErrorResponse(copyObjectOperationID, "object copy", client.readErrorResponse(response, session.credentials), false)
		if classified.outcome == outcomeRetry {
			if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable {
				if fence.RecordCanResend() != nil {
					return sdkgo.NewMutationUncertain(value, newFailure(copyObjectOperationID, sdkgo.FailureTransport, "S3 resend checkpoint could not be recorded"), sdkgo.Receipt{})
				}
			}
			return sdkgo.NewMutationRetry[CopiedObject](classified.failure, classified.retryAfter)
		}
		return sdkgo.NewMutationBranch(CopyObjectBranchProviderRejected, value, &classified.failure, client.receipt(call, classified.requestID, destination))
	}
	contents, err := providerhttp.ReadBoundedBody(response.Body, providerhttp.MaxErrorBodyBytes)
	if err != nil || containsCredential(string(contents), session.credentials, true) {
		return sdkgo.NewMutationRetry[CopiedObject](newFailure(copyObjectOperationID, sdkgo.FailureProtocol, "S3 copy response could not be verified"), 0)
	}
	var document struct {
		XMLName xml.Name `xml:"CopyObjectResult"`
		ETag    string   `xml:"ETag"`
	}
	if xml.Unmarshal(contents, &document) != nil || document.XMLName.Local != "CopyObjectResult" || document.ETag == "" || len(document.ETag) > 128 {
		return sdkgo.NewMutationRetry[CopiedObject](newFailure(copyObjectOperationID, sdkgo.FailureProtocol, "S3 copy response could not be verified"), 0)
	}
	value.ETag = document.ETag
	value.VersionID = safeHeaderValue(response.Header.Get(versionIDHeader), session.credentials)
	return sdkgo.NewMutationBranch(CopyObjectBranchCopied, value, nil, client.receipt(call, safeRequestID(response.Header), destination))
}

func (operation CopyObjectOperation) readBack(call sdkgo.Call, location objectLocation, marker string) (ObjectMetadata, bool) {
	session, cancel, failure := operation.client.startSession(call, copyObjectOperationID)
	if failure != nil {
		return ObjectMetadata{}, false
	}
	defer cancel()
	result, classified := operation.client.headObject(session, copyObjectOperationID, location)
	return result.value, classified == nil && !result.isInvalid && result.value.Metadata[IdempotencyMetadataKey] == marker
}

func copyDefect(err error) sdkgo.MutationAttempt[CopiedObject] {
	return sdkgo.NewMutationBranch(CopyObjectBranchDefect, CopiedObject{}, defectFailure(copyObjectOperationID, err), sdkgo.Receipt{})
}
