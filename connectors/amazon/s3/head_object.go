// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package s3

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	headObjectOperationID = "headObject"
	// maxHeaderValueBytes bounds every metadata header value copied into a Result.
	maxHeaderValueBytes = 2048
)

// HeadObjectInput identifies the object whose metadata is read.
type HeadObjectInput struct {
	// Bucket is the bucket name; blank uses the connection's defaultBucket.
	Bucket string `json:"bucket,omitempty"`
	// Key is the object key, such as reports/2026-09.md.
	Key string `json:"key"`
}

// ObjectMetadata is the metadata S3 reports for one object, without its content.
type ObjectMetadata struct {
	// Bucket is the object's bucket.
	Bucket string `json:"bucket"`
	// Key is the object key.
	Key string `json:"key"`
	// SizeBytes is the object size.
	SizeBytes int64 `json:"sizeBytes"`
	// ETag is the entity tag exactly as S3 reports it, including its double quotes. For a single-request
	// upload without SSE-KMS or SSE-C encryption it is the hex MD5 of the content.
	ETag string `json:"eTag,omitempty"`
	// ContentType is the stored Content-Type, such as text/markdown; charset=utf-8.
	ContentType string `json:"contentType,omitempty"`
	// ContentEncoding is the stored Content-Encoding, such as gzip; empty means none.
	ContentEncoding string `json:"contentEncoding,omitempty"`
	// ContentLanguage is the stored Content-Language.
	ContentLanguage string `json:"contentLanguage,omitempty"`
	// ContentDisposition is the stored Content-Disposition.
	ContentDisposition string `json:"contentDisposition,omitempty"`
	// CacheControl is the stored Cache-Control.
	CacheControl string `json:"cacheControl,omitempty"`
	// LastModified is when the object was last written.
	LastModified time.Time `json:"lastModified,omitzero"`
	// VersionID is the object version in a versioning-enabled bucket; empty otherwise.
	VersionID string `json:"versionId,omitempty"`
	// StorageClass is the storage class; Amazon S3 reports none for STANDARD.
	StorageClass string `json:"storageClass,omitempty"`
	// ServerSideEncryption is the server-side encryption algorithm, such as AES256 or aws:kms.
	ServerSideEncryption string `json:"serverSideEncryption,omitempty"`
	// Metadata is the user-defined metadata, keyed by lowercase name without the x-amz-meta- prefix. S3
	// returns a non-ASCII value RFC 2047-encoded, and the value is kept as S3 returns it.
	Metadata map[string]string `json:"metadata,omitempty"`
	// MissingMetadataCount is the number of metadata entries S3 could not return as HTTP headers.
	MissingMetadataCount int `json:"missingMetadataCount,omitempty"`
}

// HeadObjectOperation implements the headObject connector operation.
type HeadObjectOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (HeadObjectOperation) Definition() sdkgo.QueryDefinition { return HeadObjectDefinition }

// Invoke sends one HEAD request. A HEAD response has no body, so a missing key and a missing bucket both
// select notFound. Without s3:ListBucket, Amazon S3 answers a missing key with 403, which selects
// providerRejected.
func (operation HeadObjectOperation) Invoke(call sdkgo.Call, input HeadObjectInput) sdkgo.QueryAttempt[ObjectMetadata] {
	client := operation.client
	location, err := client.validateObjectInput(input.Bucket, input.Key)
	if err != nil {
		return sdkgo.NewQueryBranch(HeadObjectBranchDefect, ObjectMetadata{}, defectFailure(headObjectOperationID, err), sdkgo.Receipt{})
	}
	session, cancel, failure := client.startSession(call, headObjectOperationID)
	if failure != nil {
		return sdkgo.NewQueryBranch(HeadObjectBranchDefect, ObjectMetadata{}, failure, sdkgo.Receipt{})
	}
	defer cancel()
	identity := ObjectMetadata{Bucket: location.bucket, Key: location.key}
	metadata, classified := client.headObject(session, headObjectOperationID, location)
	if classified != nil {
		return queryAttemptForFailure(*classified, client.receipt(call, classified.requestID, location),
			HeadObjectBranchNotFound, HeadObjectBranchProviderRejected, HeadObjectBranchDefect, identity)
	}
	if metadata.isInvalid {
		return sdkgo.NewQueryBranch(HeadObjectBranchInvalidResponse, identity, failurePointer(headObjectOperationID, sdkgo.FailureProtocol,
			"S3 returned object metadata without a valid size or modification time"), client.receipt(call, metadata.requestID, location))
	}
	return sdkgo.NewQueryBranch(HeadObjectBranchFound, metadata.value, nil, client.receipt(call, metadata.requestID, location))
}

// objectMetadataFromResponse reads HEAD or GET object headers, dropping values that repeat a secret.
func objectMetadataFromResponse(location objectLocation, response *http.Response, credentials Credentials) (ObjectMetadata, error) {
	if response.ContentLength < 0 {
		return ObjectMetadata{}, errors.New("object size is missing")
	}
	lastModified, err := http.ParseTime(response.Header.Get("Last-Modified"))
	if err != nil {
		return ObjectMetadata{}, errors.New("object modification time is missing or invalid")
	}
	header := func(name string) string { return safeHeaderValue(response.Header.Get(name), credentials) }
	metadata := ObjectMetadata{
		Bucket: location.bucket, Key: location.key, SizeBytes: response.ContentLength, ETag: header("ETag"),
		ContentType: header("Content-Type"), ContentEncoding: header("Content-Encoding"), ContentLanguage: header("Content-Language"),
		ContentDisposition: header("Content-Disposition"), CacheControl: header("Cache-Control"), LastModified: lastModified.UTC(),
		VersionID: header(versionIDHeader), StorageClass: header("X-Amz-Storage-Class"),
		ServerSideEncryption: header("X-Amz-Server-Side-Encryption"),
	}
	for name, values := range response.Header {
		if !strings.HasPrefix(name, metadataHeaderPrefix) || len(values) == 0 {
			continue
		}
		if value := safeHeaderValue(strings.Join(values, ","), credentials); value != "" {
			if metadata.Metadata == nil {
				metadata.Metadata = map[string]string{}
			}
			metadata.Metadata[strings.ToLower(strings.TrimPrefix(name, metadataHeaderPrefix))] = value
		}
	}
	if missing, err := strconv.Atoi(response.Header.Get("X-Amz-Missing-Meta")); err == nil && missing > 0 {
		metadata.MissingMetadataCount = missing
	}
	return metadata, nil
}

// safeHeaderValue keeps a bounded header value that does not repeat the connection's secrets.
func safeHeaderValue(value string, credentials Credentials) string {
	if len(value) > maxHeaderValueBytes || containsCredential(value, credentials, false) {
		return ""
	}
	return value
}
