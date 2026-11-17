// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package recurly

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// SubscriptionSummary contains only the subscription ID and lifecycle state.
type SubscriptionSummary struct {
	// ID is the Recurly subscription ID.
	ID string `json:"id"`
	// State is the provider lifecycle state, such as active or canceled.
	State string `json:"state"`
}

// SubscriptionPage contains a bounded list of subscription states.
type SubscriptionPage struct {
	// Subscriptions excludes account, billing, and payment details.
	Subscriptions []SubscriptionSummary `json:"subscriptions"`
	// HasMore reports whether another page exists.
	HasMore bool `json:"hasMore"`
}

// ListSubscriptionsInput selects a page of 1 to 200 subscriptions.
type ListSubscriptionsInput struct {
	// Limit is the requested page size from 1 to 200.
	Limit int `json:"limit"`
}

// GetSubscriptionInput selects one subscription by ID.
type GetSubscriptionInput struct {
	// SubscriptionID is the Recurly subscription ID.
	SubscriptionID string `json:"subscriptionId"`
}

// CancelSubscriptionInput selects one subscription to cancel at term end.
type CancelSubscriptionInput struct {
	// SubscriptionID is the active or future subscription to cancel.
	SubscriptionID string `json:"subscriptionId"`
}

// ReactivateSubscriptionInput selects one canceled subscription to reactivate.
type ReactivateSubscriptionInput struct {
	// SubscriptionID is the canceled subscription to reactivate.
	SubscriptionID string `json:"subscriptionId"`
}

// ListSubscriptionsOperation lists subscription states.
type ListSubscriptionsOperation struct{ client *Client }

// ListSubscriptions binds the Query to this client.
func (client *Client) ListSubscriptions() ListSubscriptionsOperation {
	return ListSubscriptionsOperation{client}
}

// Definition returns the manifest-generated Query contract.
func (ListSubscriptionsOperation) Definition() sdkgo.QueryDefinition {
	return ListSubscriptionsDefinition
}

