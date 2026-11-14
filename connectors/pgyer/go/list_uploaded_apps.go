// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pgyer

import (
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

// ListUploadedAppsInput selects one page of apps uploaded by the API Key's account.
type ListUploadedAppsInput struct {
	// Page is a positive page number; zero asks Pgyer for its first page.
	Page int `json:"page"`
}

// UploadedApp describes an app build without download URLs or account details.
type UploadedApp struct {
	// AppKey identifies the app across versions.
	AppKey string `json:"appKey"`
	// BuildKey identifies this specific uploaded build.
	BuildKey string `json:"buildKey"`
	// Name is the app's display name.
	Name string `json:"name"`
	// BuildType is 1 for iOS or 2 for Android.
	BuildType int `json:"buildType"`
	// Version is the app's version string.
	Version string `json:"version"`
	// CreatedAt is Pgyer's upload-time string.
	CreatedAt string `json:"createdAt"`
}

// UploadedAppPage contains one bounded page of uploaded app metadata.
type UploadedAppPage struct {
	// Apps contains at most 100 items from one Pgyer response.
	Apps []UploadedApp `json:"apps"`
}

// ListUploadedAppsOperation lists account uploads through API 2.0.
type ListUploadedAppsOperation struct{ client *Client }

// Definition returns the generated app-list contract.
func (ListUploadedAppsOperation) Definition() sdkgo.QueryDefinition {
	return ListUploadedAppsDefinition
}

// Invoke reads one Pgyer upload page and classifies provider outcomes.
func (operation ListUploadedAppsOperation) Invoke(call sdkgo.Call, input ListUploadedAppsInput) sdkgo.QueryAttempt[UploadedAppPage] {
	if input.Page < 0 || input.Page > 10000 {
		return appBranch(ListUploadedAppsBranchDefect, sdkgo.FailureValidation, "Pgyer page number is invalid")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !validAPIKey(credentials.APIKey.Reveal()) {
		return appBranch(ListUploadedAppsBranchDefect, sdkgo.FailureAuthentication, "Pgyer API Key is unavailable or invalid")
	}
	form := url.Values{"_api_key": {credentials.APIKey.Reveal()}}
	if input.Page > 0 {
		form.Set("page", strconv.Itoa(input.Page))
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+"/apiv2/app/listMy", strings.NewReader(form.Encode()))
	if err != nil {
		return appBranch(ListUploadedAppsBranchDefect, sdkgo.FailureValidation, "Pgyer app-list request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return appRetry(sdkgo.FailureAvailability, "Pgyer app-list request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return appRetry(sdkgo.FailureRateLimit, "Pgyer hourly API limit reached", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return appRetry(sdkgo.FailureAvailability, "Pgyer is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return appBranch(ListUploadedAppsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Pgyer response exceeded the configured limit")
	}
	if err != nil {
		return appRetry(sdkgo.FailureAvailability, "Pgyer response was interrupted", 0)
	}
	if response.StatusCode != 200 {
		return appBranch(ListUploadedAppsBranchProviderRejected, classifyAppHTTP(response.StatusCode), "Pgyer rejected the query with HTTP "+strconv.Itoa(response.StatusCode))
	}
	page, code, err := decodeUploadedAppPage(body)
	if err != nil {
		return appBranch(ListUploadedAppsBranchInvalidResponse, sdkgo.FailureProtocol, "Pgyer returned an invalid app-list response")
	}
	if code == 1098 {
		return appRetry(sdkgo.FailureRateLimit, "Pgyer hourly API limit reached", retryAfter)
	}
	if code == 1003 || code == 1188 {
		return appRetry(sdkgo.FailureAvailability, "Pgyer is temporarily unavailable", retryAfter)
	}
	if code != 0 {
		kind := sdkgo.FailureProviderRejection
		if code == 1001 || code == 1002 || code == 1055 {
			kind = sdkgo.FailureAuthentication
		}
		if code == 1122 || code == 1124 {
			kind = sdkgo.FailureAuthorization
		}
		return appBranch(ListUploadedAppsBranchProviderRejected, kind, "Pgyer rejected the query with code "+strconv.Itoa(code))
	}
	return sdkgo.NewQueryBranch(ListUploadedAppsBranchListed, page, nil, sdkgo.Receipt{})
}
func decodeUploadedAppPage(body []byte) (UploadedAppPage, int, error) {
	var wire struct {
		Code *int `json:"code"`
		Data []struct {
			AppKey       string `json:"appKey"`
			BuildKey     string `json:"buildKey"`
			BuildName    string `json:"buildName"`
			BuildType    int    `json:"buildType"`
			BuildVersion string `json:"buildVersion"`
			BuildCreated string `json:"buildCreated"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Code == nil {
		return UploadedAppPage{}, 0, errors.New("invalid Pgyer JSON")
	}
	if *wire.Code != 0 {
		return UploadedAppPage{}, *wire.Code, nil
	}
	if wire.Data == nil || len(wire.Data) > 100 {
		return UploadedAppPage{}, 0, errors.New("invalid Pgyer app list")
	}
	page := UploadedAppPage{Apps: make([]UploadedApp, 0, len(wire.Data))}
	for _, app := range wire.Data {
		if app.AppKey == "" || app.BuildKey == "" || app.BuildName == "" || (app.BuildType != 1 && app.BuildType != 2) {
			return UploadedAppPage{}, 0, errors.New("invalid app identity")
		}
		page.Apps = append(page.Apps, UploadedApp{AppKey: app.AppKey, BuildKey: app.BuildKey, Name: app.BuildName, BuildType: app.BuildType, Version: app.BuildVersion, CreatedAt: app.BuildCreated})
	}
	return page, 0, nil
}
func validAPIKey(key string) bool {
	if key == "" || len(key) > 512 || strings.TrimSpace(key) != key {
		return false
	}
	for _, character := range key {
		if character <= 0x20 || character >= 0x7f {
			return false
		}
	}
	return true
}
func classifyAppHTTP(status int) sdkgo.FailureKind {
	switch status {
	case 401:
		return sdkgo.FailureAuthentication
	case 403:
		return sdkgo.FailureAuthorization
	case 400, 404:
		return sdkgo.FailureValidation
	default:
		return sdkgo.FailureProviderRejection
	}
}
func appBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[UploadedAppPage] {
	failure := sdkgo.Failure{Kind: kind, Provider: "pgyer", Operation: "listUploadedApps", Message: message}
	return sdkgo.NewQueryBranch(branch, UploadedAppPage{}, &failure, sdkgo.Receipt{})
}
func appRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[UploadedAppPage] {
	failure := sdkgo.Failure{Kind: kind, Provider: "pgyer", Operation: "listUploadedApps", Message: message}
	return sdkgo.NewQueryRetry[UploadedAppPage](failure, delay)
}
