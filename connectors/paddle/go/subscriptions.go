// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package paddle

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListSubscriptionsInput selects a page of subscriptions.
type ListSubscriptionsInput struct {
	// PerPage is the page size from 1 through 200.
	PerPage int `json:"perPage"`
	// After is the sub_ cursor from a previous page, or blank for the first page.
	After string `json:"after,omitempty"`
}

// SubscriptionIDInput identifies one Paddle subscription.
type SubscriptionIDInput struct {
	// SubscriptionID is a Paddle sub_ identifier.
	SubscriptionID string `json:"subscriptionId"`
}

// SetNextBillingDateInput shifts the next renewal without an immediate charge.
type SetNextBillingDateInput struct {
	// SubscriptionID is the Paddle sub_ identifier.
	SubscriptionID string `json:"subscriptionId"`
	// NextBilledAt is the new next renewal time in RFC 3339 format.
	NextBilledAt string `json:"nextBilledAt"`
}

// PaddleSubscription contains the identity, state, and next renewal time.
type PaddleSubscription struct {
	// ID is the Paddle sub_ identifier.
	ID string `json:"id"`
	// Status is the provider's lifecycle state.
	Status string `json:"status"`
	// NextBilledAt is the next renewal time or blank when absent.
	NextBilledAt string `json:"nextBilledAt"`
}

// SubscriptionPage contains one page of subscriptions.
type SubscriptionPage struct {
	// Items are the subscription summaries in this page.
	Items []PaddleSubscription `json:"items"`
	// HasMore reports whether another page follows.
	HasMore bool `json:"hasMore"`
	// NextCursor is the next page's sub_ cursor, or blank on the last page.
	NextCursor string `json:"nextCursor"`
}

// ListSubscriptionsOperation lists subscriptions.
type ListSubscriptionsOperation struct{ client *Client }

// GetSubscriptionOperation reads one subscription.
type GetSubscriptionOperation struct{ client *Client }

// SetNextBillingDateOperation shifts the next renewal without immediate billing.
type SetNextBillingDateOperation struct{ client *Client }

// ListSubscriptions binds the listing Query.
func (client *Client) ListSubscriptions() ListSubscriptionsOperation {
	return ListSubscriptionsOperation{client}
}

// GetSubscription binds the lookup Query.
func (client *Client) GetSubscription() GetSubscriptionOperation {
	return GetSubscriptionOperation{client}
}

// SetNextBillingDate binds the renewal scheduling Mutation.
func (client *Client) SetNextBillingDate() SetNextBillingDateOperation {
	return SetNextBillingDateOperation{client}
}

// Definition returns the generated listing contract.
func (ListSubscriptionsOperation) Definition() sdkgo.QueryDefinition {
	return ListSubscriptionsDefinition
}

// Definition returns the generated lookup contract.
func (GetSubscriptionOperation) Definition() sdkgo.QueryDefinition { return GetSubscriptionDefinition }

// Definition returns the generated renewal scheduling contract.
func (SetNextBillingDateOperation) Definition() sdkgo.MutationDefinition {
	return SetNextBillingDateDefinition
}

