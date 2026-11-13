// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package feihan

import (
	"crypto/md5"
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

// QueryRechargeOrderInput identifies an existing merchant order.
type QueryRechargeOrderInput struct {
	// MerchantOrderID is the merchant's order ID supplied during recharge creation.
	MerchantOrderID string `json:"merchantOrderId"`
}

// RechargeOrderResult contains the merchant order ID and provider recharge status.
type RechargeOrderResult struct {
	// MerchantOrderID is the merchant's order ID.
	MerchantOrderID string `json:"merchantOrderId"`
	// RechargeStatus is 0 processing, 1 successful, or 9 failed.
	RechargeStatus int `json:"rechargeStatus"`
}

// QueryRechargeOrderOperation reads one Feihan recharge order's status.
type QueryRechargeOrderOperation struct{ client *Client }

// Definition returns the generated recharge-order Query contract.
func (QueryRechargeOrderOperation) Definition() sdkgo.QueryDefinition {
	return QueryRechargeOrderDefinition
}

// Invoke signs and sends one query, then classifies provider and transport outcomes.
func (operation QueryRechargeOrderOperation) Invoke(call sdkgo.Call, input QueryRechargeOrderInput) sdkgo.QueryAttempt[RechargeOrderResult] {
	if !validOrderID(input.MerchantOrderID) {
		return orderBranch(QueryRechargeOrderBranchDefect, sdkgo.FailureValidation, "Feihan merchant order ID is invalid")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !validProjectID(credentials.ProjectID) || !validCredential(credentials.Password.Reveal()) || !validCredential(credentials.KeyStr.Reveal()) {
		return orderBranch(QueryRechargeOrderBranchDefect, sdkgo.FailureAuthentication, "Feihan credentials are unavailable or invalid")
	}
	passwordDigest := md5.Sum([]byte(credentials.Password.Reveal()))
	passwordHash := hex.EncodeToString(passwordDigest[:])
	signatureDigest := md5.Sum([]byte(credentials.ProjectID + passwordHash + input.MerchantOrderID + credentials.KeyStr.Reveal()))
	form := url.Values{
		"userid":     {credentials.ProjectID},
		"userpws":    {passwordHash},
		"sporder_id": {input.MerchantOrderID},
		"md5_str":    {strings.ToUpper(hex.EncodeToString(signatureDigest[:]))},
		"version":    {"6.0"},
		"format":     {"json"},
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+"/queryOrderInfo.do", strings.NewReader(form.Encode()))
	if err != nil {
		return orderBranch(QueryRechargeOrderBranchDefect, sdkgo.FailureValidation, "Feihan order request is invalid")
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return orderRetry(sdkgo.FailureAvailability, "Feihan order request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return orderRetry(sdkgo.FailureRateLimit, "Feihan rate limited order query", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return orderRetry(sdkgo.FailureAvailability, "Feihan order API is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return orderBranch(QueryRechargeOrderBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Feihan response exceeded the configured limit")
	}
	if err != nil {
		return orderRetry(sdkgo.FailureAvailability, "Feihan order response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		return orderBranch(QueryRechargeOrderBranchProviderRejected, classifyOrderHTTP(response.StatusCode), "Feihan rejected order query with HTTP "+strconv.Itoa(response.StatusCode))
	}
	result, retcode, err := decodeRechargeOrder(body, input)
	if err != nil {
		return orderBranch(QueryRechargeOrderBranchInvalidResponse, sdkgo.FailureProtocol, "Feihan returned an invalid order response")
	}
	if retcode != "1" {
		return orderBranch(QueryRechargeOrderBranchProviderRejected, sdkgo.FailureProviderRejection, "Feihan rejected order query")
	}
	if result.RechargeStatus == -1 {
		return orderBranch(QueryRechargeOrderBranchProviderRejected, sdkgo.FailureProviderRejection, "Feihan could not find the order; manual verification is required")
	}
	return sdkgo.NewQueryBranch(QueryRechargeOrderBranchFound, result, nil, sdkgo.Receipt{})
}

func decodeRechargeOrder(body []byte, input QueryRechargeOrderInput) (RechargeOrderResult, string, error) {
	var envelope struct {
		Retcode         string `json:"retcode"`
		MerchantOrderID string `json:"sporder_id"`
		GameState       *int   `json:"game_state"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Retcode == "" {
		return RechargeOrderResult{}, "", errors.New("invalid Feihan response")
	}
	if envelope.Retcode != "1" {
		return RechargeOrderResult{}, envelope.Retcode, nil
	}
	if envelope.MerchantOrderID != input.MerchantOrderID || envelope.GameState == nil {
		return RechargeOrderResult{}, "", errors.New("invalid Feihan order status")
	}
	switch *envelope.GameState {
	case -1, 0, 1, 9:
	default:
		return RechargeOrderResult{}, "", errors.New("unknown Feihan order status")
	}
	return RechargeOrderResult{MerchantOrderID: envelope.MerchantOrderID, RechargeStatus: *envelope.GameState}, envelope.Retcode, nil
}

func validOrderID(value string) bool {
	if len(value) == 0 || len(value) > 64 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}
func validProjectID(value string) bool {
	if len(value) < 2 || len(value) > 32 || value[0] != 'A' {
		return false
	}
	for _, character := range value[1:] {
		if (character < '0' || character > '9') && (character < 'A' || character > 'Z') {
			return false
		}
	}
	return true
}
func validCredential(value string) bool {
	return value != "" && len(value) <= 2048 && strings.TrimSpace(value) == value
}
func classifyOrderHTTP(status int) sdkgo.FailureKind {
	switch status {
	case 401:
		return sdkgo.FailureAuthentication
	case 403:
		return sdkgo.FailureAuthorization
	case 400, 404, 412:
		return sdkgo.FailureValidation
	default:
		return sdkgo.FailureProviderRejection
	}
}
func orderBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[RechargeOrderResult] {
	failure := sdkgo.Failure{Kind: kind, Provider: "feihan", Operation: "queryRechargeOrder", Message: message}
	return sdkgo.NewQueryBranch(branch, RechargeOrderResult{}, &failure, sdkgo.Receipt{})
}
func orderRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[RechargeOrderResult] {
	failure := sdkgo.Failure{Kind: kind, Provider: "feihan", Operation: "queryRechargeOrder", Message: message}
	return sdkgo.NewQueryRetry[RechargeOrderResult](failure, delay)
}
