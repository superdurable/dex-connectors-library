// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pingpp

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetChargeStatusInput identifies a Charge created in the API Key's test or live environment.
type GetChargeStatusInput struct {
	// ChargeID is the Ping++ Charge identifier, beginning with ch_.
	ChargeID string `json:"chargeId"`
}

// GetChargeStatusOutput contains only payment-state flags, not payment credentials or customer details.
type GetChargeStatusOutput struct {
	// ChargeID is the verified Charge identifier.
	ChargeID string `json:"chargeId"`
	// Paid indicates whether Ping++ recorded a successful payment.
	Paid bool `json:"paid"`
	// Refunded indicates whether the Charge has refund information.
	Refunded bool `json:"refunded"`
	// Reversed indicates whether the Charge was reversed.
	Reversed bool `json:"reversed"`
	// LiveMode indicates whether this Charge belongs to the live environment.
	LiveMode bool `json:"liveMode"`
}

// GetChargeStatusOperation reads one Charge's status.
type GetChargeStatusOperation struct{ client *Client }

// Definition returns the generated Charge status Query contract.
func (GetChargeStatusOperation) Definition() sdkgo.QueryDefinition { return GetChargeStatusDefinition }

// Invoke sends one authenticated read-only GET and classifies provider outcomes.
func (operation GetChargeStatusOperation) Invoke(call sdkgo.Call, input GetChargeStatusInput) sdkgo.QueryAttempt[GetChargeStatusOutput] {
	if !isValidChargeID(input.ChargeID) {
		return chargeStatusBranch(GetChargeStatusBranchDefect, sdkgo.FailureValidation, "Charge ID is invalid")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAPIKey(credentials.APIKey.Reveal()) {
		return chargeStatusBranch(GetChargeStatusBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+"/v1/charges/"+input.ChargeID, nil)
	if err != nil {
		return chargeStatusBranch(GetChargeStatusBranchDefect, sdkgo.FailureValidation, "Charge request is invalid")
	}
	request.SetBasicAuth(credentials.APIKey.Reveal(), "")
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return chargeStatusRetry(sdkgo.FailureAvailability, "Ping++ request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusForbidden {
		return chargeStatusRetry(sdkgo.FailureRateLimit, "Ping++ rate or concurrency limit reached", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError {
		return chargeStatusRetry(sdkgo.FailureAvailability, "Ping++ is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return chargeStatusBranch(GetChargeStatusBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Ping++ response exceeded the configured limit")
	}
	if err != nil {
		return chargeStatusRetry(sdkgo.FailureAvailability, "Ping++ response was interrupted", 0)
	}
	if response.StatusCode == http.StatusNotFound {
		return chargeStatusBranch(GetChargeStatusBranchNotFound, sdkgo.FailureProviderRejection, "Ping++ Charge was not found")
	}
	if response.StatusCode != http.StatusOK {
		return chargeStatusBranch(GetChargeStatusBranchProviderRejected, classifyPingppStatus(response.StatusCode), "Ping++ rejected the Charge lookup")
	}
	var wire struct {
		ID       string `json:"id"`
		Object   string `json:"object"`
		Paid     *bool  `json:"paid"`
		Refunded *bool  `json:"refunded"`
		Reversed *bool  `json:"reversed"`
		LiveMode *bool  `json:"livemode"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.ID != input.ChargeID || wire.Object != "charge" || wire.Paid == nil || wire.Refunded == nil || wire.Reversed == nil || wire.LiveMode == nil {
		return chargeStatusBranch(GetChargeStatusBranchInvalidResponse, sdkgo.FailureProtocol, "Ping++ returned an invalid Charge status")
	}
	output := GetChargeStatusOutput{ChargeID: wire.ID, Paid: *wire.Paid, Refunded: *wire.Refunded, Reversed: *wire.Reversed, LiveMode: *wire.LiveMode}
	return sdkgo.NewQueryBranch(GetChargeStatusBranchFetched, output, nil, sdkgo.Receipt{})
}
func isValidChargeID(chargeID string) bool {
	if !strings.HasPrefix(chargeID, "ch_") || len(chargeID) < 4 || len(chargeID) > 128 {
		return false
	}
	for _, character := range chargeID[3:] {
		if !((character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '_' || character == '-') {
			return false
		}
	}
	return true
}
func isValidAPIKey(apiKey string) bool {
	if apiKey == "" || len(apiKey) > 512 || strings.TrimSpace(apiKey) != apiKey {
		return false
	}
	for _, character := range apiKey {
		if character <= 0x20 || character >= 0x7f {
			return false
		}
	}
	return true
}
func classifyPingppStatus(statusCode int) sdkgo.FailureKind {
	switch statusCode {
	case http.StatusUnauthorized:
		return sdkgo.FailureAuthentication
	case http.StatusBadRequest:
		return sdkgo.FailureValidation
	default:
		return sdkgo.FailureProviderRejection
	}
}
func chargeStatusBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[GetChargeStatusOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "pingpp", Operation: "getChargeStatus", Message: message}
	return sdkgo.NewQueryBranch(branch, GetChargeStatusOutput{}, &failure, sdkgo.Receipt{})
}
func chargeStatusRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetChargeStatusOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "pingpp", Operation: "getChargeStatus", Message: message}
	return sdkgo.NewQueryRetry[GetChargeStatusOutput](failure, delay)
}