// IdempotencyKey records the stable call identity for the absolute replacement.
func (SetNextBillingDateOperation) IdempotencyKey(callID sdkgo.CallID, _ SetNextBillingDateInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

func validSubscriptionStatus(status string) bool {
	switch status {
	case "active", "canceled", "past_due", "paused", "trialing":
		return true
	default:
		return false
	}
}

func decodeSubscription(body []byte, expectedID string) (PaddleSubscription, bool) {
	var envelope struct {
		Data struct {
			ID           string  `json:"id"`
			Status       string  `json:"status"`
			NextBilledAt *string `json:"next_billed_at"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &envelope) != nil || !validPaddleID(envelope.Data.ID, "sub_") || expectedID != "" && envelope.Data.ID != expectedID || !validSubscriptionStatus(envelope.Data.Status) {
		return PaddleSubscription{}, false
	}
	result := PaddleSubscription{ID: envelope.Data.ID, Status: envelope.Data.Status}
	if envelope.Data.NextBilledAt != nil {
		if _, err := time.Parse(time.RFC3339Nano, *envelope.Data.NextBilledAt); err != nil {
			return PaddleSubscription{}, false
		}
		result.NextBilledAt = *envelope.Data.NextBilledAt
	}
	return result, true
}

func decodeSubscriptionPage(body []byte) (SubscriptionPage, bool) {
	var envelope struct {
		Data []json.RawMessage `json:"data"`
		Meta struct {
			Pagination struct {
				Next    string `json:"next"`
				HasMore bool   `json:"has_more"`
			} `json:"pagination"`
		} `json:"meta"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.Data == nil {
		return SubscriptionPage{}, false
	}
	page := SubscriptionPage{Items: make([]PaddleSubscription, 0, len(envelope.Data)), HasMore: envelope.Meta.Pagination.HasMore}
	for _, item := range envelope.Data {
		wrapped, err := json.Marshal(map[string]json.RawMessage{"data": item})
		if err != nil {
			return SubscriptionPage{}, false
		}
		subscription, ok := decodeSubscription(wrapped, "")
		if !ok {
			return SubscriptionPage{}, false
		}
		page.Items = append(page.Items, subscription)
	}
	if page.HasMore {
		next, err := url.Parse(envelope.Meta.Pagination.Next)
		if err != nil || next.Path != "/subscriptions" || !validPaddleID(next.Query().Get("after"), "sub_") {
			return SubscriptionPage{}, false
		}
		page.NextCursor = next.Query().Get("after")
	}
	return page, true
}

func subscriptionFailure(name string, kind sdkgo.FailureKind, message string) *sdkgo.Failure {
	return &sdkgo.Failure{Kind: kind, Provider: "paddle", Operation: name, Message: message}
}

// Invoke lists one page of subscriptions.
func (operation ListSubscriptionsOperation) Invoke(call sdkgo.Call, input ListSubscriptionsInput) sdkgo.QueryAttempt[SubscriptionPage] {
	const name = "listSubscriptions"
	if input.PerPage < 1 || input.PerPage > 200 || input.After != "" && !validPaddleID(input.After, "sub_") {
		return sdkgo.NewQueryBranch(ListSubscriptionsBranchDefect, SubscriptionPage{}, subscriptionFailure(name, sdkgo.FailureValidation, "subscription page input is invalid"), sdkgo.Receipt{})
	}
	query := url.Values{"per_page": {strconv.Itoa(input.PerPage)}}
	if input.After != "" {
		query.Set("after", input.After)
	}
	exchange := operation.client.exchange(call, call.Context, http.MethodGet, "/subscriptions?"+query.Encode(), nil)
	if errors.Is(exchange.err, errCredential) {
		return sdkgo.NewQueryBranch(ListSubscriptionsBranchDefect, SubscriptionPage{}, subscriptionFailure(name, sdkgo.FailureAuthentication, "Paddle credentials are invalid"), sdkgo.Receipt{})
	}
	if paddleRetryable(exchange) {
		return sdkgo.NewQueryRetry[SubscriptionPage](*subscriptionFailure(name, sdkgo.FailureAvailability, "subscription listing is unavailable"), exchange.retryAfter)
	}
	if exchange.status != http.StatusOK {
		return sdkgo.NewQueryBranch(ListSubscriptionsBranchProviderRejected, SubscriptionPage{}, subscriptionFailure(name, sdkgo.FailureProviderRejection, "Paddle rejected subscription listing"), sdkgo.Receipt{})
	}
	page, ok := decodeSubscriptionPage(exchange.body)
	if !ok || exchange.err != nil {
		return sdkgo.NewQueryBranch(ListSubscriptionsBranchInvalidResponse, SubscriptionPage{}, subscriptionFailure(name, sdkgo.FailureProtocol, "Paddle returned an invalid subscription page"), sdkgo.Receipt{})
	}
	return sdkgo.NewQueryBranch(ListSubscriptionsBranchListed, page, nil, sdkgo.Receipt{})
}

// Invoke reads a subscription by its stable ID.
func (operation GetSubscriptionOperation) Invoke(call sdkgo.Call, input SubscriptionIDInput) sdkgo.QueryAttempt[PaddleSubscription] {
	const name = "getSubscription"
	if !validPaddleID(input.SubscriptionID, "sub_") {
		return sdkgo.NewQueryBranch(GetSubscriptionBranchDefect, PaddleSubscription{}, subscriptionFailure(name, sdkgo.FailureValidation, "subscription ID is invalid"), sdkgo.Receipt{})
	}
	exchange := operation.client.exchange(call, call.Context, http.MethodGet, "/subscriptions/"+url.PathEscape(input.SubscriptionID), nil)
	if errors.Is(exchange.err, errCredential) {
		return sdkgo.NewQueryBranch(GetSubscriptionBranchDefect, PaddleSubscription{}, subscriptionFailure(name, sdkgo.FailureAuthentication, "Paddle credentials are invalid"), sdkgo.Receipt{})
	}
	if paddleRetryable(exchange) {
		return sdkgo.NewQueryRetry[PaddleSubscription](*subscriptionFailure(name, sdkgo.FailureAvailability, "subscription lookup is unavailable"), exchange.retryAfter)
	}
	if exchange.status == http.StatusNotFound {
		return sdkgo.NewQueryBranch(GetSubscriptionBranchNotFound, PaddleSubscription{}, subscriptionFailure(name, sdkgo.FailureNotFound, "subscription was not found"), sdkgo.Receipt{})
	}
	if exchange.status != http.StatusOK {
		return sdkgo.NewQueryBranch(GetSubscriptionBranchProviderRejected, PaddleSubscription{}, subscriptionFailure(name, sdkgo.FailureProviderRejection, "Paddle rejected subscription lookup"), sdkgo.Receipt{})
	}
	subscription, ok := decodeSubscription(exchange.body, input.SubscriptionID)
	if !ok || exchange.err != nil {
		return sdkgo.NewQueryBranch(GetSubscriptionBranchInvalidResponse, PaddleSubscription{}, subscriptionFailure(name, sdkgo.FailureProtocol, "Paddle returned an invalid subscription"), sdkgo.Receipt{})
	}
	return sdkgo.NewQueryBranch(GetSubscriptionBranchFound, subscription, nil, sdkgo.Receipt{})
}

// Invoke replaces the next billing date without immediate collection.
func (operation SetNextBillingDateOperation) Invoke(call sdkgo.Call, input SetNextBillingDateInput) sdkgo.MutationAttempt[PaddleSubscription] {
	const name = "setNextBillingDate"
	if !validPaddleID(input.SubscriptionID, "sub_") {
		return sdkgo.NewMutationBranch(SetNextBillingDateBranchDefect, PaddleSubscription{}, subscriptionFailure(name, sdkgo.FailureValidation, "subscription ID is invalid"), sdkgo.Receipt{})
	}
	billingDate, err := time.Parse(time.RFC3339Nano, input.NextBilledAt)
	if err != nil || !billingDate.After(time.Now().Add(30*time.Minute)) {
		return sdkgo.NewMutationBranch(SetNextBillingDateBranchDefect, PaddleSubscription{}, subscriptionFailure(name, sdkgo.FailureValidation, "next billing date must be RFC 3339 and more than 30 minutes ahead"), sdkgo.Receipt{})
	}
	payload := map[string]string{"next_billed_at": input.NextBilledAt, "proration_billing_mode": "do_not_bill"}
	exchange := operation.client.exchange(call, call.Context, http.MethodPatch, "/subscriptions/"+url.PathEscape(input.SubscriptionID), payload)
	if errors.Is(exchange.err, errCredential) {
		return sdkgo.NewMutationBranch(SetNextBillingDateBranchDefect, PaddleSubscription{}, subscriptionFailure(name, sdkgo.FailureAuthentication, "Paddle credentials are invalid"), sdkgo.Receipt{})
	}
	if paddleRetryable(exchange) {
		return sdkgo.NewMutationRetry[PaddleSubscription](*subscriptionFailure(name, sdkgo.FailureAvailability, "subscription billing date update is unavailable"), exchange.retryAfter)
	}
	if exchange.status == http.StatusNotFound {
		return sdkgo.NewMutationBranch(SetNextBillingDateBranchNotFound, PaddleSubscription{}, subscriptionFailure(name, sdkgo.FailureNotFound, "subscription was not found"), sdkgo.Receipt{})
	}
	if exchange.status != http.StatusOK {
		return sdkgo.NewMutationBranch(SetNextBillingDateBranchProviderRejected, PaddleSubscription{}, subscriptionFailure(name, sdkgo.FailureProviderRejection, "Paddle rejected the next billing date"), sdkgo.Receipt{})
	}
	subscription, ok := decodeSubscription(exchange.body, input.SubscriptionID)
	if !ok || exchange.err != nil || subscription.NextBilledAt != input.NextBilledAt {
		return sdkgo.NewMutationBranch(SetNextBillingDateBranchInvalidResponse, PaddleSubscription{}, subscriptionFailure(name, sdkgo.FailureProtocol, "Paddle returned an invalid subscription"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(SetNextBillingDateBranchUpdated, subscription, nil, sdkgo.Receipt{ProviderObjectID: subscription.ID})
}
