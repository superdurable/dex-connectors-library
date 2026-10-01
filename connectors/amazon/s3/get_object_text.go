// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package s3

import (
	"errors"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const getObjectTextOperationID = "getObjectText"

// textMediaTypes lists the non-text/* media types whose content is text.
var textMediaTypes = map[string]bool{
	"application/json": true, "application/ld+json": true, "application/xml": true, "application/yaml": true,
	"application/x-yaml": true, "application/x-ndjson": true, "application/javascript": true, "application/sql": true,
	"application/csv": true, "application/toml": true,
}

// unlabeledMediaTypes are the content types stores give an object uploaded without one; their bytes decide.
var unlabeledMediaTypes = map[string]bool{"": true, "application/octet-stream": true, "binary/octet-stream": true}

// GetObjectTextInput identifies the object whose text is read.
type GetObjectTextInput struct {
	// Bucket is the bucket name; blank uses the connection's defaultBucket.
	Bucket string `json:"bucket,omitempty"`
	// Key is the object key, such as reports/2026-09.md.
	Key string `json:"key"`
}

// ObjectText is the bounded text of one object. The unsupportedContent and tooLarge branches return the
// object's identity, content type, and size without Text.
type ObjectText struct {
	// Bucket is the object's bucket.
	Bucket string `json:"bucket"`
	// Key is the object key.
	Key string `json:"key"`
	// ContentType is the stored Content-Type.
	ContentType string `json:"contentType,omitempty"`
	// ETag is the entity tag exactly as S3 reports it, including its double quotes.
	ETag string `json:"eTag,omitempty"`
	// VersionID is the object version in a versioning-enabled bucket; empty otherwise.
	VersionID string `json:"versionId,omitempty"`
	// LastModified is when the object was last written.
	LastModified time.Time `json:"lastModified,omitzero"`
	// SizeBytes is the object size, which equals the UTF-8 byte length of Text when Text is returned.
	SizeBytes int64 `json:"sizeBytes"`
	// Text is the complete object content, unchanged, including any byte order mark.
	Text string `json:"text,omitempty"`
}

// GetObjectTextOperation implements the getObjectText connector operation.
type GetObjectTextOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (GetObjectTextOperation) Definition() sdkgo.QueryDefinition { return GetObjectTextDefinition }

// Invoke sends one GET request and decides from the response headers before reading content: a non-text
// content type or a Content-Encoding selects unsupportedContent, and a size above maxTextBytes selects
// tooLarge. Content that is not valid UTF-8 or contains a NUL byte also selects unsupportedContent.
func (operation GetObjectTextOperation) Invoke(call sdkgo.Call, input GetObjectTextInput) sdkgo.QueryAttempt[ObjectText] {
	client := operation.client
	location, err := client.validateObjectInput(input.Bucket, input.Key)
	if err != nil {
		return sdkgo.NewQueryBranch(GetObjectTextBranchDefect, ObjectText{}, defectFailure(getObjectTextOperationID, err), sdkgo.Receipt{})
	}
	session, cancel, failure := client.startSession(call, getObjectTextOperationID)
	if failure != nil {
		return sdkgo.NewQueryBranch(GetObjectTextBranchDefect, ObjectText{}, failure, sdkgo.Receipt{})
	}
	defer cancel()
	identity := ObjectText{Bucket: location.bucket, Key: location.key}
	response, err := client.exchange(session, s3Request{method: http.MethodGet, location: location})
	if errors.Is(err, errRequestNotBuilt) {
		return sdkgo.NewQueryBranch(GetObjectTextBranchDefect, identity, failurePointer(getObjectTextOperationID, sdkgo.FailureLocalDefect, err.Error()), sdkgo.Receipt{})
	}
	if err != nil {
		return sdkgo.NewQueryRetry[ObjectText](transportFailure(getObjectTextOperationID, "object read"), 0)
	}
	defer closeResponseBody(response)
	if !isSuccessStatus(response.StatusCode) {
		classified := classifyErrorResponse(getObjectTextOperationID, "object", client.readErrorResponse(response, session.credentials), true)
		return queryAttemptForFailure(classified, client.receipt(call, classified.requestID, location),
			GetObjectTextBranchNotFound, GetObjectTextBranchProviderRejected, GetObjectTextBranchDefect, identity)
	}
	receipt := client.receipt(call, safeRequestID(response.Header), location)
	metadata, err := objectMetadataFromResponse(location, response, session.credentials)
	if err != nil {
		// A store may omit Last-Modified or Content-Length on GET; the bounded read below still applies.
		metadata = ObjectMetadata{
			Bucket: location.bucket, Key: location.key, SizeBytes: max(response.ContentLength, 0),
			ContentType: safeHeaderValue(response.Header.Get("Content-Type"), session.credentials),
			ETag:        safeHeaderValue(response.Header.Get("ETag"), session.credentials),
		}
	}
	output := ObjectText{
		Bucket: location.bucket, Key: location.key, ContentType: metadata.ContentType, ETag: metadata.ETag,
		VersionID: metadata.VersionID, LastModified: metadata.LastModified, SizeBytes: metadata.SizeBytes,
	}
	if reason := unsupportedContentReason(response.Header); reason != "" {
		return sdkgo.NewQueryBranch(GetObjectTextBranchUnsupportedContent, output, failurePointer(getObjectTextOperationID, sdkgo.FailureValidation, reason), receipt)
	}
	if response.ContentLength > client.maxTextBytes {
		return sdkgo.NewQueryBranch(GetObjectTextBranchTooLarge, output, failurePointer(getObjectTextOperationID, sdkgo.FailureResponseTooLarge,
			"object size exceeds maxTextBytes"), receipt)
	}
	contents, err := providerhttp.ReadBoundedBody(response.Body, client.maxTextBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return sdkgo.NewQueryBranch(GetObjectTextBranchTooLarge, output, failurePointer(getObjectTextOperationID, sdkgo.FailureResponseTooLarge,
			"object content exceeds maxTextBytes"), receipt)
	}
	if err != nil {
		return sdkgo.NewQueryRetry[ObjectText](transportFailure(getObjectTextOperationID, "object read"), 0)
	}
	if response.ContentLength >= 0 && int64(len(contents)) != response.ContentLength {
		return sdkgo.NewQueryBranch(GetObjectTextBranchInvalidResponse, output, failurePointer(getObjectTextOperationID, sdkgo.FailureProtocol,
			"S3 returned content whose length differs from Content-Length"), receipt)
	}
	text := string(contents)
	output.SizeBytes = int64(len(contents))
	if !utf8.ValidString(text) || strings.IndexByte(text, 0) >= 0 {
		return sdkgo.NewQueryBranch(GetObjectTextBranchUnsupportedContent, output, failurePointer(getObjectTextOperationID, sdkgo.FailureValidation,
			"object content is not valid UTF-8 text"), receipt)
	}
	if containsCredential(text, session.credentials, false) {
		return sdkgo.NewQueryBranch(GetObjectTextBranchInvalidResponse, output, failurePointer(getObjectTextOperationID, sdkgo.FailureProtocol,
			"object content contains the connection's credentials, so it is not returned"), receipt)
	}
	output.Text = text
	return sdkgo.NewQueryBranch(GetObjectTextBranchRead, output, nil, receipt)
}

// unsupportedContentReason refuses an encoded object or a declared non-text media type before any read.
func unsupportedContentReason(header http.Header) string {
	if encoding := strings.TrimSpace(header.Get("Content-Encoding")); encoding != "" && !strings.EqualFold(encoding, "identity") {
		return "object content is stored with a Content-Encoding such as gzip, which getObjectText does not decode"
	}
	contentType := strings.TrimSpace(header.Get("Content-Type"))
	mediaType := ""
	if contentType != "" {
		parsed, _, err := mime.ParseMediaType(contentType)
		if err != nil {
			return "object content type is not a valid media type"
		}
		mediaType = parsed
	}
	if isTextMediaType(mediaType) || unlabeledMediaTypes[mediaType] {
		return ""
	}
	return "object content type is not text"
}

// isTextMediaType accepts text/*, the listed text application types, and +json, +xml, or +yaml suffixes.
func isTextMediaType(mediaType string) bool {
	return strings.HasPrefix(mediaType, "text/") || textMediaTypes[mediaType] ||
		strings.HasSuffix(mediaType, "+json") || strings.HasSuffix(mediaType, "+xml") || strings.HasSuffix(mediaType, "+yaml")
}
