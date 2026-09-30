// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package drive

import (
	"encoding/json"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	readFileTextOperationID       = "readFileText"
	readFileTextMetadataFields    = "id,name,mimeType,size"
	googleWorkspaceMimeTypePrefix = "application/vnd.google-apps."
	utf8ByteOrderMark             = "\xef\xbb\xbf"
)

// googleWorkspaceTextExports maps each Google Workspace type with a text export to its export MIME type.
// Google exports only the first sheet of a spreadsheet as CSV.
var googleWorkspaceTextExports = map[string]string{
	"application/vnd.google-apps.document":     "text/plain",
	"application/vnd.google-apps.spreadsheet":  "text/csv",
	"application/vnd.google-apps.presentation": "text/plain",
}

// downloadableTextMimeTypes lists non-text/* MIME types whose stored content is text.
var downloadableTextMimeTypes = map[string]bool{
	"application/json":       true,
	"application/ld+json":    true,
	"application/xml":        true,
	"application/yaml":       true,
	"application/x-yaml":     true,
	"application/x-ndjson":   true,
	"application/javascript": true,
	"application/sql":        true,
}

// ReadFileTextInput identifies the Drive file whose text is read.
type ReadFileTextInput struct {
	// FileID is the Drive file ID, such as a SearchFiles result ID.
	FileID string `json:"fileId"`
}

// ReadFileTextOutput is the bounded UTF-8 text of one file. The unsupportedContent
// and tooLarge branches return the file identity without Text.
type ReadFileTextOutput struct {
	// FileID is the Drive file ID that was read.
	FileID string `json:"fileId"`
	// Name is the file title shown in Google Drive.
	Name string `json:"name"`
	// MimeType is the file's Drive MIME type.
	MimeType string `json:"mimeType"`
	// TextMimeType is text/plain for exported Docs and Slides, text/csv for the
	// first sheet of an exported Sheets file, or the downloaded file's own type.
	TextMimeType string `json:"textMimeType,omitempty"`
	// Text is the complete file text without a leading UTF-8 byte order mark.
	Text string `json:"text,omitempty"`
	// ByteCount is the UTF-8 byte length of Text.
	ByteCount int64 `json:"byteCount"`
}

// ReadFileTextOperation implements the readFileText connector operation.
type ReadFileTextOperation struct{ client *Client }

var readFileTextMetadataBranches = readBranches{
	operationID: readFileTextOperationID, notFound: ReadFileTextBranchNotFound,
	tooLarge: ReadFileTextBranchInvalidResponse, providerRejected: ReadFileTextBranchProviderRejected,
	invalidResponse: ReadFileTextBranchInvalidResponse, defect: ReadFileTextBranchDefect,
}

