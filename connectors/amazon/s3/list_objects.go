// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package s3

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const listObjectsOperationID = "listObjects"

// ListObjectsInput selects one page of a bucket listing. Keys are listed in UTF-8 binary order.
type ListObjectsInput struct {
	// Bucket is the bucket name; blank uses the connection's defaultBucket.
	Bucket string `json:"bucket,omitempty"`
	// Prefix limits the page to keys that begin with it, such as reports/2026/; blank lists every key.
	Prefix string `json:"prefix,omitempty"`
	// Delimiter groups keys that contain it after Prefix into CommonPrefixes, usually "/" to list one
	// folder level; blank lists every key under Prefix.
	Delimiter string `json:"delimiter,omitempty"`
	// StartAfter lists only keys after this key; blank starts at the first key.
	StartAfter string `json:"startAfter,omitempty"`
	// ContinuationToken is the NextContinuationToken of the previous page; blank reads the first page.
	ContinuationToken string `json:"continuationToken,omitempty"`
	// MaxKeys bounds the objects plus common prefixes on the page, from 1 to 1000; zero uses the
	// connection's listPageSize.
	MaxKeys int `json:"maxKeys,omitempty"`
}

// ListObjectsOutput is one page of a bucket listing.
type ListObjectsOutput struct {
	// Bucket is the listed bucket.
	Bucket string `json:"bucket"`
	// Prefix is the requested key prefix.
	Prefix string `json:"prefix,omitempty"`
	// Delimiter is the requested delimiter.
	Delimiter string `json:"delimiter,omitempty"`
	// Objects lists the page's objects in key order.
	Objects []ObjectSummary `json:"objects,omitempty"`
	// CommonPrefixes lists the folder-like key prefixes the delimiter rolled up, each ending in Delimiter.
	CommonPrefixes []string `json:"commonPrefixes,omitempty"`
	// IsTruncated reports that more keys remain after this page.
	IsTruncated bool `json:"isTruncated"`
	// NextContinuationToken continues the listing when IsTruncated is true; pass it as ContinuationToken.
	NextContinuationToken string `json:"nextContinuationToken,omitempty"`
}

// ObjectSummary is one object on a listObjects page.
type ObjectSummary struct {
	// Key is the object key.
	Key string `json:"key"`
	// SizeBytes is the object size.
	SizeBytes int64 `json:"sizeBytes"`
	// ETag is the entity tag exactly as S3 reports it, including its double quotes.
	ETag string `json:"eTag,omitempty"`
	// LastModified is when the object was last written.
	LastModified time.Time `json:"lastModified,omitzero"`
	// StorageClass is the S3 storage class, such as STANDARD.
	StorageClass string `json:"storageClass,omitempty"`
}

// ListObjectsOperation implements the listObjects connector operation.
type ListObjectsOperation struct{ client *Client }

// listBucketResult is the ListObjectsV2 response document.
type listBucketResult struct {
	XMLName               xml.Name       `xml:"ListBucketResult"`
	EncodingType          string         `xml:"EncodingType"`
	IsTruncated           bool           `xml:"IsTruncated"`
	NextContinuationToken string         `xml:"NextContinuationToken"`
	Contents              []listedObject `xml:"Contents"`
	CommonPrefixes        []listedPrefix `xml:"CommonPrefixes"`
}

type listedObject struct {
	Key          string `xml:"Key"`
	LastModified string `xml:"LastModified"`
	ETag         string `xml:"ETag"`
	Size         string `xml:"Size"`
	StorageClass string `xml:"StorageClass"`
}

type listedPrefix struct {
	Prefix string `xml:"Prefix"`
}

// Definition returns the immutable connector operation definition.
func (ListObjectsOperation) Definition() sdkgo.QueryDefinition { return ListObjectsDefinition }

