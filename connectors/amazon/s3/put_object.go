// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package s3

import (
	"crypto/md5"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	putObjectOperationID = "putObject"
	// IdempotencyMetadataKey is the user-defined metadata name, without the x-amz-meta- prefix, under which
	// putObject records the Step execution's idempotency key on every object it writes.
	IdempotencyMetadataKey = "dex-idempotency-key"
	// maxUserMetadataBytes is the Amazon S3 limit on user-defined metadata in one PUT request.
	maxUserMetadataBytes   = 2048
	maxContentTypeBytes    = 256
	maxIdempotencyKeyBytes = 128
)

// metadataKeyPattern keeps metadata names lowercase, as S3 stores them, and safe as HTTP header names.
var metadataKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)

// PutObjectInput describes one object to store. Set at most one of TextContent and ByteContent; leaving
// both empty stores an empty object.
type PutObjectInput struct {
	// Bucket is the bucket name; blank uses the connection's defaultBucket.
	Bucket string `json:"bucket,omitempty"`
	// Key is the object key, such as reports/2026-09.md.
	Key string `json:"key"`
	// ContentType is the media type S3 stores and returns, such as text/markdown; charset=utf-8.
	ContentType string `json:"contentType"`
	// TextContent is UTF-8 text content.
	TextContent string `json:"textContent,omitempty"`
	// ByteContent is binary content, encoded as base64 in JSON.
	ByteContent []byte `json:"byteContent,omitempty"`
	// Metadata is user-defined metadata, keyed by lowercase name without the x-amz-meta- prefix, such as
	// report-id. Values are printable ASCII. The dex-idempotency-key name is reserved.
	Metadata map[string]string `json:"metadata,omitempty"`
	// IsCreateOnly stores the object only when no current object exists at the key, by sending
	// If-None-Match: *; an object that this Step execution did not write selects alreadyExists.
	IsCreateOnly bool `json:"isCreateOnly,omitempty"`
}

// StoredObject identifies the object at the key after putObject. On alreadyExists it describes the
// existing object, which this Step execution did not write.
type StoredObject struct {
	// Bucket is the object's bucket.
	Bucket string `json:"bucket"`
	// Key is the object key.
	Key string `json:"key"`
	// ETag is the entity tag exactly as S3 reports it, including its double quotes.
	ETag string `json:"eTag,omitempty"`
	// VersionID is the version S3 created in a versioning-enabled bucket; empty otherwise.
	VersionID string `json:"versionId,omitempty"`
	// SizeBytes is the object size.
	SizeBytes int64 `json:"sizeBytes"`
	// ContentType is the object's content type.
	ContentType string `json:"contentType,omitempty"`
	// LastModified is when the object was written, set when the connector read the object back.
	LastModified time.Time `json:"lastModified,omitzero"`
	// IsFromEarlierAttempt reports that a create-only write found the object an earlier attempt of the same
	// Step execution stored, so this attempt wrote nothing.
	IsFromEarlierAttempt bool `json:"isFromEarlierAttempt,omitempty"`
}

// PutObjectOperation implements the putObject connector operation.
type PutObjectOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (PutObjectOperation) Definition() sdkgo.MutationDefinition { return PutObjectDefinition }

