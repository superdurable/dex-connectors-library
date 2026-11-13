// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package xingyapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

var instanceGUIDPattern = regexp.MustCompile(`^inst_[A-Za-z0-9_-]{1,128}$`)
var accountIDPattern = regexp.MustCompile(`^[0-9]{1,20}$`)

// GetInstanceOnlineStatusInput identifies an existing managed Enterprise WeChat instance.
type GetInstanceOnlineStatusInput struct {
	// InstanceGUID is the inst_xxxx identifier shown on the provider's 实例 page.
	InstanceGUID string `json:"instanceGUID"`
}

// GetInstanceOnlineStatusOutput reports whether a managed account is online.
type GetInstanceOnlineStatusOutput struct {
	// IsOnline is true when the provider returns code 0 and a nonzero account ID.
	IsOnline bool `json:"isOnline"`
	// AccountID is the provider's decimal uin when online; it is empty when offline.
	AccountID string `json:"accountID"`
}

// GetInstanceOnlineStatusOperation checks one managed Enterprise WeChat instance.
type GetInstanceOnlineStatusOperation struct{ client *Client }

// Definition returns the generated instance status contract.
func (GetInstanceOnlineStatusOperation) Definition() sdkgo.QueryDefinition {
	return GetInstanceOnlineStatusDefinition
}

// Invoke calls the provider's read-only personal/getInfo endpoint with a fixed request shape.
func (operation GetInstanceOnlineStatusOperation) Invoke(call sdkgo.Call, input GetInstanceOnlineStatusInput) sdkgo.QueryAttempt[GetInstanceOnlineStatusOutput] {
	if !instanceGUIDPattern.MatchString(input.InstanceGUID) {
		return statusBranch(GetInstanceOnlineStatusBranchDefect, sdkgo.FailureValidation, "XingyAPI requires an inst_xxxx instance GUID")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAPIKey(credentials.APIKey.Reveal()) {
		return statusBranch(GetInstanceOnlineStatusBranchDefect, sdkgo.FailureAuthentication, "XingyAPI API Key is unavailable or invalid")
	}
	body, err := json.Marshal(struct {
		InstanceGUID string `json:"instance_guid"`
	}{input.InstanceGUID})
	if err != nil {
		return statusBranch(GetInstanceOnlineStatusBranchDefect, sdkgo.FailureValidation, "XingyAPI instance request is invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, instanceOnlineStatusEndpoint, bytes.NewReader(body))
	if err != nil {
		return statusBranch(GetInstanceOnlineStatusBranchDefect, sdkgo.FailureValidation, "XingyAPI instance request is invalid")
	}
	request.Header.Set("X-Nebula-Key", credentials.APIKey.Reveal())
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return statusRetry(sdkgo.FailureAvailability, "XingyAPI instance request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return statusRetry(sdkgo.FailureRateLimit, "XingyAPI rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return statusRetry(sdkgo.FailureAvailability, "XingyAPI instance service is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return classifyStatusHTTP(response.StatusCode)
	}
	responseBody, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return statusBranch(GetInstanceOnlineStatusBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "XingyAPI response exceeded the configured limit")
	}
	if err != nil {
		return statusRetry(sdkgo.FailureAvailability, "XingyAPI response was interrupted", 0)
	}
	var wire struct {
		OK      *bool  `json:"ok"`
		Code    *int   `json:"code"`
		Message string `json:"message"`
		Data    *struct {
			UIN json.RawMessage `json:"uin"`
		} `json:"data"`
	}
	if json.Unmarshal(responseBody, &wire) != nil || wire.OK == nil || wire.Code == nil {
		return statusBranch(GetInstanceOnlineStatusBranchInvalidResponse, sdkgo.FailureProtocol, "XingyAPI returned invalid instance data")
	}
	if *wire.Code == 50001 && !*wire.OK && wire.Message == "no account is currently logged in on this device" {
		return sdkgo.NewQueryBranch(GetInstanceOnlineStatusBranchChecked, GetInstanceOnlineStatusOutput{IsOnline: false}, nil, sdkgo.Receipt{})
	}
	if *wire.Code == 50000 {
		return statusRetry(sdkgo.FailureAvailability, "XingyAPI upstream service is temporarily unavailable", retryAfter)
	}
	if *wire.Code != 0 {
		kind := sdkgo.FailureProviderRejection
		if *wire.Code == 401 {
			kind = sdkgo.FailureAuthentication
		}
		if *wire.Code == 40301 {
			kind = sdkgo.FailureAuthorization
		}
		return statusBranch(GetInstanceOnlineStatusBranchProviderRejected, kind, "XingyAPI rejected instance request with code "+strconv.Itoa(*wire.Code))
	}
	if !*wire.OK || wire.Data == nil {
		return statusBranch(GetInstanceOnlineStatusBranchInvalidResponse, sdkgo.FailureProtocol, "XingyAPI returned inconsistent instance data")
	}
	accountID, isValid := parseAccountID(wire.Data.UIN)
	if !isValid {
		return statusBranch(GetInstanceOnlineStatusBranchInvalidResponse, sdkgo.FailureProtocol, "XingyAPI returned invalid account ID")
	}
	if accountID == "0" {
		return sdkgo.NewQueryBranch(GetInstanceOnlineStatusBranchChecked, GetInstanceOnlineStatusOutput{IsOnline: false}, nil, sdkgo.Receipt{})
	}
	return sdkgo.NewQueryBranch(GetInstanceOnlineStatusBranchChecked, GetInstanceOnlineStatusOutput{IsOnline: true, AccountID: accountID}, nil, sdkgo.Receipt{})
}

func parseAccountID(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	value := string(raw)
	if strings.HasPrefix(value, `"`) {
		if json.Unmarshal(raw, &value) != nil {
			return "", false
		}
	}
	return value, accountIDPattern.MatchString(value)
}

func classifyStatusHTTP(status int) sdkgo.QueryAttempt[GetInstanceOnlineStatusOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return statusBranch(GetInstanceOnlineStatusBranchProviderRejected, kind, "XingyAPI rejected instance request with HTTP "+strconv.Itoa(status))
}

func statusBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[GetInstanceOnlineStatusOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "xingyapi", Operation: "getInstanceOnlineStatus", Message: message}
	return sdkgo.NewQueryBranch(branch, GetInstanceOnlineStatusOutput{}, &failure, sdkgo.Receipt{})
}
func statusRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetInstanceOnlineStatusOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "xingyapi", Operation: "getInstanceOnlineStatus", Message: message}
	return sdkgo.NewQueryRetry[GetInstanceOnlineStatusOutput](failure, delay)
}
