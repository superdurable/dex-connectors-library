// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package chargebee

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetSubscriptionInput selects a subscription by its non-secret Chargebee ID.
type GetSubscriptionInput struct {
	// SubscriptionID is the ID shown in Chargebee Subscriptions or returned by the API.
	SubscriptionID string `json:"subscriptionId"`
}

// SubscriptionStatus excludes customer and payment details.
type SubscriptionStatus struct {
	// ID is the Chargebee subscription ID.
	ID string `json:"id"`
	// CurrencyCode is the subscription's ISO 4217 currency.
	CurrencyCode string `json:"currencyCode"`
	// Status is the subscription state such as active.
	Status string `json:"status"`
}

// GetSubscriptionOperation reads one subscription's identity and state.
type GetSubscriptionOperation struct{ client *Client }

// GetSubscription binds the Query to this client.
func (client *Client) GetSubscription() GetSubscriptionOperation {
	return GetSubscriptionOperation{client}
}

// Definition returns the manifest-generated Query subscription.
func (GetSubscriptionOperation) Definition() sdkgo.QueryDefinition { return GetSubscriptionDefinition }

// Invoke reads one subscription and classifies retryable and definitive outcomes.
func (operation GetSubscriptionOperation) Invoke(call sdkgo.Call, input GetSubscriptionInput) sdkgo.QueryAttempt[SubscriptionStatus] {
	if !validSubscriptionID(input.SubscriptionID) {
		return subscriptionBranch(GetSubscriptionBranchDefect, sdkgo.FailureValidation, "subscription ID must be 1–50 URL-safe characters")
	}
	response := operation.client.get(call, "/subscriptions/"+url.PathEscape(input.SubscriptionID))
	if errors.Is(response.err, errCredential) {
		return subscriptionBranch(GetSubscriptionBranchDefect, sdkgo.FailureAuthentication, "Chargebee credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[SubscriptionStatus](subscriptionFailure(sdkgo.FailureTransport, "Chargebee request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[SubscriptionStatus](subscriptionFailure(sdkgo.FailureRateLimit, "Chargebee rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[SubscriptionStatus](subscriptionFailure(sdkgo.FailureAvailability, "Chargebee is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return subscriptionBranch(GetSubscriptionBranchNotFound, sdkgo.FailureNotFound, "Chargebee did not expose the subscription")
	}
	if response.status != http.StatusOK {
		return subscriptionBranch(GetSubscriptionBranchProviderRejected, sdkgo.FailureProviderRejection, "Chargebee rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return subscriptionBranch(GetSubscriptionBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Chargebee response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[SubscriptionStatus](subscriptionFailure(sdkgo.FailureTransport, "Chargebee response was interrupted"), 0)
	}
	subscriptionStatus, err := decodeSubscription(response.body, input.SubscriptionID)
	if err != nil {
		return subscriptionBranch(GetSubscriptionBranchInvalidResponse, sdkgo.FailureProtocol, "Chargebee returned invalid subscription JSON")
	}
	return sdkgo.NewQueryBranch(GetSubscriptionBranchFound, subscriptionStatus, nil, sdkgo.Receipt{})
}

func validSubscriptionID(subscriptionID string) bool {
	if len(subscriptionID) == 0 || len(subscriptionID) > 50 {
		return false
	}
	for _, character := range subscriptionID {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}

func decodeSubscription(body []byte, requestedSubscriptionID string) (SubscriptionStatus, error) {
	var wire struct {
		Subscription struct {
			ID           string `json:"id"`
			CurrencyCode string `json:"currency_code"`
			Status       string `json:"status"`
		} `json:"subscription"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return SubscriptionStatus{}, err
	}
	if wire.Subscription.ID != requestedSubscriptionID || len(wire.Subscription.CurrencyCode) != 3 || strings.TrimSpace(wire.Subscription.Status) == "" {
		return SubscriptionStatus{}, fmt.Errorf("subscription identity, currency, or status missing")
	}
	return SubscriptionStatus{ID: wire.Subscription.ID, CurrencyCode: wire.Subscription.CurrencyCode, Status: wire.Subscription.Status}, nil
}

func subscriptionFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "chargebee", Operation: "getSubscription", Message: message}
}

func subscriptionBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[SubscriptionStatus] {
	failure := subscriptionFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, SubscriptionStatus{}, &failure, sdkgo.Receipt{})
}