// Invoke sends one ListObjectsV2 request with URL-encoded keys and returns the decoded page. An empty page
// with no further page selects notFound; an empty page that S3 marks truncated selects found.
func (operation ListObjectsOperation) Invoke(call sdkgo.Call, input ListObjectsInput) sdkgo.QueryAttempt[ListObjectsOutput] {
	client := operation.client
	bucket, maxKeys, err := operation.validateInput(input)
	if err != nil {
		return sdkgo.NewQueryBranch(ListObjectsBranchDefect, ListObjectsOutput{}, defectFailure(listObjectsOperationID, err), sdkgo.Receipt{})
	}
	session, cancel, failure := client.startSession(call, listObjectsOperationID)
	if failure != nil {
		return sdkgo.NewQueryBranch(ListObjectsBranchDefect, ListObjectsOutput{}, failure, sdkgo.Receipt{})
	}
	defer cancel()
	query := url.Values{"list-type": {"2"}, "encoding-type": {"url"}, "max-keys": {strconv.Itoa(maxKeys)}}
	for name, value := range map[string]string{
		"prefix": input.Prefix, "delimiter": input.Delimiter, "start-after": input.StartAfter, "continuation-token": input.ContinuationToken,
	} {
		if value != "" {
			query.Set(name, value)
		}
	}
	location := objectLocation{bucket: bucket}
	response, err := client.exchange(session, s3Request{method: http.MethodGet, location: location, query: query})
	if errors.Is(err, errRequestNotBuilt) {
		return sdkgo.NewQueryBranch(ListObjectsBranchDefect, ListObjectsOutput{}, failurePointer(listObjectsOperationID, sdkgo.FailureLocalDefect, err.Error()), sdkgo.Receipt{})
	}
	if err != nil {
		return sdkgo.NewQueryRetry[ListObjectsOutput](transportFailure(listObjectsOperationID, "list"), 0)
	}
	defer closeResponseBody(response)
	if !isSuccessStatus(response.StatusCode) {
		classified := classifyErrorResponse(listObjectsOperationID, "list", client.readErrorResponse(response, session.credentials), false)
		// A missing bucket is a rejection, never an empty listing, so no failure maps to notFound.
		return queryAttemptForFailure(classified, client.receipt(call, classified.requestID, location), "", ListObjectsBranchProviderRejected, ListObjectsBranchDefect, ListObjectsOutput{Bucket: bucket})
	}
	receipt := client.receipt(call, safeRequestID(response.Header), location)
	contents, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return sdkgo.NewQueryBranch(ListObjectsBranchInvalidResponse, ListObjectsOutput{Bucket: bucket}, failurePointer(listObjectsOperationID, sdkgo.FailureResponseTooLarge,
			"S3 list page exceeds maxResponseBytes; lower maxKeys"), receipt)
	}
	if err != nil {
		return sdkgo.NewQueryRetry[ListObjectsOutput](transportFailure(listObjectsOperationID, "list"), 0)
	}
	if containsCredential(string(contents), session.credentials, false) {
		return sdkgo.NewQueryBranch(ListObjectsBranchInvalidResponse, ListObjectsOutput{Bucket: bucket}, failurePointer(listObjectsOperationID, sdkgo.FailureProtocol,
			"S3 list page contains the connection's credentials"), receipt)
	}
	output, err := decodeListBucketResult(contents)
	if err != nil {
		return sdkgo.NewQueryBranch(ListObjectsBranchInvalidResponse, ListObjectsOutput{Bucket: bucket}, failurePointer(listObjectsOperationID, sdkgo.FailureProtocol,
			"S3 returned an invalid list page: "+err.Error()), receipt)
	}
	output.Bucket, output.Prefix, output.Delimiter = bucket, input.Prefix, input.Delimiter
	if len(output.Objects) == 0 && len(output.CommonPrefixes) == 0 && !output.IsTruncated {
		return sdkgo.NewQueryBranch(ListObjectsBranchNotFound, output, failurePointer(listObjectsOperationID, sdkgo.FailureNotFound,
			"no object matched the prefix and no further page remains"), receipt)
	}
	return sdkgo.NewQueryBranch(ListObjectsBranchFound, output, nil, receipt)
}

