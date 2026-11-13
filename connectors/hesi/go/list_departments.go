// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hesi

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

const authPath = "/api/openapi/v1/auth/getAccessToken"
const departmentsPath = "/api/openapi/v1/departments"

// ListDepartmentsInput selects a zero-based department window; zero count uses 20.
type ListDepartmentsInput struct {
	// Start is the zero-based department offset.
	Start int `json:"start"`
	// Count is the number of departments requested, from 1 to 100.
	Count int `json:"count"`
}

// DepartmentSummary excludes Hesi legal entity and cost center data.
type DepartmentSummary struct {
	// ID is the provider's stable department ID.
	ID string `json:"id"`
	// Name is the department name.
	Name string `json:"name"`
	// Code is the optional enterprise department code.
	Code string `json:"code"`
	// Active reports whether the department is enabled.
	Active bool `json:"active"`
}

// ListDepartmentsOutput contains one department page and a next offset.
type ListDepartmentsOutput struct {
	// Departments are returned in provider order.
	Departments []DepartmentSummary `json:"departments"`
	// Total is the number of departments reported by Hesi.
	Total int `json:"total"`
	// NextStart is the next zero-based offset, or zero when the page is terminal.
	NextStart int `json:"nextStart"`
}

// ListDepartmentsOperation reads the Hesi department directory.
type ListDepartmentsOperation struct{ client *Client }

// Definition returns the generated department-list Query contract.
func (ListDepartmentsOperation) Definition() sdkgo.QueryDefinition { return ListDepartmentsDefinition }

// Invoke exchanges credentials for a short-lived token and reads one department page.
func (operation ListDepartmentsOperation) Invoke(call sdkgo.Call, input ListDepartmentsInput) sdkgo.QueryAttempt[ListDepartmentsOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidCredential(credentials.AppKey.Reveal()) || !isValidCredential(credentials.AppSecurity.Reveal()) {
		return departmentBranch(ListDepartmentsBranchDefect, sdkgo.FailureAuthentication, "Hesi credentials are unavailable or invalid")
	}
	count := input.Count
	if count == 0 {
		count = 20
	}
	if input.Start < 0 || input.Start > 1000000 || count < 1 || count > 100 {
		return departmentBranch(ListDepartmentsBranchDefect, sdkgo.FailureValidation, "start or count is outside the supported range")
	}
	authBody, _ := json.Marshal(struct {
		AppKey      string `json:"appKey"`
		AppSecurity string `json:"appSecurity"`
	}{credentials.AppKey.Reveal(), credentials.AppSecurity.Reveal()})
	authRequest, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+authPath, strings.NewReader(string(authBody)))
	if err != nil {
		return departmentBranch(ListDepartmentsBranchDefect, sdkgo.FailureValidation, "authorization request is invalid")
	}
	authRequest.Header.Set("Content-Type", "application/json")
	authRequest.Header.Set("Accept", "application/json")
	authResponse, err := operation.client.httpClient.Do(authRequest)
	if err != nil {
		return departmentRetry(sdkgo.FailureAvailability, "Hesi authorization transport failed", 0)
	}
	authBytes, attempt := operation.readResponse(authResponse, "authorization")
	if attempt != nil {
		return *attempt
	}
	var authWire struct {
		Value *struct {
			AccessToken string `json:"accessToken"`
		} `json:"value"`
	}
	if !decodeOne(authBytes, &authWire) || authWire.Value == nil || !isValidCredential(authWire.Value.AccessToken) {
		return departmentBranch(ListDepartmentsBranchInvalidResponse, sdkgo.FailureProtocol, "Hesi returned invalid authorization data")
	}
	values := url.Values{}
	values.Set("accessToken", authWire.Value.AccessToken)
	values.Set("start", strconv.Itoa(input.Start))
	values.Set("count", strconv.Itoa(count))
	departmentRequest, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+departmentsPath+"?"+values.Encode(), nil)
	if err != nil {
		return departmentBranch(ListDepartmentsBranchDefect, sdkgo.FailureValidation, "department request is invalid")
	}
	departmentRequest.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(departmentRequest)
	if err != nil {
		return departmentRetry(sdkgo.FailureAvailability, "Hesi department transport failed", 0)
	}
	body, attempt := operation.readResponse(response, "department")
	if attempt != nil {
		return *attempt
	}
	var wire struct {
		Count int `json:"count"`
		Items []struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Code   string `json:"code"`
			Active *bool  `json:"active"`
		} `json:"items"`
	}
	if !decodeOne(body, &wire) || wire.Items == nil || wire.Count < 0 || len(wire.Items) > count {
		return departmentBranch(ListDepartmentsBranchInvalidResponse, sdkgo.FailureProtocol, "Hesi returned invalid department data")
	}
	departments := make([]DepartmentSummary, 0, len(wire.Items))
	for _, item := range wire.Items {
		if item.ID == "" || len(item.ID) > 256 || item.Name == "" || len(item.Name) > 512 || len(item.Code) > 256 || item.Active == nil {
			return departmentBranch(ListDepartmentsBranchInvalidResponse, sdkgo.FailureProtocol, "Hesi returned invalid department metadata")
		}
		departments = append(departments, DepartmentSummary{ID: item.ID, Name: item.Name, Code: item.Code, Active: *item.Active})
	}
	nextStart := 0
	if input.Start+len(departments) < wire.Count {
		if len(departments) == 0 || input.Start+len(departments) > 1000000 {
			return departmentBranch(ListDepartmentsBranchInvalidResponse, sdkgo.FailureProtocol, "Hesi pagination cannot advance")
		}
		nextStart = input.Start + len(departments)
	}
	return sdkgo.NewQueryBranch(ListDepartmentsBranchListed, ListDepartmentsOutput{Departments: departments, Total: wire.Count, NextStart: nextStart}, nil, sdkgo.Receipt{})
}

func (operation ListDepartmentsOperation) readResponse(response *http.Response, stage string) ([]byte, *sdkgo.QueryAttempt[ListDepartmentsOutput]) {
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		attempt := departmentRetry(sdkgo.FailureRateLimit, "Hesi "+stage+" request rate limited", retryAfter)
		return nil, &attempt
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		attempt := departmentRetry(sdkgo.FailureAvailability, "Hesi "+stage+" temporarily unavailable", retryAfter)
		return nil, &attempt
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		attempt := departmentBranch(ListDepartmentsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Hesi "+stage+" response exceeded the configured limit")
		return nil, &attempt
	}
	if err != nil {
		attempt := departmentRetry(sdkgo.FailureAvailability, "Hesi "+stage+" response was interrupted", 0)
		return nil, &attempt
	}
	if response.StatusCode != http.StatusOK {
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
			kind = sdkgo.FailureAuthentication
		}
		attempt := departmentBranch(ListDepartmentsBranchProviderRejected, kind, "Hesi rejected "+stage+" with HTTP "+strconv.Itoa(response.StatusCode))
		return nil, &attempt
	}
	return body, nil
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
	failure := sdkgo.Failure{Kind: kind, Provider: "hesi", Operation: "listDepartments", Message: message}
	return sdkgo.NewQueryBranch(branch, ListDepartmentsOutput{}, &failure, sdkgo.Receipt{})
}
func departmentRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListDepartmentsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "hesi", Operation: "listDepartments", Message: message}
	return sdkgo.NewQueryRetry[ListDepartmentsOutput](failure, delay)
}
