// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package honor

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// LookupAppIDInput identifies one Android package owned by the Honor developer account.
type LookupAppIDInput struct {
	// PackageName is the exact Java package name bound in Honor App Management.
	PackageName string `json:"packageName"`
}

// LookupAppIDOutput contains only the matched package name and app ID.
type LookupAppIDOutput struct {
	// PackageName is the matched Android package name.
	PackageName string `json:"packageName"`
	// AppID is Honor's numeric ID; zero indicates no match.
	AppID int64 `json:"appID"`
}

// LookupAppIDOperation reads an owned app's ID without requesting app contents.
type LookupAppIDOperation struct{ client *Client }

// Definition returns the generated app ID query contract.
func (LookupAppIDOperation) Definition() sdkgo.QueryDefinition { return LookupAppIDDefinition }

// Invoke exchanges credentials for a short-lived token, then queries one package name.
func (operation LookupAppIDOperation) Invoke(call sdkgo.Call, input LookupAppIDInput) sdkgo.QueryAttempt[LookupAppIDOutput] {
	if len(input.PackageName) > 255 || !packageNamePattern.MatchString(input.PackageName) {
		return lookupBranch(LookupAppIDBranchDefect, sdkgo.FailureValidation, "Honor package name is invalid")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidCredential(credentials.ClientID.Reveal()) || !isValidCredential(credentials.ClientSecret.Reveal()) {
		return lookupBranch(LookupAppIDBranchDefect, sdkgo.FailureAuthentication, "Honor credentials are unavailable or invalid")
	}
	token, decision := operation.fetchAccessToken(call, credentials)
	if decision != nil {
		return *decision
	}
	return operation.queryAppID(call, input.PackageName, token)
}

func (operation LookupAppIDOperation) fetchAccessToken(call sdkgo.Call, credentials Credentials) (string, *sdkgo.QueryAttempt[LookupAppIDOutput]) {
	form := url.Values{"grant_type": {"client_credentials"}, "client_id": {credentials.ClientID.Reveal()}, "client_secret": {credentials.ClientSecret.Reveal()}}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		decision := lookupBranch(LookupAppIDBranchDefect, sdkgo.FailureValidation, "Honor token request is invalid")
		return "", &decision
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		decision := lookupRetry(sdkgo.FailureAvailability, "Honor token request failed", 0)
		return "", &decision
	}
	defer func() { _ = response.Body.Close() }()
	if decision := classifyHonorStatus(response); decision != nil {
		return "", decision
	}
	body, decision := operation.readResponse(response.Body)
	if decision != nil {
		return "", decision
	}
	var wire struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
		TokenType   string `json:"token_type"`
	}
	if json.Unmarshal(body, &wire) != nil || !isValidCredential(wire.AccessToken) || wire.ExpiresIn <= 0 || wire.TokenType != "Bearer" {
		result := lookupBranch(LookupAppIDBranchInvalidResponse, sdkgo.FailureProtocol, "Honor returned invalid token data")
		return "", &result
	}
	return wire.AccessToken, nil
}

func (operation LookupAppIDOperation) queryAppID(call sdkgo.Call, packageName, token string) sdkgo.QueryAttempt[LookupAppIDOutput] {
	endpoint := lookupEndpoint + "?" + url.Values{"pkgName": {packageName}}.Encode()
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, endpoint, nil)
	if err != nil {
		return lookupBranch(LookupAppIDBranchDefect, sdkgo.FailureValidation, "Honor app request is invalid")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return lookupRetry(sdkgo.FailureAvailability, "Honor app request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	if decision := classifyHonorStatus(response); decision != nil {
		return *decision
	}
	body, decision := operation.readResponse(response.Body)
	if decision != nil {
		return *decision
	}
	var wire struct {
		Code *int `json:"code"`
		Data []struct {
			AppID       int64  `json:"appId"`
			PackageName string `json:"packageName"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Code == nil {
		return lookupBranch(LookupAppIDBranchInvalidResponse, sdkgo.FailureProtocol, "Honor returned invalid app data")
	}
	if *wire.Code != 0 {
		return lookupBranch(LookupAppIDBranchProviderRejected, sdkgo.FailureProviderRejection, "Honor rejected app request with code "+strconv.Itoa(*wire.Code))
	}
	if len(wire.Data) == 0 {
		return sdkgo.NewQueryBranch(LookupAppIDBranchNotFound, LookupAppIDOutput{PackageName: packageName}, nil, sdkgo.Receipt{})
	}
	if len(wire.Data) != 1 || wire.Data[0].PackageName != packageName || wire.Data[0].AppID <= 0 {
		return lookupBranch(LookupAppIDBranchInvalidResponse, sdkgo.FailureProtocol, "Honor returned mismatched app data")
	}
	return sdkgo.NewQueryBranch(LookupAppIDBranchFound, LookupAppIDOutput{PackageName: packageName, AppID: wire.Data[0].AppID}, nil, sdkgo.Receipt{})
}

func (operation LookupAppIDOperation) readResponse(body io.Reader) ([]byte, *sdkgo.QueryAttempt[LookupAppIDOutput]) {
	content, err := providerhttp.ReadBoundedBody(body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		result := lookupBranch(LookupAppIDBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Honor response exceeded the configured limit")
		return nil, &result
	}
	if err != nil {
		result := lookupRetry(sdkgo.FailureAvailability, "Honor response was interrupted", 0)
		return nil, &result
	}
	return content, nil
}

func classifyHonorStatus(response *http.Response) *sdkgo.QueryAttempt[LookupAppIDOutput] {
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		result := lookupRetry(sdkgo.FailureRateLimit, "Honor rate limited the request", retryAfter)
		return &result
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		result := lookupRetry(sdkgo.FailureAvailability, "Honor API is temporarily unavailable", retryAfter)
		return &result
	}
	if response.StatusCode == http.StatusOK {
		return nil
	}
	kind := sdkgo.FailureProviderRejection
	if response.StatusCode == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if response.StatusCode == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	result := lookupBranch(LookupAppIDBranchProviderRejected, kind, "Honor rejected request with HTTP "+strconv.Itoa(response.StatusCode))
	return &result
}

func lookupBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[LookupAppIDOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "honor", Operation: "lookupAppID", Message: message}
	return sdkgo.NewQueryBranch(branch, LookupAppIDOutput{}, &failure, sdkgo.Receipt{})
}
func lookupRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[LookupAppIDOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "honor", Operation: "lookupAppID", Message: message}
	return sdkgo.NewQueryRetry[LookupAppIDOutput](failure, delay)
}