// Invoke lists subscriptions without customer or payment details.
func (operation ListSubscriptionsOperation) Invoke(call sdkgo.Call, input ListSubscriptionsInput) sdkgo.QueryAttempt[SubscriptionPage] {
	if input.Limit < 1 || input.Limit > 200 {
		return subscriptionPageBranch(ListSubscriptionsBranchDefect, sdkgo.FailureValidation, "Recurly subscription page size is invalid")
	}
	response := operation.client.get(call, "/subscriptions?limit="+strconv.Itoa(input.Limit))
	if errors.Is(response.err, errCredential) {
		return subscriptionPageBranch(ListSubscriptionsBranchDefect, sdkgo.FailureAuthentication, "Recurly credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return subscriptionPageRetry(sdkgo.FailureTransport, "Recurly subscription list request failed", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return subscriptionPageRetry(sdkgo.FailureRateLimit, "Recurly rate limited subscription listing", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return subscriptionPageRetry(sdkgo.FailureAvailability, "Recurly subscription listing is unavailable", response.retryAfter)
	}
	if response.status != http.StatusOK {
		return subscriptionPageBranch(ListSubscriptionsBranchProviderRejected, sdkgo.FailureProviderRejection, "Recurly rejected subscription listing")
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return subscriptionPageBranch(ListSubscriptionsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Recurly subscription list exceeded the response limit")
	}
	if response.err != nil {
		return subscriptionPageRetry(sdkgo.FailureTransport, "Recurly subscription list was interrupted", 0)
	}
	var wire struct {
		Object  string            `json:"object"`
		Data    []json.RawMessage `json:"data"`
		HasMore bool              `json:"has_more"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Object != "list" || wire.Data == nil {
		return subscriptionPageBranch(ListSubscriptionsBranchInvalidResponse, sdkgo.FailureProtocol, "Recurly subscription list is invalid")
	}
	page := SubscriptionPage{Subscriptions: make([]SubscriptionSummary, 0, len(wire.Data)), HasMore: wire.HasMore}
	for _, raw := range wire.Data {
		summary, valid := decodeSubscription(raw)
		if !valid {
			return subscriptionPageBranch(ListSubscriptionsBranchInvalidResponse, sdkgo.FailureProtocol, "Recurly subscription list item is invalid")
		}
		page.Subscriptions = append(page.Subscriptions, summary)
	}
	return sdkgo.NewQueryBranch(ListSubscriptionsBranchListed, page, nil, sdkgo.Receipt{})
}

// GetSubscriptionOperation reads one subscription state.
type GetSubscriptionOperation struct{ client *Client }

// GetSubscription binds the Query to this client.
func (client *Client) GetSubscription() GetSubscriptionOperation {
	return GetSubscriptionOperation{client}
}

// Definition returns the manifest-generated Query contract.
func (GetSubscriptionOperation) Definition() sdkgo.QueryDefinition { return GetSubscriptionDefinition }

// Invoke reads one subscription by ID.
func (operation GetSubscriptionOperation) Invoke(call sdkgo.Call, input GetSubscriptionInput) sdkgo.QueryAttempt[SubscriptionSummary] {
	if !validAccountID(input.SubscriptionID) {
		return getSubscriptionBranch(GetSubscriptionBranchDefect, sdkgo.FailureValidation, "Recurly subscription ID is invalid")
	}
	response := operation.client.get(call, "/subscriptions/"+url.PathEscape(input.SubscriptionID))
	if errors.Is(response.err, errCredential) {
		return getSubscriptionBranch(GetSubscriptionBranchDefect, sdkgo.FailureAuthentication, "Recurly credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return getSubscriptionRetry(sdkgo.FailureTransport, "Recurly subscription request failed", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return getSubscriptionRetry(sdkgo.FailureRateLimit, "Recurly rate limited subscription lookup", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return getSubscriptionRetry(sdkgo.FailureAvailability, "Recurly subscription lookup is unavailable", response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return getSubscriptionBranch(GetSubscriptionBranchNotFound, sdkgo.FailureNotFound, "Recurly subscription was not found")
	}
	if response.status != http.StatusOK {
		return getSubscriptionBranch(GetSubscriptionBranchProviderRejected, sdkgo.FailureProviderRejection, "Recurly rejected subscription lookup")
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return getSubscriptionBranch(GetSubscriptionBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Recurly subscription exceeded the response limit")
	}
	if response.err != nil {
		return getSubscriptionRetry(sdkgo.FailureTransport, "Recurly subscription response was interrupted", 0)
	}
	summary, valid := decodeSubscription(response.body)
	if !valid || summary.ID != input.SubscriptionID {
		return getSubscriptionBranch(GetSubscriptionBranchInvalidResponse, sdkgo.FailureProtocol, "Recurly subscription response is invalid")
	}
	return sdkgo.NewQueryBranch(GetSubscriptionBranchFound, summary, nil, sdkgo.Receipt{})
}

// CancelSubscriptionOperation cancels a subscription at its term end.
type CancelSubscriptionOperation struct{ client *Client }

// CancelSubscription binds the Mutation to this client.
func (client *Client) CancelSubscription() CancelSubscriptionOperation {
	return CancelSubscriptionOperation{client}
}

// Definition returns the manifest-generated Mutation contract.
func (CancelSubscriptionOperation) Definition() sdkgo.MutationDefinition {
	return CancelSubscriptionDefinition
}

// IdempotencyKey records the stable Dex Call ID for this state change.
func (CancelSubscriptionOperation) IdempotencyKey(callID sdkgo.CallID, _ CancelSubscriptionInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke cancels a subscription and confirms a repeated cancellation by reading its state.
func (operation CancelSubscriptionOperation) Invoke(call sdkgo.Call, input CancelSubscriptionInput) sdkgo.MutationAttempt[SubscriptionSummary] {
	return operation.invokeStateChange(call, input.SubscriptionID, "cancel", "canceled", CancelSubscriptionBranchCancelled, CancelSubscriptionBranchProviderRejected, CancelSubscriptionBranchDefect)
}
func (operation CancelSubscriptionOperation) invokeStateChange(call sdkgo.Call, id, action, wanted string, success, rejected, defect sdkgo.BranchID) sdkgo.MutationAttempt[SubscriptionSummary] {
	return changeSubscriptionState(operation.client, call, id, action, wanted, "cancelSubscription", success, rejected, defect)
}

// ReactivateSubscriptionOperation reactivates a canceled subscription.
type ReactivateSubscriptionOperation struct{ client *Client }

// ReactivateSubscription binds the Mutation to this client.
func (client *Client) ReactivateSubscription() ReactivateSubscriptionOperation {
	return ReactivateSubscriptionOperation{client}
}

// Definition returns the manifest-generated Mutation contract.
func (ReactivateSubscriptionOperation) Definition() sdkgo.MutationDefinition {
	return ReactivateSubscriptionDefinition
}

// IdempotencyKey records the stable Dex Call ID for this state change.
func (ReactivateSubscriptionOperation) IdempotencyKey(callID sdkgo.CallID, _ ReactivateSubscriptionInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reactivates a subscription and confirms a repeated activation by reading its state.
func (operation ReactivateSubscriptionOperation) Invoke(call sdkgo.Call, input ReactivateSubscriptionInput) sdkgo.MutationAttempt[SubscriptionSummary] {
	return changeSubscriptionState(operation.client, call, input.SubscriptionID, "reactivate", "active", "reactivateSubscription", ReactivateSubscriptionBranchReactivated, ReactivateSubscriptionBranchProviderRejected, ReactivateSubscriptionBranchDefect)
}

func changeSubscriptionState(client *Client, call sdkgo.Call, id, action, wanted, operation string, success, rejected, defect sdkgo.BranchID) sdkgo.MutationAttempt[SubscriptionSummary] {
	if !validAccountID(id) {
		return subscriptionMutationBranch(operation, defect, sdkgo.FailureValidation, "Recurly subscription ID is invalid")
	}
	response := client.putSubscription(call, "/subscriptions/"+url.PathEscape(id)+"/"+action)
	if errors.Is(response.err, errCredential) {
		return subscriptionMutationBranch(operation, defect, sdkgo.FailureAuthentication, "Recurly credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return subscriptionMutationRetry(operation, sdkgo.FailureTransport, "Recurly subscription request failed", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return subscriptionMutationRetry(operation, sdkgo.FailureRateLimit, "Recurly rate limited subscription update", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return subscriptionMutationRetry(operation, sdkgo.FailureAvailability, "Recurly subscription outcome is unknown", response.retryAfter)
	}
	if response.status == http.StatusUnprocessableEntity {
		return confirmSubscriptionState(client, call, id, wanted, operation, success, rejected)
	}
	if response.status != http.StatusOK {
		return subscriptionMutationBranch(operation, rejected, sdkgo.FailureProviderRejection, "Recurly rejected subscription update")
	}
	if response.err != nil {
		return subscriptionMutationRetry(operation, sdkgo.FailureTransport, "Recurly subscription response was interrupted", 0)
	}
	summary, valid := decodeSubscription(response.body)
	if !valid || summary.ID != id || (summary.State != wanted && !(action == "cancel" && summary.State == "failed")) {
		return subscriptionMutationRetry(operation, sdkgo.FailureProtocol, "Recurly subscription may have changed but response was invalid", 0)
	}
	return sdkgo.NewMutationBranch(success, summary, nil, sdkgo.Receipt{})
}

func confirmSubscriptionState(client *Client, call sdkgo.Call, id, wanted, operation string, success, rejected sdkgo.BranchID) sdkgo.MutationAttempt[SubscriptionSummary] {
	response := client.get(call, "/subscriptions/"+url.PathEscape(id))
	if response.err != nil || response.status == http.StatusTooManyRequests || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return subscriptionMutationRetry(operation, sdkgo.FailureAvailability, "Recurly subscription state could not be checked", response.retryAfter)
	}
	if response.status != http.StatusOK {
		return subscriptionMutationBranch(operation, rejected, sdkgo.FailureProviderRejection, "Recurly rejected subscription state lookup")
	}
	summary, valid := decodeSubscription(response.body)
	if !valid {
		return subscriptionMutationRetry(operation, sdkgo.FailureProtocol, "Recurly subscription state is invalid", 0)
	}
	if summary.ID != id || (summary.State != wanted && !(wanted == "canceled" && summary.State == "failed")) {
		return subscriptionMutationBranch(operation, rejected, sdkgo.FailureProviderRejection, "Recurly subscription is in another state")
	}
	return sdkgo.NewMutationBranch(success, summary, nil, sdkgo.Receipt{})
}

func (client *Client) putSubscription(call sdkgo.Call, path string) providerResponse {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return providerResponse{err: errCredential}
	}
	key := credentials.APIKey.Reveal()
	if !providerhttp.IsHeaderSafeCredential(key) {
		return providerResponse{err: errCredential}
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPut, client.baseURL+path, bytes.NewReader([]byte("{}")))
	if err != nil {
		return providerResponse{err: errRequest}
	}
	request.Header.Set("Accept", "application/vnd.recurly.v2021-02-25+json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(key+":")))
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	defer func() { _ = response.Body.Close() }()
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	return providerResponse{body: body, status: response.StatusCode, retryAfter: providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()), err: err}
}

func decodeSubscription(body []byte) (SubscriptionSummary, bool) {
	var wire struct {
		ID    string `json:"id"`
		State string `json:"state"`
	}
	if json.Unmarshal(body, &wire) != nil || !validAccountID(wire.ID) || wire.State == "" {
		return SubscriptionSummary{}, false
	}
	return SubscriptionSummary{ID: wire.ID, State: wire.State}, true
}
func subscriptionFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "recurly", Operation: operation, Message: message}
}
func subscriptionPageBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[SubscriptionPage] {
	failure := subscriptionFailure("listSubscriptions", kind, message)
	return sdkgo.NewQueryBranch(branch, SubscriptionPage{}, &failure, sdkgo.Receipt{})
}
func subscriptionPageRetry(kind sdkgo.FailureKind, message string, after time.Duration) sdkgo.QueryAttempt[SubscriptionPage] {
	return sdkgo.NewQueryRetry[SubscriptionPage](subscriptionFailure("listSubscriptions", kind, message), after)
}
func getSubscriptionBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[SubscriptionSummary] {
	failure := subscriptionFailure("getSubscription", kind, message)
	return sdkgo.NewQueryBranch(branch, SubscriptionSummary{}, &failure, sdkgo.Receipt{})
}
func getSubscriptionRetry(kind sdkgo.FailureKind, message string, after time.Duration) sdkgo.QueryAttempt[SubscriptionSummary] {
	return sdkgo.NewQueryRetry[SubscriptionSummary](subscriptionFailure("getSubscription", kind, message), after)
}
func subscriptionMutationBranch(operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[SubscriptionSummary] {
	failure := subscriptionFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, SubscriptionSummary{}, &failure, sdkgo.Receipt{})
}
func subscriptionMutationRetry(operation string, kind sdkgo.FailureKind, message string, after time.Duration) sdkgo.MutationAttempt[SubscriptionSummary] {
	return sdkgo.NewMutationRetry[SubscriptionSummary](subscriptionFailure(operation, kind, message), after)
}
