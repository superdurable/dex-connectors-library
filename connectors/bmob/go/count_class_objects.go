// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bmob

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

// CountClassObjectsInput identifies one existing Bmob class.
type CountClassObjectsInput struct {
	// ClassName is the case-sensitive Bmob class name from the app's Data console.
	ClassName string `json:"className"`
}

// CountClassObjectsOutput contains the count without object fields or identifiers.
type CountClassObjectsOutput struct {
	// Count is the number of objects matching the class, as returned by Bmob.
	Count int64 `json:"count"`
}

// CountClassObjectsOperation reads the count of one class.
type CountClassObjectsOperation struct{ client *Client }

// Definition returns the generated count contract.
func (CountClassObjectsOperation) Definition() sdkgo.QueryDefinition {
	return CountClassObjectsDefinition
}

// Invoke requests only a class count and classifies provider and transport outcomes.
func (operation CountClassObjectsOperation) Invoke(call sdkgo.Call, input CountClassObjectsInput) sdkgo.QueryAttempt[CountClassObjectsOutput] {
	if !isValidClassName(input.ClassName) {
		return classCountBranch(CountClassObjectsBranchDefect, sdkgo.FailureValidation, "class name is invalid")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidCredential(credentials.ApplicationID.Reveal()) || !isValidCredential(credentials.RESTAPIKey.Reveal()) {
		return classCountBranch(CountClassObjectsBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	query := url.Values{"count": {"1"}, "limit": {"0"}}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+"/1/classes/"+input.ClassName+"?"+query.Encode(), nil)
	if err != nil {
		return classCountBranch(CountClassObjectsBranchDefect, sdkgo.FailureValidation, "class count request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-Bmob-Application-Id", credentials.ApplicationID.Reveal())
	request.Header.Set("X-Bmob-REST-API-Key", credentials.RESTAPIKey.Reveal())
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return classCountRetry(sdkgo.FailureAvailability, "Bmob request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return classCountRetry(sdkgo.FailureRateLimit, "Bmob rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError {
		return classCountRetry(sdkgo.FailureAvailability, "Bmob is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return classCountBranch(CountClassObjectsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Bmob response exceeded the configured limit")
	}
	if err != nil {
		return classCountRetry(sdkgo.FailureAvailability, "Bmob response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		kind := sdkgo.FailureProviderRejection
		switch response.StatusCode {
		case http.StatusUnauthorized:
			kind = sdkgo.FailureAuthentication
		case http.StatusForbidden:
			kind = sdkgo.FailureAuthorization
		case http.StatusBadRequest, http.StatusNotFound:
			kind = sdkgo.FailureValidation
		}
		return classCountBranch(CountClassObjectsBranchProviderRejected, kind, "Bmob rejected the request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	var wire struct {
		Count   *int64            `json:"count"`
		Results []json.RawMessage `json:"results"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Count == nil || *wire.Count < 0 || wire.Results == nil || len(wire.Results) != 0 {
		return classCountBranch(CountClassObjectsBranchInvalidResponse, sdkgo.FailureProtocol, "Bmob returned an invalid count response")
	}
	return sdkgo.NewQueryBranch(CountClassObjectsBranchCounted, CountClassObjectsOutput{Count: *wire.Count}, nil, sdkgo.Receipt{})
}

func isValidClassName(className string) bool {
	if len(className) == 0 || len(className) > 128 || (className[0] < 'A' || className[0] > 'Z') && (className[0] < 'a' || className[0] > 'z') {
		return false
	}
	for _, character := range className {
		if !(character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '_') {
			return false
		}
	}
	return true
}
func isValidCredential(value string) bool {
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
func classCountBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[CountClassObjectsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "bmob", Operation: "countClassObjects", Message: message}
	return sdkgo.NewQueryBranch(branch, CountClassObjectsOutput{}, &failure, sdkgo.Receipt{})
}
func classCountRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[CountClassObjectsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "bmob", Operation: "countClassObjects", Message: message}
	return sdkgo.NewQueryRetry[CountClassObjectsOutput](failure, delay)
}