// IdempotencyKey derives the key from the stable connector call ID, so every attempt of one Step execution
// shares it and sends identical requests.
func (PutObjectOperation) IdempotencyKey(callID sdkgo.CallID, _ PutObjectInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends one PUT request with the content's MD5 and SHA-256 and the idempotency marker. Every
// ambiguous outcome returns Retry, because a repeated PUT of the same bytes leaves the same object. In
// create-only mode a 412 is followed by one HEAD request: an object carrying this Step execution's marker is
// reported as stored, and any other object selects alreadyExists.
func (operation PutObjectOperation) Invoke(call sdkgo.Call, input PutObjectInput) sdkgo.MutationAttempt[StoredObject] {
	client := operation.client
	location, content, err := operation.validateInput(input)
	if err != nil {
		return sdkgo.NewMutationBranch(PutObjectBranchDefect, StoredObject{}, defectFailure(putObjectOperationID, err), sdkgo.Receipt{})
	}
	idempotencyKey := string(call.IdempotencyKey)
	if !providerhttp.IsHeaderSafeCredential(idempotencyKey) || len(idempotencyKey) > maxIdempotencyKeyBytes {
		return sdkgo.NewMutationBranch(PutObjectBranchDefect, StoredObject{}, failurePointer(putObjectOperationID, sdkgo.FailureLocalDefect,
			"idempotency key is missing or not header-safe"), sdkgo.Receipt{})
	}
	session, cancel, failure := client.startSession(call, putObjectOperationID)
	if failure != nil {
		return sdkgo.NewMutationBranch(PutObjectBranchDefect, StoredObject{}, failure, sdkgo.Receipt{})
	}
	defer cancel()
	requested := StoredObject{Bucket: location.bucket, Key: location.key, SizeBytes: int64(len(content)), ContentType: input.ContentType}
	response, err := client.exchange(session, s3Request{
		method: http.MethodPut, location: location, body: content, header: putObjectHeader(input, content, idempotencyKey),
	})
	if errors.Is(err, errRequestNotBuilt) {
		return sdkgo.NewMutationBranch(PutObjectBranchDefect, requested, failurePointer(putObjectOperationID, sdkgo.FailureLocalDefect, err.Error()), sdkgo.Receipt{})
	}
	if err != nil {
		// Resending the same bytes to the same key converges, so an unknown outcome is safe to retry.
		return sdkgo.NewMutationRetry[StoredObject](transportFailure(putObjectOperationID, "object write"), 0)
	}
	defer closeResponseBody(response)
	if isSuccessStatus(response.StatusCode) {
		stored := requested
		stored.ETag = safeHeaderValue(response.Header.Get("ETag"), session.credentials)
		stored.VersionID = safeHeaderValue(response.Header.Get(versionIDHeader), session.credentials)
		return sdkgo.NewMutationBranch(PutObjectBranchStored, stored, nil, client.receipt(call, safeRequestID(response.Header), location))
	}
	errorResponse := client.readErrorResponse(response, session.credentials)
	if input.IsCreateOnly && errorResponse.statusCode == http.StatusPreconditionFailed {
		return operation.resolveExistingObject(session, location, requested)
	}
	classified := classifyErrorResponse(putObjectOperationID, "object write", errorResponse, false)
	if input.IsCreateOnly && errorResponse.code == "NotImplemented" {
		classified.failure.Message = statusMessage("S3 rejected the create-only write", errorResponse) +
			"; the store does not support If-None-Match, so leave isCreateOnly false"
	}
	return mutationAttemptForFailure(classified, client.receipt(call, classified.requestID, location), PutObjectBranchProviderRejected, PutObjectBranchDefect, requested)
}

// resolveExistingObject decides whether an earlier attempt of this Step stored the existing object.
func (operation PutObjectOperation) resolveExistingObject(session *operationSession, location objectLocation, requested StoredObject) sdkgo.MutationAttempt[StoredObject] {
	client, call := operation.client, session.call
	existing, classified := client.headObject(session, putObjectOperationID, location)
	switch {
	case classified != nil && classified.outcome == outcomeNotFound:
		return sdkgo.NewMutationRetry[StoredObject](newFailure(putObjectOperationID, sdkgo.FailureConflict,
			"the object that refused the create-only write was removed before it could be read; the write is retried"), 0)
	case classified != nil && classified.failure.Kind == sdkgo.FailureAuthorization:
		failure := newFailure(putObjectOperationID, sdkgo.FailureAuthorization,
			"an object exists at the key, and reading it needs s3:GetObject to tell this Step's earlier write from another writer's")
		return sdkgo.NewMutationBranch(PutObjectBranchProviderRejected, requested, &failure, client.receipt(call, classified.requestID, location))
	case classified != nil:
		return mutationAttemptForFailure(*classified, client.receipt(call, classified.requestID, location), PutObjectBranchProviderRejected, PutObjectBranchDefect, requested)
	case existing.isInvalid:
		return sdkgo.NewMutationBranch(PutObjectBranchProviderRejected, requested, failurePointer(putObjectOperationID, sdkgo.FailureProtocol,
			"S3 returned invalid metadata for the existing object, so the connector cannot tell who wrote it"), client.receipt(call, existing.requestID, location))
	}
	metadata := existing.value
	stored := StoredObject{
		Bucket: location.bucket, Key: location.key, ETag: metadata.ETag, VersionID: metadata.VersionID,
		SizeBytes: metadata.SizeBytes, ContentType: metadata.ContentType, LastModified: metadata.LastModified,
	}
	receipt := client.receipt(call, existing.requestID, location)
	if metadata.Metadata[IdempotencyMetadataKey] == string(call.IdempotencyKey) {
		stored.IsFromEarlierAttempt = true
		return sdkgo.NewMutationBranch(PutObjectBranchStored, stored, nil, receipt)
	}
	return sdkgo.NewMutationBranch(PutObjectBranchAlreadyExists, stored, failurePointer(putObjectOperationID, sdkgo.FailureConflict,
		"an object that this Step did not write already exists at the key"), receipt)
}

// validateInput resolves the location and returns the content to send.
func (operation PutObjectOperation) validateInput(input PutObjectInput) (objectLocation, []byte, error) {
	client := operation.client
	location, err := client.validateObjectInput(input.Bucket, input.Key)
	if err != nil {
		return objectLocation{}, nil, err
	}
	if input.TextContent != "" && len(input.ByteContent) > 0 {
		return objectLocation{}, nil, errors.New("set at most one of textContent and byteContent")
	}
	content := input.ByteContent
	if input.TextContent != "" {
		content = []byte(input.TextContent)
	}
	if content == nil {
		content = []byte{}
	}
	if int64(len(content)) > client.maxUploadBytes {
		return objectLocation{}, nil, fmt.Errorf("content exceeds maxUploadBytes (%d bytes)", client.maxUploadBytes)
	}
	if err := validateContentType(input.ContentType); err != nil {
		return objectLocation{}, nil, err
	}
	if err := validateUserMetadata(input.Metadata); err != nil {
		return objectLocation{}, nil, err
	}
	return location, content, nil
}

// putObjectHeader builds headers that are identical for every attempt of one Step execution.
func putObjectHeader(input PutObjectInput, content []byte, idempotencyKey string) http.Header {
	digest := md5.Sum(content)
	header := http.Header{}
	header.Set("Content-Type", input.ContentType)
	// Content-MD5 lets S3 reject corrupted content, and Object Lock buckets require it.
	header.Set("Content-MD5", base64.StdEncoding.EncodeToString(digest[:]))
	for name, value := range input.Metadata {
		header.Set(metadataHeaderPrefix+name, value)
	}
	header.Set(metadataHeaderPrefix+IdempotencyMetadataKey, idempotencyKey)
	if input.IsCreateOnly {
		header.Set("If-None-Match", "*")
	}
	return header
}

func validateContentType(contentType string) error {
	if contentType == "" || len(contentType) > maxContentTypeBytes || !isPrintableHeaderValue(contentType) {
		return errors.New("contentType must be a media type such as text/plain; charset=utf-8")
	}
	if _, _, err := mime.ParseMediaType(contentType); err != nil {
		return errors.New("contentType must be a media type such as text/plain; charset=utf-8")
	}
	return nil
}

// validateUserMetadata counts the 2 KB limit conservatively, with prefixes and the connector's marker.
func validateUserMetadata(metadata map[string]string) error {
	size := len(metadataHeaderPrefix) + len(IdempotencyMetadataKey) + maxIdempotencyKeyBytes
	for name, value := range metadata {
		if !metadataKeyPattern.MatchString(name) {
			return errors.New("metadata names must be 1 to 128 lowercase letters, digits, dots, hyphens, or underscores, starting with a letter or digit")
		}
		if name == IdempotencyMetadataKey {
			return errors.New("metadata name dex-idempotency-key is reserved for the connector")
		}
		if value == "" || !isPrintableHeaderValue(value) || strings.TrimSpace(value) != value {
			return errors.New("metadata values must be non-empty printable ASCII without leading or trailing spaces")
		}
		size += len(metadataHeaderPrefix) + len(name) + len(value)
	}
	if size > maxUserMetadataBytes {
		return errors.New("metadata exceeds the 2 KB S3 limit, counted with the x-amz-meta- prefixes and the connector's marker")
	}
	return nil
}

// isPrintableHeaderValue accepts printable ASCII, including spaces, which S3 stores unchanged.
func isPrintableHeaderValue(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] < 0x20 || value[index] > 0x7E {
			return false
		}
	}
	return true
}
