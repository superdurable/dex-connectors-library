// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package onedrive

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	readFileTextOperationID = "readFileText"
	readFileTextItemFields  = "id,name,size,file,folder,package"
)

// textMimeTypes lists non-text/* media types whose stored content is text.
var textMimeTypes = map[string]bool{
	"application/json": true, "application/ld+json": true, "application/xml": true, "application/yaml": true,
	"application/x-yaml": true, "application/x-ndjson": true, "application/javascript": true, "application/sql": true,
	"application/x-sh": true, "application/toml": true,
}

// textFileExtensions identify text files that Graph labels with a generic media type.
var textFileExtensions = map[string]bool{
	".txt": true, ".md": true, ".markdown": true, ".csv": true, ".tsv": true, ".json": true, ".ndjson": true,
	".xml": true, ".yaml": true, ".yml": true, ".toml": true, ".ini": true, ".cfg": true, ".conf": true,
	".log": true, ".sql": true, ".html": true, ".htm": true, ".css": true, ".js": true, ".ts": true,
	".py": true, ".go": true, ".sh": true,
}

// genericMimeTypes are labels that say nothing about whether content is text.
var genericMimeTypes = map[string]bool{"": true, "application/octet-stream": true}

// ReadFileTextInput identifies the file whose text is read.
type ReadFileTextInput struct {
	// DriveID is the drive that holds the file; blank means the signed-in
	// user's OneDrive and is invalid for app-only connections.
	DriveID string `json:"driveId,omitempty"`
	// ItemID is the file's drive item ID, such as a SearchFiles result ID.
	ItemID string `json:"itemId"`
}

// ReadFileTextOutput is the bounded UTF-8 text of one file. The
// unsupportedContent and tooLarge branches return the file identity without Text.
type ReadFileTextOutput struct {
	// ItemID is the drive item ID that was read.
	ItemID string `json:"itemId"`
	// Name is the file name.
	Name string `json:"name"`
	// MimeType is the media type Graph reports for the file.
	MimeType string `json:"mimeType,omitempty"`
	// SizeBytes is the file size Graph reports.
	SizeBytes int64 `json:"sizeBytes"`
	// Text is the complete file content, unchanged, including any byte order mark.
	Text string `json:"text,omitempty"`
	// ByteCount is the UTF-8 byte length of Text.
	ByteCount int64 `json:"byteCount"`
}

// ReadFileTextOperation implements the readFileText connector operation.
type ReadFileTextOperation struct{ client *Client }

