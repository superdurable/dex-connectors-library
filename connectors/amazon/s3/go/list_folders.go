// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package s3

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const listFoldersOperationID = "listFolders"

// ListFoldersInput selects one page of immediate child prefixes in a bucket.
type ListFoldersInput struct {
	// Bucket is the bucket; blank uses the connection default.
	Bucket string `json:"bucket,omitempty"`
	// ParentPrefix is the parent folder path, ending in a slash; blank lists the root.
	ParentPrefix string `json:"parentPrefix,omitempty"`
	// ContinuationToken resumes a prior page; blank begins at the start.
	ContinuationToken string `json:"continuationToken,omitempty"`
	// MaxKeys is 1 to 1000; zero uses the connection listPageSize.
	MaxKeys int `json:"maxKeys,omitempty"`
}

// ListFoldersOutput contains immediate child folder prefixes and the next page token.
type ListFoldersOutput struct {
	// Bucket is the bucket.
	Bucket string `json:"bucket"`
	// ParentPrefix is the requested parent prefix.
	ParentPrefix string `json:"parentPrefix,omitempty"`
	// Folders are complete child prefixes ending in a slash.
	Folders []string `json:"folders,omitempty"`
	// NextContinuationToken resumes a truncated page.
	NextContinuationToken string `json:"nextContinuationToken,omitempty"`
	// IsTruncated reports that more pages remain.
	IsTruncated bool `json:"isTruncated"`
}

// ListFoldersOperation implements listFolders.
type ListFoldersOperation struct{ client *Client }

// Definition returns the generated Query contract.
func (ListFoldersOperation) Definition() sdkgo.QueryDefinition { return ListFoldersDefinition }

// Invoke requests a ListObjectsV2 page with slash as the delimiter.
func (operation ListFoldersOperation) Invoke(call sdkgo.Call, input ListFoldersInput) sdkgo.QueryAttempt[ListFoldersOutput] {
	client := operation.client
	bucket, err := client.resolveBucket(input.Bucket)
	if err != nil {
		return folderListDefect(err)
	}
	if err := validateKeyPrefix(input.ParentPrefix, "parentPrefix"); err != nil {
		return folderListDefect(err)
	}
	if input.ParentPrefix != "" && !strings.HasSuffix(input.ParentPrefix, "/") {
		return folderListDefect(errors.New("parentPrefix must end with a slash"))
	}
	if err := validateContinuationToken(input.ContinuationToken); err != nil {
		return folderListDefect(err)
	}
	maxKeys := input.MaxKeys
	if maxKeys == 0 {
		maxKeys = client.listPageSize
	}
	if maxKeys < 1 || maxKeys > 1000 {
		return folderListDefect(errors.New("maxKeys must be 1 to 1000"))
	}
	session, cancel, failure := client.startSession(call, listFoldersOperationID)
	if failure != nil {
		return sdkgo.NewQueryBranch(ListFoldersBranchDefect, ListFoldersOutput{}, failure, sdkgo.Receipt{})
	}
	defer cancel()
	output := ListFoldersOutput{Bucket: bucket, ParentPrefix: input.ParentPrefix}
	query := url.Values{"list-type": {"2"}, "encoding-type": {"url"}, "delimiter": {"/"}, "max-keys": {strconv.Itoa(maxKeys)}}
	if input.ParentPrefix != "" {
		query.Set("prefix", input.ParentPrefix)
	}
	if input.ContinuationToken != "" {
		query.Set("continuation-token", input.ContinuationToken)
	}
	location := objectLocation{bucket: bucket}
	response, err := client.exchange(session, s3Request{method: http.MethodGet, location: location, query: query})
	if errors.Is(err, errRequestNotBuilt) {
		return sdkgo.NewQueryBranch(ListFoldersBranchDefect, output, failurePointer(listFoldersOperationID, sdkgo.FailureLocalDefect, err.Error()), sdkgo.Receipt{})
	}
	if err != nil {
		return sdkgo.NewQueryRetry[ListFoldersOutput](transportFailure(listFoldersOperationID, "folder listing"), 0)
	}
	defer closeResponseBody(response)
	if !isSuccessStatus(response.StatusCode) {
		classified := classifyErrorResponse(listFoldersOperationID, "folder listing", client.readErrorResponse(response, session.credentials), false)
		return queryAttemptForFailure(classified, client.receipt(call, classified.requestID, location), "", ListFoldersBranchProviderRejected, ListFoldersBranchDefect, output)
	}
	receipt := client.receipt(call, safeRequestID(response.Header), location)
	contents, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return sdkgo.NewQueryBranch(ListFoldersBranchInvalidResponse, output, failurePointer(listFoldersOperationID, sdkgo.FailureResponseTooLarge, "S3 folder list exceeds maxResponseBytes"), receipt)
	}
	if err != nil {
		return sdkgo.NewQueryRetry[ListFoldersOutput](transportFailure(listFoldersOperationID, "folder listing"), 0)
	}
	if containsCredential(string(contents), session.credentials, true) {
		return sdkgo.NewQueryBranch(ListFoldersBranchInvalidResponse, output, failurePointer(listFoldersOperationID, sdkgo.FailureProtocol, "S3 folder list contains connection credentials"), receipt)
	}
	page, err := decodeListBucketResult(contents)
	if err != nil {
		return sdkgo.NewQueryBranch(ListFoldersBranchInvalidResponse, output, failurePointer(listFoldersOperationID, sdkgo.FailureProtocol, "S3 returned an invalid folder list"), receipt)
	}
	output.Folders, output.IsTruncated, output.NextContinuationToken = page.CommonPrefixes, page.IsTruncated, page.NextContinuationToken
	for _, folder := range output.Folders {
		if !strings.HasPrefix(folder, input.ParentPrefix) || !strings.HasSuffix(folder, "/") {
			return sdkgo.NewQueryBranch(ListFoldersBranchInvalidResponse, output, failurePointer(listFoldersOperationID, sdkgo.FailureProtocol, "S3 returned a folder outside the parent prefix"), receipt)
		}
	}
	if len(output.Folders) == 0 && !output.IsTruncated {
		return sdkgo.NewQueryBranch(ListFoldersBranchNotFound, output, failurePointer(listFoldersOperationID, sdkgo.FailureNotFound, "no child folder matched the parent prefix"), receipt)
	}
	return sdkgo.NewQueryBranch(ListFoldersBranchListed, output, nil, receipt)
}

func folderListDefect(err error) sdkgo.QueryAttempt[ListFoldersOutput] {
	return sdkgo.NewQueryBranch(ListFoldersBranchDefect, ListFoldersOutput{}, defectFailure(listFoldersOperationID, err), sdkgo.Receipt{})
}
