// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package azureblob

import (
	"encoding/base64"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

var containerPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$`)

// GetBlobInput names one blob within a lowercase Azure Storage container.
type GetBlobInput struct {
	Container string `json:"container"`
	BlobName  string `json:"blobName"`
}

// BlobDownload holds bounded blob bytes as base64 and non-secret response metadata.
type BlobDownload struct {
	DataBase64  string `json:"dataBase64"`
	ContentType string `json:"contentType"`
	ETag        string `json:"etag"`
}

// GetBlobOperation downloads one complete Azure Storage blob.
type GetBlobOperation struct{ client *Client }

// GetBlob binds the Query to this client.
func (client *Client) GetBlob() GetBlobOperation { return GetBlobOperation{client} }

// Definition returns the generated Query contract.
func (GetBlobOperation) Definition() sdkgo.QueryDefinition { return GetBlobDefinition }

// Invoke signs a bounded download and classifies its outcome.
func (operation GetBlobOperation) Invoke(call sdkgo.Call, input GetBlobInput) sdkgo.QueryAttempt[BlobDownload] {
	if !containerPattern.MatchString(input.Container) || strings.Contains(input.Container, "--") || !isValidBlobName(input.BlobName) {
		return blobBranch(GetBlobBranchDefect, sdkgo.FailureValidation, "Azure Blob container or blob name is invalid")
	}
	response := operation.client.downloadBlob(call, input.Container, input.BlobName)
	if errors.Is(response.err, errCredential) {
		return blobBranch(GetBlobBranchDefect, sdkgo.FailureAuthentication, "Azure Blob account key is unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[BlobDownload](blobFailure(sdkgo.FailureTransport, "Azure Blob request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[BlobDownload](blobFailure(sdkgo.FailureRateLimit, "Azure Blob rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[BlobDownload](blobFailure(sdkgo.FailureAvailability, "Azure Blob is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return sdkgo.NewQueryBranch(GetBlobBranchMissing, BlobDownload{}, nil, sdkgo.Receipt{})
	}
	if response.status != http.StatusOK {
		return blobBranch(GetBlobBranchProviderRejected, sdkgo.FailureProviderRejection, "Azure Blob rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return blobBranch(GetBlobBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Azure Blob body exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[BlobDownload](blobFailure(sdkgo.FailureTransport, "Azure Blob response was interrupted"), 0)
	}
	if !isSafeResponseHeader(response.contentType) || !isSafeResponseHeader(response.etag) {
		return blobBranch(GetBlobBranchInvalidResponse, sdkgo.FailureProtocol, "Azure Blob returned invalid response headers")
	}
	return sdkgo.NewQueryBranch(GetBlobBranchDownloaded, BlobDownload{DataBase64: base64.StdEncoding.EncodeToString(response.body), ContentType: response.contentType, ETag: response.etag}, nil, sdkgo.Receipt{})
}

func isValidBlobName(name string) bool {
	if name == "" || len(name) > 1024 || strings.ContainsAny(name, "\x00\r\n") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}
func isSafeResponseHeader(value string) bool {
	return len(value) <= 255 && !strings.ContainsAny(value, "\x00\r\n")
}
func blobFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "microsoft-azure-blob", Operation: "getBlob", Message: message}
}
func blobBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[BlobDownload] {
	failure := blobFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, BlobDownload{}, &failure, sdkgo.Receipt{})
}
