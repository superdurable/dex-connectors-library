// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package kingsoftoffice

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListFolderFilesInput selects one drive folder and an optional page cursor.
type ListFolderFilesInput struct {
	// DriveID is the WPS drive identifier visible in its drive API or URL.
	DriveID string `json:"driveId"`
	// ParentID is the WPS folder identifier; 0 selects the drive root.
	ParentID string `json:"parentId"`
	// PageToken is an opaque cursor from a prior response; blank selects the first page.
	PageToken string `json:"pageToken"`
}

// FileMetadata holds only file identifiers and names, without content, people, or sharing links.
type FileMetadata struct {
	// ID is the WPS file identifier.
	ID string `json:"id"`
	// Name is the file or folder name.
	Name string `json:"name"`
	// Type is the WPS file type, such as file or folder.
	Type string `json:"type"`
	// SizeBytes is the provider's file size in bytes.
	SizeBytes int64 `json:"sizeBytes"`
}

// ListFolderFilesOutput is one bounded page of file metadata.
type ListFolderFilesOutput struct {
	// Items contains at most the configured page size.
	Items []FileMetadata `json:"items"`
	// NextPageToken is empty when no next page exists.
	NextPageToken string `json:"nextPageToken"`
}

// ListFolderFilesOperation reads one WPS folder page.
type ListFolderFilesOperation struct{ client *Client }

// Definition returns the generated file-list Query contract.
func (ListFolderFilesOperation) Definition() sdkgo.QueryDefinition { return ListFolderFilesDefinition }

