// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package heap

import (
	"bytes"
	"crypto/sha256"
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

// TrackEventInput records one event for a stable Heap identity.
type TrackEventInput struct {
	// Identity is a permanent, case-sensitive identity already used by Heap.
	Identity string `json:"identity"`
	// Event is the custom event name, up to 1024 bytes.
	Event string `json:"event"`
	// Timestamp optionally specifies an RFC3339 event time; blank uses ingestion time.
	Timestamp string `json:"timestamp,omitempty"`
	// Properties are non-secret event properties; reserved Heap keys are forbidden.
	Properties map[string]string `json:"properties,omitempty"`
}

// EventTracked confirms Heap accepted an event for ingestion.
type EventTracked struct {
	// Accepted is true after Heap returns HTTP 200.
	Accepted bool `json:"accepted"`
}

// SetAccountPropertiesInput overwrites selected properties for an account.
type SetAccountPropertiesInput struct {
	// AccountID is the stable account identity configured in Heap.
	AccountID string `json:"accountId"`
	// Properties are non-secret absolute replacement values.
	Properties map[string]string `json:"properties"`
}

// AccountPropertiesResult confirms Heap accepted account properties.
type AccountPropertiesResult struct {
	// Accepted is true after Heap returns HTTP 200.
	Accepted bool `json:"accepted"`
}

// TrackEventOperation sends a deduplicated server-side event.
type TrackEventOperation struct{ client *Client }

// TrackEvent binds event tracking to this client.
func (client *Client) TrackEvent() TrackEventOperation { return TrackEventOperation{client} }

// Definition returns the generated event Mutation contract.
func (TrackEventOperation) Definition() sdkgo.MutationDefinition { return TrackEventDefinition }

// IdempotencyKey derives a stable provider key from the Dex call identity.
func (TrackEventOperation) IdempotencyKey(callID sdkgo.CallID, _ TrackEventInput) sdkgo.IdempotencyKey {
	digest := sha256.Sum256([]byte(callID))
	return sdkgo.IdempotencyKey(hex.EncodeToString(digest[:]))
}

// Invoke sends one Heap event with its provider deduplication key.
func (operation TrackEventOperation) Invoke(call sdkgo.Call, input TrackEventInput) sdkgo.MutationAttempt[EventTracked] {
	if !validHeapIdentity(input.Identity) || strings.TrimSpace(input.Event) == "" || !utf8.ValidString(input.Event) || len(input.Event) > 1024 || !validHeapEventProperties(input.Properties) || !validHeapTimestamp(input.Timestamp) {
		return heapMutationBranch("trackEvent", TrackEventBranchDefect, sdkgo.FailureValidation, "event identity, name, timestamp, or properties are invalid", EventTracked{})
	}
	payload, err := json.Marshal(struct {
		AppID          string            `json:"app_id"`
		Identity       string            `json:"identity"`
		Event          string            `json:"event"`
		Timestamp      string            `json:"timestamp,omitempty"`
		Properties     map[string]string `json:"properties,omitempty"`
		IdempotencyKey string            `json:"idempotency_key"`
	}{operation.client.appID, input.Identity, input.Event, input.Timestamp, input.Properties, string(call.IdempotencyKey)})
	if err != nil || len(payload) > 65536 {
		return heapMutationBranch("trackEvent", TrackEventBranchDefect, sdkgo.FailureValidation, "event payload exceeds the limit", EventTracked{})
	}
	return sendHeapAcceptance(operation.client, call, "trackEvent", "/api/track", payload, TrackEventBranchTracked, TrackEventBranchProviderRejected, TrackEventBranchInvalidResponse, TrackEventBranchDefect, EventTracked{Accepted: true})
}

// SetAccountPropertiesOperation overwrites selected Heap account properties.
type SetAccountPropertiesOperation struct{ client *Client }

// SetAccountProperties binds an account update to this client.
func (client *Client) SetAccountProperties() SetAccountPropertiesOperation {
	return SetAccountPropertiesOperation{client}
}

// Definition returns the generated account Mutation contract.
func (SetAccountPropertiesOperation) Definition() sdkgo.MutationDefinition {
	return SetAccountPropertiesDefinition
}

// IdempotencyKey derives a stable call key; repeated absolute updates are safe.
func (SetAccountPropertiesOperation) IdempotencyKey(callID sdkgo.CallID, _ SetAccountPropertiesInput) sdkgo.IdempotencyKey {
	digest := sha256.Sum256([]byte(callID))
	return sdkgo.IdempotencyKey(hex.EncodeToString(digest[:]))
}

// Invoke sends an absolute Heap account-property update.
func (operation SetAccountPropertiesOperation) Invoke(call sdkgo.Call, input SetAccountPropertiesInput) sdkgo.MutationAttempt[AccountPropertiesResult] {
	if strings.TrimSpace(input.AccountID) == "" || !utf8.ValidString(input.AccountID) || len(input.AccountID) > 255 || !validHeapStringProperties(input.Properties) {
		return heapMutationBranch("setAccountProperties", SetAccountPropertiesBranchDefect, sdkgo.FailureValidation, "account identity or properties are invalid", AccountPropertiesResult{})
	}
	payload, err := json.Marshal(struct {
		AppID      string            `json:"app_id"`
		AccountID  string            `json:"account_id"`
		Properties map[string]string `json:"properties"`
	}{operation.client.appID, input.AccountID, input.Properties})
	if err != nil || len(payload) > 65536 {
		return heapMutationBranch("setAccountProperties", SetAccountPropertiesBranchDefect, sdkgo.FailureValidation, "account payload exceeds the limit", AccountPropertiesResult{})
	}
	return sendHeapAcceptance(operation.client, call, "setAccountProperties", "/api/add_account_properties", payload, SetAccountPropertiesBranchAccepted, SetAccountPropertiesBranchProviderRejected, SetAccountPropertiesBranchInvalidResponse, SetAccountPropertiesBranchDefect, AccountPropertiesResult{Accepted: true})
}

func validHeapIdentity(identity string) bool {
	return strings.TrimSpace(identity) != "" && utf8.ValidString(identity) && len(identity) <= 255
}
func validHeapTimestamp(timestamp string) bool {
	if timestamp == "" {
		return true
	}
	_, err := time.Parse(time.RFC3339, timestamp)
	return err == nil
}
func validHeapStringProperties(properties map[string]string) bool {
	if len(properties) == 0 || len(properties) > 50 {
		return false
	}
	for key, value := range properties {
		if strings.TrimSpace(key) == "" || !utf8.ValidString(key) || !utf8.ValidString(value) || len(key) >= 1024 || len(value) >= 1024 {
			return false
		}
	}
	return true
}
func validHeapEventProperties(properties map[string]string) bool {
	if len(properties) > 50 {
		return false
	}
	for key, value := range properties {
		if strings.TrimSpace(key) == "" || !utf8.ValidString(key) || !utf8.ValidString(value) || len(key) >= 1024 || len(value) >= 1024 || key == "user_id" || key == "session_id" || key == "screen_name" {
			return false
		}
	}
	return true
}

func sendHeapAcceptance[T any](client *Client, call sdkgo.Call, operationID, path string, payload []byte, successBranch, rejectedBranch, invalidBranch, defectBranch sdkgo.BranchID, accepted T) sdkgo.MutationAttempt[T] {
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, client.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return heapMutationBranch(operationID, defectBranch, sdkgo.FailureLocalDefect, "Heap request could not be built", accepted)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return heapMutationRetry[T](operationID, sdkgo.FailureTransport, "Heap response is unavailable", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return heapMutationRetry[T](operationID, sdkgo.FailureRateLimit, "Heap rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return heapMutationRetry[T](operationID, sdkgo.FailureAvailability, "Heap is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return heapMutationBranch(operationID, rejectedBranch, sdkgo.FailureProviderRejection, "Heap rejected HTTP "+strconv.Itoa(response.StatusCode), accepted)
	}
	if _, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes); err != nil {
		if errors.Is(err, providerhttp.ErrBodyTooLarge) {
			return heapMutationBranch(operationID, invalidBranch, sdkgo.FailureResponseTooLarge, "Heap confirmation exceeded the limit", accepted)
		}
		return heapMutationRetry[T](operationID, sdkgo.FailureTransport, "Heap confirmation was interrupted", 0)
	}
	return sdkgo.NewMutationBranch(successBranch, accepted, nil, sdkgo.Receipt{})
}
func heapMutationBranch[T any](operationID string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string, _ T) sdkgo.MutationAttempt[T] {
	failure := sdkgo.Failure{Kind: kind, Provider: "heap", Operation: operationID, Message: message}
	return sdkgo.NewMutationBranch(branch, *new(T), &failure, sdkgo.Receipt{})
}
func heapMutationRetry[T any](operationID string, kind sdkgo.FailureKind, message string, after time.Duration) sdkgo.MutationAttempt[T] {
	return sdkgo.NewMutationRetry[T](sdkgo.Failure{Kind: kind, Provider: "heap", Operation: operationID, Message: message}, after)
}
