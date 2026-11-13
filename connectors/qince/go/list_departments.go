// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package qince

import (
	"bytes"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const departmentQueryPath = "/api/organization/v1/queryOrganization/"
const departmentQueryBody = "{}"

// ListDepartmentsInput requests the department catalog visible to the OpenAPI authorization.
type ListDepartmentsInput struct{}

// Department contains only Qince organization structure metadata.
type Department struct {
	// ID is the Qince department identifier.
	ID string `json:"id"`
	// Code is the department code.
	Code string `json:"code"`
	// Name is the department name.
	Name string `json:"name"`
	// Active indicates Qince org_status 1.
	Active bool `json:"active"`
}

// ListDepartmentsOutput contains the returned department catalog in provider order.
type ListDepartmentsOutput struct {
	Departments []Department `json:"departments"`
}

// ListDepartmentsOperation reads Qince's organization query.
type ListDepartmentsOperation struct{ client *Client }

// Definition returns the generated department-query contract.
func (ListDepartmentsOperation) Definition() sdkgo.QueryDefinition { return ListDepartmentsDefinition }

// Invoke lists departments and classifies provider and transport outcomes.
func (operation ListDepartmentsOperation) Invoke(call sdkgo.Call, _ ListDepartmentsInput) sdkgo.QueryAttempt[ListDepartmentsOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidOpenID(credentials.OpenID) || !isValidAppKey(credentials.AppKey.Reveal()) {
		return departmentCatalogBranch(ListDepartmentsBranchDefect, sdkgo.FailureAuthentication, "Qince OpenAPI credentials are unavailable or invalid")
	}
	messageID, err := newMessageID()
	if err != nil {
		return departmentCatalogBranch(ListDepartmentsBranchDefect, sdkgo.FailureAvailability, "cannot create Qince message ID")
	}
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	digest := signDepartmentQuery(departmentQueryBody, credentials.AppKey.Reveal(), timestamp)
	endpoint := operation.client.baseURL + departmentQueryPath + credentials.OpenID + "/" + timestamp + "/" + digest + "/" + messageID
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, endpoint, bytes.NewBufferString(departmentQueryBody))
	if err != nil {
		return departmentCatalogBranch(ListDepartmentsBranchDefect, sdkgo.FailureValidation, "Qince department query is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return departmentCatalogRetry(sdkgo.FailureAvailability, "Qince request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return departmentCatalogRetry(sdkgo.FailureRateLimit, "Qince rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return departmentCatalogRetry(sdkgo.FailureAvailability, "Qince temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return departmentCatalogBranch(ListDepartmentsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Qince response exceeded the configured limit")
	}
	if err != nil {
		return departmentCatalogRetry(sdkgo.FailureAvailability, "Qince response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == http.StatusUnauthorized {
			kind = sdkgo.FailureAuthentication
		}
		if response.StatusCode == http.StatusForbidden {
			kind = sdkgo.FailureAuthorization
		}
		return departmentCatalogBranch(ListDepartmentsBranchProviderRejected, kind, "Qince rejected the request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	var wire struct {
		ReturnCode   json.RawMessage `json:"return_code"`
		MessageID    string          `json:"msg_id"`
		ResponseData json.RawMessage `json:"response_data"`
	}
	if json.Unmarshal(body, &wire) != nil || len(wire.ReturnCode) == 0 || wire.MessageID != messageID {
		return departmentCatalogBranch(ListDepartmentsBranchInvalidResponse, sdkgo.FailureProtocol, "Qince returned invalid department-query metadata")
	}
	code := string(wire.ReturnCode)
	if len(code) > 1 && code[0] == '"' {
		var decoded string
		if json.Unmarshal(wire.ReturnCode, &decoded) != nil {
			return departmentCatalogBranch(ListDepartmentsBranchInvalidResponse, sdkgo.FailureProtocol, "Qince returned an invalid result code")
		}
		code = decoded
	}
	if code != "0" {
		return departmentCatalogBranch(ListDepartmentsBranchProviderRejected, sdkgo.FailureProviderRejection, "Qince rejected the department query")
	}
	data := wire.ResponseData
	if len(data) > 0 && data[0] == '"' {
		var decoded string
		if json.Unmarshal(data, &decoded) != nil {
			return departmentCatalogBranch(ListDepartmentsBranchInvalidResponse, sdkgo.FailureProtocol, "Qince returned invalid department data")
		}
		data = []byte(decoded)
	}
	var records []struct {
		ID     string `json:"id"`
		Code   string `json:"org_code"`
		Name   string `json:"org_name"`
		Status string `json:"org_status"`
	}
	if json.Unmarshal(data, &records) != nil || records == nil || len(records) > 10000 {
		return departmentCatalogBranch(ListDepartmentsBranchInvalidResponse, sdkgo.FailureProtocol, "Qince returned invalid department data")
	}
	departments := make([]Department, 0, len(records))
	for _, record := range records {
		if !isSafeMetadata(record.ID, 32) || !isSafeMetadata(record.Code, 100) || !isSafeMetadata(record.Name, 100) || (record.Status != "0" && record.Status != "1") {
			return departmentCatalogBranch(ListDepartmentsBranchInvalidResponse, sdkgo.FailureProtocol, "Qince returned an invalid department")
		}
		departments = append(departments, Department{ID: record.ID, Code: record.Code, Name: record.Name, Active: record.Status == "1"})
	}
	return sdkgo.NewQueryBranch(ListDepartmentsBranchListed, ListDepartmentsOutput{Departments: departments}, nil, sdkgo.Receipt{})
}
func signDepartmentQuery(body, appKey, timestamp string) string {
	sum := md5.Sum([]byte(body + "|" + appKey + "|" + timestamp))
	return hex.EncodeToString(sum[:])
}
func newMessageID() (string, error) {
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(entropy[:]), nil
}
func isValidOpenID(value string) bool { return len(value) == 19 && isASCIIAlphanumeric(value) }
func isValidAppKey(value string) bool { return len(value) == 18 && isASCIIAlphanumeric(value) }
func isASCIIAlphanumeric(value string) bool {
	for _, r := range value {
		if !(r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z') {
			return false
		}
	}
	return true
}
func isSafeMetadata(value string, maxLength int) bool {
	return value != "" && utf8.ValidString(value) && utf8.RuneCountInString(value) <= maxLength && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\r\n\x00")
}
func departmentCatalogBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListDepartmentsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "qince", Operation: "listDepartments", Message: message}
	return sdkgo.NewQueryBranch(branch, ListDepartmentsOutput{}, &failure, sdkgo.Receipt{})
}
func departmentCatalogRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListDepartmentsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "qince", Operation: "listDepartments", Message: message}
	return sdkgo.NewQueryRetry[ListDepartmentsOutput](failure, delay)
}
