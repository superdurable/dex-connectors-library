// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package dropbox

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// DownloadFileInput selects one Dropbox file by path, ID, or revision.
type DownloadFileInput struct {
	// Path is an absolute Dropbox path or a stable id: or rev: identifier.
	Path string `json:"path"`
}

// DownloadedFile contains bounded Base64 content and provider file identity.
type DownloadedFile struct {
	// File is Dropbox's metadata for the downloaded file.
	File EntryMetadata `json:"file"`
	// ContentBase64 is the downloaded byte sequence encoded as Base64.
	ContentBase64 string `json:"contentBase64"`
	// ContentBytes is the number of decoded bytes in ContentBase64.
	ContentBytes int `json:"contentBytes"`
}

// DownloadFileOperation reads bounded file content from Dropbox.
type DownloadFileOperation struct{ client *Client }

// DownloadFile binds the download Query to this client.
func (client *Client) DownloadFile() DownloadFileOperation { return DownloadFileOperation{client} }

// Definition returns the generated download Query contract.
func (DownloadFileOperation) Definition() sdkgo.QueryDefinition { return DownloadFileDefinition }

// Invoke downloads one file and checks its bounded body and metadata header.
func (operation DownloadFileOperation) Invoke(call sdkgo.Call, input DownloadFileInput) sdkgo.QueryAttempt[DownloadedFile] {
	branches := queryBranches{found: DownloadFileBranchDownloaded, notFound: DownloadFileBranchNotFound, rejected: DownloadFileBranchProviderRejected, invalid: DownloadFileBranchInvalidResponse, defect: DownloadFileBranchDefect}
	if !validDropboxPath(input.Path, false) {
		return queryBranch("downloadFile", branches.defect, DownloadedFile{}, sdkgo.FailureValidation, "file path is invalid")
	}
	response := operation.client.download(call, input.Path)
	if errors.Is(response.err, errCredential) {
		return queryBranch("downloadFile", branches.defect, DownloadedFile{}, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRefresh) {
		return sdkgo.NewQueryRetry[DownloadedFile](queryFailure("downloadFile", sdkgo.FailureAvailability, "Dropbox credentials could not be refreshed"), 0)
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[DownloadedFile](queryFailure("downloadFile", sdkgo.FailureTransport, "Dropbox download request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[DownloadedFile](queryFailure("downloadFile", sdkgo.FailureRateLimit, "Dropbox rate limited the download"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[DownloadedFile](queryFailure("downloadFile", sdkgo.FailureAvailability, "Dropbox download is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusConflict {
		var providerError struct {
			ErrorSummary string `json:"error_summary"`
		}
		if json.Unmarshal(response.body, &providerError) == nil && strings.HasPrefix(providerError.ErrorSummary, "path/not_found") {
			return queryBranch("downloadFile", branches.notFound, DownloadedFile{}, sdkgo.FailureNotFound, "Dropbox did not find the file")
		}
	}
	if response.status != http.StatusOK {
		return queryBranch("downloadFile", branches.rejected, DownloadedFile{}, sdkgo.FailureProviderRejection, "Dropbox rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return queryBranch("downloadFile", DownloadFileBranchTooLarge, DownloadedFile{}, sdkgo.FailureResponseTooLarge, "Dropbox file exceeds the configured download limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[DownloadedFile](queryFailure("downloadFile", sdkgo.FailureTransport, "Dropbox download response was interrupted"), 0)
	}
	file, err := decodeEntry([]byte(response.metadata))
	if err != nil || file.Kind != "file" {
		return queryBranch("downloadFile", branches.invalid, DownloadedFile{}, sdkgo.FailureProtocol, "Dropbox returned invalid file metadata")
	}
	output := DownloadedFile{File: file, ContentBase64: base64.StdEncoding.EncodeToString(response.body), ContentBytes: len(response.body)}
	return sdkgo.NewQueryBranch(branches.found, output, nil, sdkgo.Receipt{})
}
