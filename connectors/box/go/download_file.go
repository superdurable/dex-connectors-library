// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package box

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// DownloadFileInput selects one Box file by its numeric ID.
type DownloadFileInput struct {
	// FileID appears in Box Web's /files/<ID> URL.
	FileID string `json:"fileId"`
}

// DownloadedFile contains bounded Base64 content and its Box file ID.
type DownloadedFile struct {
	// FileID is the Box file ID requested by the Flow.
	FileID string `json:"fileId"`
	// ContentBase64 is the downloaded byte sequence encoded as Base64.
	ContentBase64 string `json:"contentBase64"`
	// ContentBytes is the number of decoded bytes in ContentBase64.
	ContentBytes int `json:"contentBytes"`
}

// DownloadFileOperation reads bounded Box file content.
type DownloadFileOperation struct{ client *Client }

// DownloadFile binds the download Query to this client.
func (client *Client) DownloadFile() DownloadFileOperation { return DownloadFileOperation{client} }

// Definition returns the generated download Query contract.
func (DownloadFileOperation) Definition() sdkgo.QueryDefinition { return DownloadFileDefinition }

// Invoke downloads one file and classifies retryable and definitive provider outcomes.
func (operation DownloadFileOperation) Invoke(call sdkgo.Call, input DownloadFileInput) sdkgo.QueryAttempt[DownloadedFile] {
	const operationName = "downloadFile"
	if input.FileID == "" || len(input.FileID) > 40 || strings.Trim(input.FileID, "0123456789") != "" {
		return queryBranch(operationName, DownloadFileBranchDefect, DownloadedFile{}, sdkgo.FailureValidation, "Box file ID must contain only decimal digits")
	}
	response := operation.client.download(call, input.FileID)
	if errors.Is(response.err, errCredential) {
		return queryBranch(operationName, DownloadFileBranchDefect, DownloadedFile{}, sdkgo.FailureAuthentication, "Box credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRefresh) {
		return sdkgo.NewQueryRetry[DownloadedFile](queryFailure(operationName, sdkgo.FailureAvailability, "Box credentials could not be refreshed"), 0)
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[DownloadedFile](queryFailure(operationName, sdkgo.FailureTransport, "Box download request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[DownloadedFile](queryFailure(operationName, sdkgo.FailureRateLimit, "Box rate limited the download"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[DownloadedFile](queryFailure(operationName, sdkgo.FailureAvailability, "Box download is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusAccepted {
		return sdkgo.NewQueryRetry[DownloadedFile](queryFailure(operationName, sdkgo.FailureAvailability, "Box file is not ready for download"), response.retryAfter)
	}
	if errors.Is(response.err, errUnsafeRedirect) {
		return queryBranch(operationName, DownloadFileBranchInvalidResponse, DownloadedFile{}, sdkgo.FailureProtocol, "Box returned an unsafe download redirect")
	}
	if response.status != http.StatusOK && errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return queryBranch(operationName, DownloadFileBranchInvalidResponse, DownloadedFile{}, sdkgo.FailureResponseTooLarge, "Box error response exceeds the configured limit")
	}
	if response.status == http.StatusNotFound {
		return queryBranch(operationName, DownloadFileBranchNotFound, DownloadedFile{}, sdkgo.FailureNotFound, "Box did not find the file")
	}
	if response.status != http.StatusOK {
		return queryBranch(operationName, DownloadFileBranchProviderRejected, DownloadedFile{}, sdkgo.FailureProviderRejection, "Box rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return queryBranch(operationName, DownloadFileBranchTooLarge, DownloadedFile{}, sdkgo.FailureResponseTooLarge, "Box file exceeds the configured download limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[DownloadedFile](queryFailure(operationName, sdkgo.FailureTransport, "Box download response was interrupted"), 0)
	}
	output := DownloadedFile{FileID: input.FileID, ContentBase64: base64.StdEncoding.EncodeToString(response.body), ContentBytes: len(response.body)}
	return sdkgo.NewQueryBranch(DownloadFileBranchDownloaded, output, nil, sdkgo.Receipt{})
}