// Invoke gets an application token, optionally signs, and fetches one metadata page.
func (operation ListFolderFilesOperation) Invoke(call sdkgo.Call, input ListFolderFilesInput) sdkgo.QueryAttempt[ListFolderFilesOutput] {
	if !isValidResourceID(input.DriveID) || !isValidResourceID(input.ParentID) || !isValidPageToken(input.PageToken) {
		return listFilesBranch(ListFolderFilesBranchDefect, sdkgo.FailureValidation, "drive, folder, or page token is invalid")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAppID(credentials.AppID) || !isValidAppKey(credentials.AppKey.Reveal()) {
		return listFilesBranch(ListFolderFilesBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	accessToken, tokenFailure := operation.client.accessTokenFor(call.Context, credentials.AppID, credentials.AppKey.Reveal())
	if tokenFailure != nil {
		if tokenFailure.retry {
			return listFilesRetry(tokenFailure.kind, tokenFailure.message, tokenFailure.delay)
		}
		if tokenFailure.invalidResponse {
			return listFilesBranch(ListFolderFilesBranchInvalidResponse, tokenFailure.kind, tokenFailure.message)
		}
		return listFilesBranch(ListFolderFilesBranchProviderRejected, tokenFailure.kind, tokenFailure.message)
	}
	path := "/v7/drives/" + url.PathEscape(input.DriveID) + "/files/" + url.PathEscape(input.ParentID) + "/children"
	query := url.Values{"page_size": {strconv.FormatInt(operation.client.pageSize, 10)}}
	if input.PageToken != "" {
		query.Set("page_token", input.PageToken)
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+path+"?"+query.Encode(), nil)
	if err != nil {
		return listFilesBranch(ListFolderFilesBranchDefect, sdkgo.FailureValidation, "WPS file-list request is invalid")
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("Accept", "application/json")
	if operation.client.requestSigningEnabled {
		request.Header.Set("Content-Type", "application/json")
		ksoDate := time.Now().UTC().Format(http.TimeFormat)
		request.Header.Set("X-Kso-Date", ksoDate)
		request.Header.Set("X-Kso-Authorization", signKSO1Request(credentials.AppID, credentials.AppKey.Reveal(), request.Method, request.URL.RequestURI(), "application/json", ksoDate))
	}
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return listFilesRetry(sdkgo.FailureAvailability, "WPS file-list request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	delay := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return listFilesRetry(sdkgo.FailureRateLimit, "WPS rate limited file listing", delay)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return listFilesRetry(sdkgo.FailureAvailability, "WPS file listing is temporarily unavailable", delay)
	}
	if response.StatusCode != http.StatusOK {
		return listFilesBranch(ListFolderFilesBranchProviderRejected, classifyWPSHTTPStatus(response.StatusCode), "WPS rejected file listing")
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return listFilesBranch(ListFolderFilesBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "WPS file-list response exceeded limit")
	}
	if err != nil {
		return listFilesRetry(sdkgo.FailureAvailability, "WPS file-list response was interrupted", 0)
	}
	var wire struct {
		Code *int `json:"code"`
		Data struct {
			Items *[]struct {
				ID   string `json:"id"`
				Name string `json:"name"`
				Type string `json:"type"`
				Size *int64 `json:"size"`
			} `json:"items"`
			NextPageToken string `json:"next_page_token"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Code == nil {
		return listFilesBranch(ListFolderFilesBranchInvalidResponse, sdkgo.FailureProtocol, "WPS returned malformed file-list data")
	}
	if *wire.Code == 429000001 {
		return listFilesRetry(sdkgo.FailureRateLimit, "WPS rate limited file listing", delay)
	}
	if *wire.Code != 0 {
		return listFilesBranch(ListFolderFilesBranchProviderRejected, classifyWPSErrorCode(*wire.Code), "WPS rejected file listing")
	}
	if wire.Data.Items == nil || len(*wire.Data.Items) > int(operation.client.pageSize) || len(wire.Data.NextPageToken) > 2048 {
		return listFilesBranch(ListFolderFilesBranchInvalidResponse, sdkgo.FailureProtocol, "WPS returned an invalid file-list page")
	}
	output := ListFolderFilesOutput{Items: make([]FileMetadata, 0, len(*wire.Data.Items)), NextPageToken: wire.Data.NextPageToken}
	for _, item := range *wire.Data.Items {
		if item.ID == "" || item.Name == "" || item.Type == "" || item.Size == nil || *item.Size < 0 {
			return listFilesBranch(ListFolderFilesBranchInvalidResponse, sdkgo.FailureProtocol, "WPS returned an invalid file item")
		}
		output.Items = append(output.Items, FileMetadata{ID: item.ID, Name: item.Name, Type: item.Type, SizeBytes: *item.Size})
	}
	return sdkgo.NewQueryBranch(ListFolderFilesBranchListed, output, nil, sdkgo.Receipt{})
}
func signKSO1Request(appID, appKey, method, requestURI, contentType, ksoDate string) string {
	signed := "KSO-1" + method + requestURI + contentType + ksoDate
	mac := hmac.New(sha256.New, []byte(appKey))
	_, _ = mac.Write([]byte(signed))
	return "KSO-1 " + appID + ":" + hex.EncodeToString(mac.Sum(nil))
}
func isValidResourceID(value string) bool {
	if value == "" || len(value) > 256 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e || character == '/' || character == '?' || character == '#' {
			return false
		}
	}
	return true
}
func isValidAppID(value string) bool { return isValidResourceID(value) }
func isValidPageToken(value string) bool {
	if len(value) > 2048 {
		return false
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}
func isValidAppKey(value string) bool {
	if value == "" || len(value) > 512 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character <= 0x20 || character >= 0x7f {
			return false
		}
	}
	return true
}
func classifyWPSHTTPStatus(statusCode int) sdkgo.FailureKind {
	switch statusCode {
	case 401:
		return sdkgo.FailureAuthentication
	case 403:
		return sdkgo.FailureAuthorization
	case 400, 404, 422:
		return sdkgo.FailureValidation
	default:
		return sdkgo.FailureProviderRejection
	}
}
func classifyWPSErrorCode(code int) sdkgo.FailureKind {
	switch code {
	case 40100011, 40100008:
		return sdkgo.FailureAuthentication
	case 403000001, 40300010:
		return sdkgo.FailureAuthorization
	case 400000001:
		return sdkgo.FailureQuotaExhausted
	default:
		return sdkgo.FailureProviderRejection
	}
}
func listFilesBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListFolderFilesOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "kingsoftoffice", Operation: "listFolderFiles", Message: message}
	return sdkgo.NewQueryBranch(branch, ListFolderFilesOutput{}, &failure, sdkgo.Receipt{})
}
func listFilesRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListFolderFilesOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "kingsoftoffice", Operation: "listFolderFiles", Message: message}
	return sdkgo.NewQueryRetry[ListFolderFilesOutput](failure, delay)
}