var readFileTextReadBranches = readBranches{
	operationID: readFileTextOperationID, notFound: ReadFileTextBranchNotFound,
	providerRejected: ReadFileTextBranchProviderRejected, invalidResponse: ReadFileTextBranchInvalidResponse,
	defect: ReadFileTextBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (ReadFileTextOperation) Definition() sdkgo.QueryDefinition { return ReadFileTextDefinition }

// Invoke reads the item's metadata, then downloads its content. Graph answers
// the content request with a redirect to a short-lived pre-authenticated URL;
// the connector follows it once without the access token and never returns or
// logs that URL. A folder, package, or non-text file selects
// unsupportedContent, and a file larger than maxTextBytes selects tooLarge,
// without a content request.
func (operation ReadFileTextOperation) Invoke(call sdkgo.Call, input ReadFileTextInput) sdkgo.QueryAttempt[ReadFileTextOutput] {
	client := operation.client
	if err := validateDriveAndFolderIDs(input.DriveID, input.ItemID); err != nil || input.ItemID == "" || input.ItemID == rootFolderAlias {
		return sdkgo.NewQueryBranch(ReadFileTextBranchDefect, ReadFileTextOutput{}, graphFailurePointer(sdkgo.FailureValidation, readFileTextOperationID, "itemId must be a Microsoft Graph file item ID, and driveId a drive ID or blank"), sdkgo.Receipt{})
	}
	credential, failure := client.resolveCredential(call, readFileTextOperationID, input.DriveID)
	if failure != nil {
		return sdkgo.NewQueryBranch(ReadFileTextBranchDefect, ReadFileTextOutput{}, failure, sdkgo.Receipt{})
	}
	itemURL := client.itemURL(input.DriveID, input.ItemID)
	metadataResponse, outcome := client.sendRead(call, &credential, readFileTextReadBranches, graphRequest{
		method: http.MethodGet, target: itemURL + "?" + buildODataQuery([2]string{"$select", readFileTextItemFields}),
		responseLimit: client.maxResponseBytes,
	})
	if outcome != nil {
		return queryAttemptFromReadOutcome[ReadFileTextOutput](outcome)
	}
	metadataReceipt := client.receipt(call, metadataResponse.requestID, input.ItemID)
	item, err := decodeItemResponse(metadataResponse.body)
	if err != nil {
		return sdkgo.NewQueryBranch(ReadFileTextBranchInvalidResponse, ReadFileTextOutput{}, graphFailurePointer(sdkgo.FailureProtocol, readFileTextOperationID, "provider returned an invalid item response"), metadataReceipt)
	}
	output := ReadFileTextOutput{ItemID: item.ID, Name: item.Name, MimeType: item.MimeType, SizeBytes: item.SizeBytes}
	if reason := unsupportedContentReason(item); reason != "" {
		return sdkgo.NewQueryBranch(ReadFileTextBranchUnsupportedContent, output, graphFailurePointer(sdkgo.FailureValidation, readFileTextOperationID, reason), metadataReceipt)
	}
	if item.SizeBytes > client.maxTextBytes {
		return sdkgo.NewQueryBranch(ReadFileTextBranchTooLarge, output, graphFailurePointer(sdkgo.FailureResponseTooLarge, readFileTextOperationID, "file size exceeds the configured text limit"), metadataReceipt)
	}
	content, contentRequestID, attempt := operation.downloadContent(call, &credential, itemURL+"/content", output)
	if attempt != nil {
		return *attempt
	}
	contentReceipt := client.receipt(call, contentRequestID, item.ID)
	text := string(content)
	if !utf8.ValidString(text) || strings.IndexByte(text, 0) >= 0 {
		return sdkgo.NewQueryBranch(ReadFileTextBranchUnsupportedContent, output, graphFailurePointer(sdkgo.FailureValidation, readFileTextOperationID, "file content is not valid UTF-8 text"), contentReceipt)
	}
	output.Text = text
	output.ByteCount = int64(len(text))
	return sdkgo.NewQueryBranch(ReadFileTextBranchRead, output, nil, contentReceipt)
}

// downloadContent follows one content redirect without credentials; a nil attempt returns the bounded file.
func (operation ReadFileTextOperation) downloadContent(
	call sdkgo.Call,
	credential *Credentials,
	contentURL string,
	output ReadFileTextOutput,
) ([]byte, string, *sdkgo.QueryAttempt[ReadFileTextOutput]) {
	client := operation.client
	response, err := client.sendRequest(call, credential, graphRequest{
		method: http.MethodGet, target: contentURL, responseLimit: client.maxTextBytes, isRawContent: true,
	})
	if attempt := operation.classifyContentResponse(call, response, err, output, false); attempt != nil {
		return nil, "", attempt
	}
	if !isRedirectStatus(response.status) {
		return response.body, response.requestID, nil
	}
	downloadURL, err := operation.validateDownloadURL(response.header.Get("Location"))
	if err != nil {
		attempt := sdkgo.NewQueryBranch(ReadFileTextBranchInvalidResponse, output, graphFailurePointer(sdkgo.FailureProtocol, readFileTextOperationID, "provider returned an unusable download redirect"), client.receipt(call, response.requestID, output.ItemID))
		return nil, "", &attempt
	}
	httpRequest, err := http.NewRequestWithContext(call.Context, http.MethodGet, downloadURL.String(), nil)
	if err != nil {
		attempt := sdkgo.NewQueryBranch(ReadFileTextBranchDefect, output, graphFailurePointer(sdkgo.FailureLocalDefect, readFileTextOperationID, "download request could not be built"), sdkgo.Receipt{})
		return nil, "", &attempt
	}
	// The download URL is pre-authenticated, so the access token is never sent to its host.
	httpResponse, err := client.httpClient.Do(httpRequest)
	if err != nil {
		attempt := sdkgo.NewQueryRetry[ReadFileTextOutput](graphFailure(sdkgo.FailureTransport, readFileTextOperationID, "file download failed"), 0)
		return nil, "", &attempt
	}
	downloadResponse, err := client.readResponse(httpResponse, client.maxTextBytes)
	downloadResponse.requestID = response.requestID
	if attempt := operation.classifyContentResponse(call, downloadResponse, err, output, true); attempt != nil {
		return nil, "", attempt
	}
	if isRedirectStatus(downloadResponse.status) {
		attempt := sdkgo.NewQueryBranch(ReadFileTextBranchInvalidResponse, output, graphFailurePointer(sdkgo.FailureProtocol, readFileTextOperationID, "file download redirected more than once"), client.receipt(call, response.requestID, output.ItemID))
		return nil, "", &attempt
	}
	return downloadResponse.body, response.requestID, nil
}

// classifyContentResponse returns nil for a 2xx or redirect response and a branch or Retry otherwise.
func (operation ReadFileTextOperation) classifyContentResponse(
	call sdkgo.Call,
	response graphResponse,
	err error,
	output ReadFileTextOutput,
	isPreauthenticatedDownload bool,
) *sdkgo.QueryAttempt[ReadFileTextOutput] {
	client := operation.client
	receipt := client.receipt(call, response.requestID, output.ItemID)
	var requestErr *graphRequestError
	switch {
	case errors.As(err, &requestErr) && requestErr.kind == sdkgo.FailureResponseTooLarge:
		attempt := sdkgo.NewQueryBranch(ReadFileTextBranchTooLarge, output, graphFailurePointer(sdkgo.FailureResponseTooLarge, readFileTextOperationID, "file content exceeds the configured text limit"), receipt)
		return &attempt
	case errors.As(err, &requestErr) && requestErr.kind == sdkgo.FailureLocalDefect:
		attempt := sdkgo.NewQueryBranch(ReadFileTextBranchDefect, output, graphFailurePointer(requestErr.kind, readFileTextOperationID, requestErr.message), receipt)
		return &attempt
	case err != nil:
		attempt := sdkgo.NewQueryRetry[ReadFileTextOutput](graphFailure(sdkgo.FailureTransport, readFileTextOperationID, "file download failed"), 0)
		return &attempt
	case response.status >= 200 && response.status < 300, isRedirectStatus(response.status):
		return nil
	case isPreauthenticatedDownload && (response.status == http.StatusUnauthorized || response.status == http.StatusForbidden):
		// The storage host rejected an expired pre-authenticated URL; the next attempt gets a fresh one.
		attempt := sdkgo.NewQueryRetry[ReadFileTextOutput](graphFailure(sdkgo.FailureAuthorization, readFileTextOperationID, "file download link expired"), 0)
		return &attempt
	}
	outcome := client.classifyFailedRead(response, readFileTextReadBranches, receipt)
	if outcome.branch == "" {
		attempt := sdkgo.NewQueryRetry[ReadFileTextOutput](outcome.failure, outcome.retryAfter)
		return &attempt
	}
	failure := outcome.failure
	attempt := sdkgo.NewQueryBranch(outcome.branch, output, &failure, outcome.receipt)
	return &attempt
}

// validateDownloadURL requires absolute HTTPS without user information; loopback HTTP only behind a loopback endpoint.
func (operation ReadFileTextOperation) validateDownloadURL(location string) (*url.URL, error) {
	client := operation.client
	parsed, err := url.Parse(location)
	if err != nil || !parsed.IsAbs() || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return nil, errors.New("download URL is not an absolute URL")
	}
	switch parsed.Scheme {
	case "https":
		return parsed, nil
	case "http":
		if client.endpointOrigin.Scheme == "http" && isLoopbackHost(parsed.Hostname()) {
			return parsed, nil
		}
	}
	return nil, errors.New("download URL must use HTTPS")
}

// unsupportedContentReason returns why an item cannot be read as text, or "" for a text file.
func unsupportedContentReason(item DriveItem) string {
	switch {
	case item.IsFolder:
		return "item is a folder"
	case item.IsPackage:
		return "item is a package, such as a OneNote notebook"
	case isTextMimeType(item.MimeType):
		return ""
	case genericMimeTypes[item.MimeType] && textFileExtensions[strings.ToLower(path.Ext(item.Name))]:
		return ""
	default:
		return "file type is not text"
	}
}

func isTextMimeType(mimeType string) bool {
	mediaType := strings.ToLower(strings.TrimSpace(strings.SplitN(mimeType, ";", 2)[0]))
	return strings.HasPrefix(mediaType, "text/") || textMimeTypes[mediaType] ||
		strings.HasSuffix(mediaType, "+json") || strings.HasSuffix(mediaType, "+xml")
}

func isRedirectStatus(status int) bool {
	switch status {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return true
	default:
		return false
	}
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}