var readFileTextContentBranches = readBranches{
	operationID: readFileTextOperationID, notFound: ReadFileTextBranchNotFound,
	tooLarge: ReadFileTextBranchTooLarge, providerRejected: ReadFileTextBranchProviderRejected,
	invalidResponse: ReadFileTextBranchInvalidResponse, defect: ReadFileTextBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (ReadFileTextOperation) Definition() sdkgo.QueryDefinition { return ReadFileTextDefinition }

// Invoke reads the file's metadata, then exports or downloads its text. A
// type without text selects unsupportedContent, and a file whose reported
// size already exceeds maxTextBytes selects tooLarge, without a content request.
func (operation ReadFileTextOperation) Invoke(call sdkgo.Call, input ReadFileTextInput) sdkgo.QueryAttempt[ReadFileTextOutput] {
	client := operation.client
	if !isDriveID(input.FileID) {
		return sdkgo.NewQueryBranch(ReadFileTextBranchDefect, ReadFileTextOutput{}, driveFailurePointer(sdkgo.FailureValidation, readFileTextOperationID, "fileId must be a Drive file ID"), sdkgo.Receipt{})
	}
	credential, failure := client.resolveCredential(call, readFileTextOperationID)
	if failure != nil {
		return sdkgo.NewQueryBranch(ReadFileTextBranchDefect, ReadFileTextOutput{}, failure, sdkgo.Receipt{})
	}
	metadataResponse, outcome := client.sendRead(call, &credential, readFileTextMetadataBranches, driveRequest{
		method: http.MethodGet, target: client.fileMetadataURL(input.FileID, readFileTextMetadataFields), responseLimit: client.maxResponseBytes,
	})
	if outcome != nil {
		return queryAttemptFromReadOutcome[ReadFileTextOutput](outcome)
	}
	metadataReceipt := client.receipt(call, metadataResponse.requestID, input.FileID)
	var resource driveFileResource
	if err := json.Unmarshal(metadataResponse.body, &resource); err != nil {
		return sdkgo.NewQueryBranch(ReadFileTextBranchInvalidResponse, ReadFileTextOutput{}, driveFailurePointer(sdkgo.FailureProtocol, readFileTextOperationID, "provider returned an invalid file response"), metadataReceipt)
	}
	file, err := convertFileResource(resource)
	if err != nil {
		return sdkgo.NewQueryBranch(ReadFileTextBranchInvalidResponse, ReadFileTextOutput{}, driveFailurePointer(sdkgo.FailureProtocol, readFileTextOperationID, "provider returned an invalid file response"), metadataReceipt)
	}
	output := ReadFileTextOutput{FileID: file.ID, Name: file.Name, MimeType: file.MimeType}
	contentURL := ""
	if exportMimeType, isExportable := googleWorkspaceTextExports[file.MimeType]; isExportable {
		output.TextMimeType = exportMimeType
		contentURL = client.fileExportURL(file.ID, exportMimeType)
	} else if isDownloadableTextMimeType(file.MimeType) {
		if file.SizeBytes > client.maxTextBytes {
			return sdkgo.NewQueryBranch(ReadFileTextBranchTooLarge, output, driveFailurePointer(sdkgo.FailureResponseTooLarge, readFileTextOperationID, "file size exceeds the configured text limit"), metadataReceipt)
		}
		output.TextMimeType = file.MimeType
		contentURL = client.fileContentURL(file.ID)
	} else {
		return sdkgo.NewQueryBranch(ReadFileTextBranchUnsupportedContent, output, driveFailurePointer(sdkgo.FailureValidation, readFileTextOperationID, unsupportedContentMessage(file.MimeType)), metadataReceipt)
	}
	contentResponse, outcome := client.sendRead(call, &credential, readFileTextContentBranches, driveRequest{
		method: http.MethodGet, target: contentURL, responseLimit: client.maxTextBytes,
	})
	if outcome != nil && outcome.branch == "" {
		return sdkgo.NewQueryRetry[ReadFileTextOutput](outcome.failure, outcome.retryAfter)
	}
	if outcome != nil {
		output.TextMimeType = ""
		failure := outcome.failure
		return sdkgo.NewQueryBranch(outcome.branch, output, &failure, outcome.receipt)
	}
	contentReceipt := client.receipt(call, contentResponse.requestID, file.ID)
	text := strings.TrimPrefix(string(contentResponse.body), utf8ByteOrderMark)
	if !utf8.ValidString(text) || strings.IndexByte(text, 0) >= 0 {
		output.TextMimeType = ""
		return sdkgo.NewQueryBranch(ReadFileTextBranchUnsupportedContent, output, driveFailurePointer(sdkgo.FailureValidation, readFileTextOperationID, "file content is not valid UTF-8 text"), contentReceipt)
	}
	output.Text = text
	output.ByteCount = int64(len(text))
	return sdkgo.NewQueryBranch(ReadFileTextBranchRead, output, nil, contentReceipt)
}

func isDownloadableTextMimeType(mimeType string) bool {
	return strings.HasPrefix(mimeType, "text/") || downloadableTextMimeTypes[mimeType]
}

func unsupportedContentMessage(mimeType string) string {
	switch {
	case mimeType == "application/vnd.google-apps.folder":
		return "file is a folder"
	case mimeType == "application/vnd.google-apps.shortcut":
		return "file is a shortcut; read its target file ID from getFile"
	case strings.HasPrefix(mimeType, googleWorkspaceMimeTypePrefix):
		return "Google Workspace file type has no text export"
	default:
		return "file type is not text"
	}
}