// validateInput resolves the bucket and the page size and checks every listing parameter.
func (operation ListObjectsOperation) validateInput(input ListObjectsInput) (string, int, error) {
	bucket, err := operation.client.resolveBucket(input.Bucket)
	if err != nil {
		return "", 0, err
	}
	maxKeys := input.MaxKeys
	if maxKeys == 0 {
		maxKeys = operation.client.listPageSize
	}
	if maxKeys < 1 || maxKeys > maxListKeys {
		return "", 0, fmt.Errorf("maxKeys must be from 1 to %d, or zero for the connection's listPageSize", maxListKeys)
	}
	for _, check := range []struct{ value, field string }{
		{input.Prefix, "prefix"}, {input.Delimiter, "delimiter"}, {input.StartAfter, "startAfter"},
	} {
		if err := validateKeyPrefix(check.value, check.field); err != nil {
			return "", 0, err
		}
	}
	if err := validateContinuationToken(input.ContinuationToken); err != nil {
		return "", 0, err
	}
	return bucket, maxKeys, nil
}

// decodeListBucketResult validates an untrusted ListObjectsV2 document and decodes URL-encoded keys.
func decodeListBucketResult(contents []byte) (ListObjectsOutput, error) {
	var document listBucketResult
	decoder := xml.NewDecoder(bytes.NewReader(contents))
	if err := decoder.Decode(&document); err != nil {
		return ListObjectsOutput{}, errors.New("the document is not a ListBucketResult")
	}
	isURLEncoded := document.EncodingType == "url"
	output := ListObjectsOutput{IsTruncated: document.IsTruncated, NextContinuationToken: document.NextContinuationToken}
	if output.IsTruncated && output.NextContinuationToken == "" {
		return ListObjectsOutput{}, errors.New("a truncated page has no continuation token")
	}
	if err := validateContinuationToken(output.NextContinuationToken); err != nil {
		return ListObjectsOutput{}, errors.New("the continuation token is not printable")
	}
	for _, listed := range document.Contents {
		key, err := decodeListedKey(listed.Key, isURLEncoded)
		if err != nil || key == "" {
			return ListObjectsOutput{}, errors.New("an object key is missing or not UTF-8")
		}
		size, err := strconv.ParseInt(listed.Size, 10, 64)
		if err != nil || size < 0 {
			return ListObjectsOutput{}, errors.New("an object size is not a non-negative integer")
		}
		summary := ObjectSummary{Key: key, SizeBytes: size, ETag: listed.ETag, StorageClass: listed.StorageClass}
		if listed.LastModified != "" {
			if summary.LastModified, err = time.Parse(time.RFC3339, listed.LastModified); err != nil {
				return ListObjectsOutput{}, errors.New("an object modification time is not an ISO 8601 timestamp")
			}
		}
		output.Objects = append(output.Objects, summary)
	}
	for _, listed := range document.CommonPrefixes {
		prefix, err := decodeListedKey(listed.Prefix, isURLEncoded)
		if err != nil || prefix == "" {
			return ListObjectsOutput{}, errors.New("a common prefix is missing or not UTF-8")
		}
		output.CommonPrefixes = append(output.CommonPrefixes, prefix)
	}
	return output, nil
}

// decodeListedKey undoes encoding-type=url, which S3 applies as form encoding, so "+" is a space.
func decodeListedKey(value string, isURLEncoded bool) (string, error) {
	if isURLEncoded {
		decoded, err := url.QueryUnescape(value)
		if err != nil {
			return "", err
		}
		value = decoded
	}
	if !utf8.ValidString(value) {
		return "", errors.New("key is not UTF-8")
	}
	return value, nil
}
