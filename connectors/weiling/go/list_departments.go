// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package weiling

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const authPath = "/openapi/auth/access_token/get"
const departmentsPath = "/openapi/department/list"

// ListDepartmentsInput requests the accessible department directory without caller-supplied identifiers.
type ListDepartmentsInput struct{}

// DepartmentSummary excludes Weiling leader and employee identifiers.
type DepartmentSummary struct {
	// ID is the provider's department ID.
	ID string `json:"id"`
	// Name is the department name.
	Name string `json:"name"`
	// ParentID is the optional parent department ID.
	ParentID string `json:"parentID"`
}

// ListDepartmentsOutput contains the accessible department directory.
type ListDepartmentsOutput struct {
	// Departments contains accessible department metadata without leader IDs.
	Departments []DepartmentSummary `json:"departments"`
}

// ListDepartmentsOperation reads Weiling CRM departments.
type ListDepartmentsOperation struct{ client *Client }

// Definition returns the generated department-list Query contract.
func (ListDepartmentsOperation) Definition() sdkgo.QueryDefinition { return ListDepartmentsDefinition }

// Invoke obtains a cached short-lived token and reads the accessible department directory.
func (operation ListDepartmentsOperation) Invoke(call sdkgo.Call, input ListDepartmentsInput) sdkgo.QueryAttempt[ListDepartmentsOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidCredential(credentials.AppID.Reveal()) || !isValidCredential(credentials.AppSecret.Reveal()) {
		return departmentBranch(ListDepartmentsBranchDefect, sdkgo.FailureAuthentication, "Weiling credentials are unavailable or invalid")
	}
	token, attempt := operation.accessToken(call, credentials.AppID.Reveal(), credentials.AppSecret.Reveal())
	if attempt != nil {
		return *attempt
	}
	values := url.Values{}
	values.Set("access_token", token)
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+departmentsPath+"?"+values.Encode(), nil)
	if err != nil {
		return departmentBranch(ListDepartmentsBranchDefect, sdkgo.FailureValidation, "department request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return departmentRetry(sdkgo.FailureAvailability, "Weiling department transport failed", 0)
	}
	body, attempt := operation.readResponse(response, "department")
	if attempt != nil {
		return *attempt
	}
	var wire struct {
		Code *int `json:"code"`
		Data *struct {
			DataList []struct {
				ID       string `json:"dept_id"`
				Name     string `json:"name"`
				ParentID string `json:"parent_id"`
			} `json:"data_list"`
		} `json:"data"`
	}
	if !decodeOne(body, &wire) || wire.Code == nil {
		return departmentBranch(ListDepartmentsBranchInvalidResponse, sdkgo.FailureProtocol, "Weiling returned invalid department JSON")
	}
	if *wire.Code != 0 {
		return departmentProviderFailure(*wire.Code, "department")
	}
	if wire.Data == nil || wire.Data.DataList == nil {
		return departmentBranch(ListDepartmentsBranchInvalidResponse, sdkgo.FailureProtocol, "Weiling department list is missing")
	}
	departments := make([]DepartmentSummary, 0, len(wire.Data.DataList))
	for _, item := range wire.Data.DataList {
		if item.ID == "" || len(item.ID) > 256 || item.Name == "" || len(item.Name) > 512 || len(item.ParentID) > 256 {
			return departmentBranch(ListDepartmentsBranchInvalidResponse, sdkgo.FailureProtocol, "Weiling returned invalid department metadata")
		}
		departments = append(departments, DepartmentSummary{ID: item.ID, Name: item.Name, ParentID: item.ParentID})
	}
	return sdkgo.NewQueryBranch(ListDepartmentsBranchListed, ListDepartmentsOutput{Departments: departments}, nil, sdkgo.Receipt{})
}
func (operation ListDepartmentsOperation) accessToken(call sdkgo.Call, appID, appSecret string) (string, *sdkgo.QueryAttempt[ListDepartmentsOutput]) {
	credentialHash := sha256.Sum256([]byte(appID + "\x00" + appSecret))
	operation.client.tokenMutex.Lock()
	defer operation.client.tokenMutex.Unlock()
	if operation.client.cachedToken != "" && operation.client.tokenCredentialHash == credentialHash && time.Now().Before(operation.client.tokenExpiresAt) {
		return operation.client.cachedToken, nil
	}
	values := url.Values{}
	values.Set("app_id", appID)
	values.Set("app_secret", appSecret)
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+authPath+"?"+values.Encode(), nil)
	if err != nil {
		attempt := departmentBranch(ListDepartmentsBranchDefect, sdkgo.FailureValidation, "authorization request is invalid")
		return "", &attempt
	}
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		attempt := departmentRetry(sdkgo.FailureAvailability, "Weiling authorization transport failed", 0)
		return "", &attempt
	}
	body, attempt := operation.readResponse(response, "authorization")
	if attempt != nil {
		return "", attempt
	}
	var wire struct {
		Code *int `json:"code"`
		Data *struct {
			AccessToken string `json:"access_token"`
			ExpiresIn   int    `json:"expires_in"`
		} `json:"data"`
	}
	if !decodeOne(body, &wire) || wire.Code == nil {
		failure := departmentBranch(ListDepartmentsBranchInvalidResponse, sdkgo.FailureProtocol, "Weiling returned invalid authorization JSON")
		return "", &failure
	}
	if *wire.Code != 0 {
		failure := departmentProviderFailure(*wire.Code, "authorization")
		return "", &failure
	}
	if wire.Data == nil || !isValidCredential(wire.Data.AccessToken) || wire.Data.ExpiresIn < 180 || wire.Data.ExpiresIn > 86400 {
		failure := departmentBranch(ListDepartmentsBranchInvalidResponse, sdkgo.FailureProtocol, "Weiling returned invalid authorization data")
		return "", &failure
	}
	operation.client.cachedToken = wire.Data.AccessToken
	operation.client.tokenCredentialHash = credentialHash
	operation.client.tokenExpiresAt = time.Now().Add(time.Duration(wire.Data.ExpiresIn-120) * time.Second)
	return wire.Data.AccessToken, nil
}
func (operation ListDepartmentsOperation) readResponse(response *http.Response, stage string) ([]byte, *sdkgo.QueryAttempt[ListDepartmentsOutput]) {
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == 429 {
		attempt := departmentRetry(sdkgo.FailureRateLimit, "Weiling "+stage+" request rate limited", retryAfter)
		return nil, &attempt
	}
	if response.StatusCode == 408 || response.StatusCode >= 500 {
		attempt := departmentRetry(sdkgo.FailureAvailability, "Weiling "+stage+" temporarily unavailable", retryAfter)
		return nil, &attempt
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		attempt := departmentBranch(ListDepartmentsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Weiling "+stage+" response exceeded the configured limit")
		return nil, &attempt
	}
	if err != nil {
		attempt := departmentRetry(sdkgo.FailureAvailability, "Weiling "+stage+" response was interrupted", 0)
		return nil, &attempt
	}
	if response.StatusCode != 200 {
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == 401 || response.StatusCode == 403 {
			kind = sdkgo.FailureAuthentication
		}
		attempt := departmentBranch(ListDepartmentsBranchProviderRejected, kind, "Weiling rejected "+stage+" with HTTP "+strconv.Itoa(response.StatusCode))
		return nil, &attempt
	}
	return body, nil
}
func departmentProviderFailure(code int, stage string) sdkgo.QueryAttempt[ListDepartmentsOutput] {
	if code == 1009 {
		return departmentRetry(sdkgo.FailureRateLimit, "Weiling "+stage+" request rate limited", 0)
	}
	kind := sdkgo.FailureProviderRejection
	if code == 403 || code == 1007 || code == 1008 {
		kind = sdkgo.FailureAuthentication
	}
	return departmentBranch(ListDepartmentsBranchProviderRejected, kind, "Weiling rejected "+stage+" with code "+strconv.Itoa(code))
}
func decodeOne(body []byte, target any) bool {
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	if decoder.Decode(target) != nil {
		return false
	}
	var extra any
	return decoder.Decode(&extra) == io.EOF
}
func isValidCredential(value string) bool {
	if value == "" || len(value) > 2048 {
		return false
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}
func departmentBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListDepartmentsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "weiling", Operation: "listDepartments", Message: message}
	return sdkgo.NewQueryBranch(branch, ListDepartmentsOutput{}, &failure, sdkgo.Receipt{})
}
func departmentRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListDepartmentsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "weiling", Operation: "listDepartments", Message: message}
	return sdkgo.NewQueryRetry[ListDepartmentsOutput](failure, delay)
}
