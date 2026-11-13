// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package fxiaoke

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListObjectMetadataInput has no provider-specific query parameters.
type ListObjectMetadataInput struct{}

// ObjectMetadata is the safe subset of one CRM object definition.
type ObjectMetadata struct {
	// APIName is the stable CRM object API name.
	APIName string `json:"apiName"`
	// DisplayName is the object's localized display name.
	DisplayName string `json:"displayName"`
	// Active indicates whether the object is available.
	Active bool `json:"active"`
}

// ListObjectMetadataOutput contains object names without customer records.
type ListObjectMetadataOutput struct {
	// Objects are the CRM objects visible to the configured employee.
	Objects []ObjectMetadata `json:"objects"`
}

// ListObjectMetadataOperation reads CRM object metadata without mutating provider state.
type ListObjectMetadataOperation struct{ client *Client }

// Definition returns the generated object-list contract.
func (ListObjectMetadataOperation) Definition() sdkgo.QueryDefinition {
	return ListObjectMetadataDefinition
}

// Invoke obtains a client-credentials token and lists visible CRM objects.
func (operation ListObjectMetadataOperation) Invoke(call sdkgo.Call, _ ListObjectMetadataInput) sdkgo.QueryAttempt[ListObjectMetadataOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidIdentifier(credentials.AppID) || !isValidSecret(credentials.AppSecret.Reveal()) || !isValidSecret(credentials.PermanentCode.Reveal()) {
		return objectBranch(ListObjectMetadataBranchDefect, sdkgo.FailureAuthentication, "FXiaoke app credentials are unavailable or invalid")
	}
	token, enterpriseAccount, failure := operation.client.getAccessToken(call, credentials)
	if failure != nil {
		return *failure
	}
	traceID, err := newTraceID()
	if err != nil {
		return objectRetry(sdkgo.FailureAvailability, "FXiaoke request ID could not be generated", 0)
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+"/cgi/crm/v2/object/list?thirdTraceId="+traceID, bytes.NewBufferString(`{}`))
	if err != nil {
		return objectBranch(ListObjectMetadataBranchDefect, sdkgo.FailureValidation, "FXiaoke object-list request is invalid")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("x-fs-ea", enterpriseAccount)
	request.Header.Set("x-fs-userid", operation.client.employeeID)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	body, status, retryAfter, requestFailure := operation.client.doBoundedRequest(request)
	if requestFailure != nil {
		return *requestFailure
	}
	if status != http.StatusOK {
		return classifyHTTPStatus(status, retryAfter)
	}
	var wire struct {
		ErrorCode *int `json:"errorCode"`
		Data      *struct {
			Objects []struct {
				APIName     string `json:"describeApiName"`
				DisplayName string `json:"describeDisplayName"`
				Active      *bool  `json:"isActive"`
			} `json:"objects"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &wire) != nil {
		return objectBranch(ListObjectMetadataBranchInvalidResponse, sdkgo.FailureProtocol, "FXiaoke returned invalid object metadata")
	}
	if wire.ErrorCode != nil && *wire.ErrorCode != 0 {
		if *wire.ErrorCode == 20016 {
			operation.client.clearAccessToken()
			return objectRetry(sdkgo.FailureAuthentication, "FXiaoke token expired", 0)
		}
		if isTransientCode(*wire.ErrorCode) {
			return objectRetry(sdkgo.FailureAvailability, "FXiaoke object list is temporarily unavailable", 0)
		}
		return objectBranch(ListObjectMetadataBranchProviderRejected, sdkgo.FailureProviderRejection, "FXiaoke rejected the object-list request with code "+strconv.Itoa(*wire.ErrorCode))
	}
	if wire.Data == nil || wire.Data.Objects == nil {
		return objectBranch(ListObjectMetadataBranchInvalidResponse, sdkgo.FailureProtocol, "FXiaoke returned no object list")
	}
	output := ListObjectMetadataOutput{Objects: make([]ObjectMetadata, 0, len(wire.Data.Objects))}
	for _, object := range wire.Data.Objects {
		if object.APIName == "" || object.DisplayName == "" || object.Active == nil {
			return objectBranch(ListObjectMetadataBranchInvalidResponse, sdkgo.FailureProtocol, "FXiaoke returned invalid object metadata")
		}
		output.Objects = append(output.Objects, ObjectMetadata{APIName: object.APIName, DisplayName: object.DisplayName, Active: *object.Active})
	}
	return sdkgo.NewQueryBranch(ListObjectMetadataBranchListed, output, nil, sdkgo.Receipt{})
}

func (client *Client) getAccessToken(call sdkgo.Call, credentials Credentials) (string, string, *sdkgo.QueryAttempt[ListObjectMetadataOutput]) {
	client.tokenMu.Lock()
	defer client.tokenMu.Unlock()
	key := credentialKey(credentials)
	if client.tokenKey == key && client.token != "" && time.Now().Before(client.tokenValidUntil) {
		return client.token, client.enterpriseAccount, nil
	}
	traceID, err := newTraceID()
	if err != nil {
		attempt := objectRetry(sdkgo.FailureAvailability, "FXiaoke request ID could not be generated", 0)
		return "", "", &attempt
	}
	body, err := json.Marshal(struct {
		AppID         string `json:"appId"`
		AppSecret     string `json:"appSecret"`
		PermanentCode string `json:"permanentCode"`
		GrantType     string `json:"grantType"`
	}{credentials.AppID, credentials.AppSecret.Reveal(), credentials.PermanentCode.Reveal(), "app_secret"})
	if err != nil {
		attempt := objectBranch(ListObjectMetadataBranchDefect, sdkgo.FailureValidation, "FXiaoke token request is invalid")
		return "", "", &attempt
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, client.baseURL+"/oauth2.0/token?thirdTraceId="+traceID, bytes.NewReader(body))
	if err != nil {
		attempt := objectBranch(ListObjectMetadataBranchDefect, sdkgo.FailureValidation, "FXiaoke token request is invalid")
		return "", "", &attempt
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	responseBody, status, retryAfter, requestFailure := client.doBoundedRequest(request)
	if requestFailure != nil {
		return "", "", requestFailure
	}
	if status != http.StatusOK {
		attempt := classifyHTTPStatus(status, retryAfter)
		return "", "", &attempt
	}
	var wire struct {
		ErrorCode         *int   `json:"errorCode"`
		AccessToken       string `json:"accessToken"`
		EnterpriseAccount string `json:"ea"`
		ExpiresIn         int64  `json:"expiresIn"`
	}
	if json.Unmarshal(responseBody, &wire) != nil || wire.ErrorCode == nil {
		attempt := objectBranch(ListObjectMetadataBranchInvalidResponse, sdkgo.FailureProtocol, "FXiaoke returned an invalid token response")
		return "", "", &attempt
	}
	if *wire.ErrorCode != 0 {
		if isTransientCode(*wire.ErrorCode) {
			attempt := objectRetry(sdkgo.FailureAvailability, "FXiaoke token service is temporarily unavailable", 0)
			return "", "", &attempt
		}
		attempt := objectBranch(ListObjectMetadataBranchProviderRejected, sdkgo.FailureAuthentication, "FXiaoke rejected app credentials with code "+strconv.Itoa(*wire.ErrorCode))
		return "", "", &attempt
	}
	if !isValidSecret(wire.AccessToken) || !isValidIdentifier(wire.EnterpriseAccount) || wire.ExpiresIn <= 0 || wire.ExpiresIn > 7200 {
		attempt := objectBranch(ListObjectMetadataBranchInvalidResponse, sdkgo.FailureProtocol, "FXiaoke returned invalid token metadata")
		return "", "", &attempt
	}
	cacheSeconds := min(wire.ExpiresIn-60, int64(6000))
	if cacheSeconds < 1 {
		cacheSeconds = 1
	}
	client.tokenKey, client.token, client.enterpriseAccount, client.tokenValidUntil = key, wire.AccessToken, wire.EnterpriseAccount, time.Now().Add(time.Duration(cacheSeconds)*time.Second)
	return client.token, client.enterpriseAccount, nil
}

func (client *Client) clearAccessToken() {
	client.tokenMu.Lock()
	defer client.tokenMu.Unlock()
	client.token, client.enterpriseAccount, client.tokenValidUntil = "", "", time.Time{}
}
func (client *Client) doBoundedRequest(request *http.Request) ([]byte, int, time.Duration, *sdkgo.QueryAttempt[ListObjectMetadataOutput]) {
	response, err := client.httpClient.Do(request)
	if err != nil {
		attempt := objectRetry(sdkgo.FailureAvailability, "FXiaoke request failed", 0)
		return nil, 0, 0, &attempt
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		attempt := objectBranch(ListObjectMetadataBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "FXiaoke response exceeded the configured limit")
		return nil, 0, 0, &attempt
	}
	if err != nil {
		attempt := objectRetry(sdkgo.FailureAvailability, "FXiaoke response was interrupted", 0)
		return nil, 0, 0, &attempt
	}
	return body, response.StatusCode, retryAfter, nil
}
func newTraceID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", value[:4], value[4:6], value[6:8], value[8:10], value[10:]), nil
}
func isTransientCode(code int) bool {
	return code == -1 || code == -2 || code == 14001 || code == 30004 || code == 50009
}
func classifyHTTPStatus(status int, retryAfter time.Duration) sdkgo.QueryAttempt[ListObjectMetadataOutput] {
	if status == http.StatusTooManyRequests {
		return objectRetry(sdkgo.FailureRateLimit, "FXiaoke rate limited the request", retryAfter)
	}
	if status == http.StatusRequestTimeout || status >= 500 {
		return objectRetry(sdkgo.FailureAvailability, "FXiaoke is temporarily unavailable", retryAfter)
	}
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return objectBranch(ListObjectMetadataBranchProviderRejected, kind, "FXiaoke rejected the request with HTTP "+strconv.Itoa(status))
}
func objectBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListObjectMetadataOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "fxiaoke", Operation: "listObjectMetadata", Message: message}
	return sdkgo.NewQueryBranch(branch, ListObjectMetadataOutput{}, &failure, sdkgo.Receipt{})
}
func objectRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListObjectMetadataOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "fxiaoke", Operation: "listObjectMetadata", Message: message}
	return sdkgo.NewQueryRetry[ListObjectMetadataOutput](failure, delay)
}
