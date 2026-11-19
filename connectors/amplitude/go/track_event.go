// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package amplitude

import (
	"encoding/json"
	"errors"
	"net/http"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const trackEventOperationID = "trackEvent"

// TrackEventInput uploads one event for an existing or new Amplitude user.
type TrackEventInput struct {
	// UserID is a non-secret project user ID of at least five UTF-8 bytes.
	UserID string `json:"userID"`
	// EventType is the nonempty event name visible in Amplitude's event catalog.
	EventType string `json:"eventType"`
	// EventProperties are optional string properties; blank map sends no properties.
	EventProperties map[string]string `json:"eventProperties,omitempty"`
}

// TrackedEvent identifies the event accepted by Amplitude.
type TrackedEvent struct {
	// UserID is the input user ID.
	UserID string `json:"userID"`
	// EventType is the input event name.
	EventType string `json:"eventType"`
	// InsertID is the stable Dex-derived ID Amplitude uses to deduplicate retries for seven days.
	InsertID string `json:"insertID"`
}

// TrackEventOperation implements one HTTP V2 event upload.
type TrackEventOperation struct{ client *Client }

// TrackEvent binds the Mutation to this client.
func (client *Client) TrackEvent() TrackEventOperation { return TrackEventOperation{client} }

// Definition returns the generated Mutation contract.
func (TrackEventOperation) Definition() sdkgo.MutationDefinition { return TrackEventDefinition }

// IdempotencyKey supplies the event insert ID for provider deduplication.
func (TrackEventOperation) IdempotencyKey(callID sdkgo.CallID, _ TrackEventInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke uploads one event and retries an unknown outcome with the same insert ID.
func (operation TrackEventOperation) Invoke(call sdkgo.Call, input TrackEventInput) sdkgo.MutationAttempt[TrackedEvent] {
	if len(input.UserID) < 5 || !validUserSearch(input.UserID) || !validUserSearch(input.EventType) {
		return trackedBranch(TrackEventBranchDefect, sdkgo.FailureValidation, "Amplitude user ID or event type is invalid")
	}
	for name, value := range input.EventProperties {
		if !validUserSearch(name) || len(value) > 8192 || !utf8.ValidString(value) {
			return trackedBranch(TrackEventBranchDefect, sdkgo.FailureValidation, "Amplitude event property is invalid")
		}
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.APIKey.Reveal()) {
		return trackedBranch(TrackEventBranchDefect, sdkgo.FailureAuthentication, "Amplitude API key is unavailable or invalid")
	}
	insertID := string(call.IdempotencyKey)
	payload, err := json.Marshal(eventUploadRequest{APIKey: credentials.APIKey.Reveal(), Events: []eventUpload{{
		UserID: input.UserID, EventType: input.EventType, InsertID: insertID, EventProperties: input.EventProperties,
	}}})
	if err != nil || len(payload) > 1024*1024 {
		return trackedBranch(TrackEventBranchDefect, sdkgo.FailureValidation, "Amplitude event payload exceeds one MiB")
	}
	response := operation.client.post(call, "/2/httpapi", "application/json", payload)
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewMutationRetry[TrackedEvent](eventFailure(sdkgo.FailureTransport, "Amplitude event upload failed"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusRequestTimeout || response.status >= 500 || response.err != nil {
		kind := sdkgo.FailureAvailability
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		if response.err != nil {
			kind = sdkgo.FailureTransport
		}
		return sdkgo.NewMutationRetry[TrackedEvent](eventFailure(kind, "Amplitude event upload should be retried"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return trackedBranch(TrackEventBranchProviderRejected, sdkgo.FailureProviderRejection, "Amplitude rejected the event upload")
	}
	var accepted struct {
		Code           int `json:"code"`
		EventsIngested int `json:"events_ingested"`
	}
	if json.Unmarshal(response.body, &accepted) != nil || accepted.Code != 200 || accepted.EventsIngested != 1 {
		return sdkgo.NewMutationRetry[TrackedEvent](eventFailure(sdkgo.FailureProtocol, "Amplitude event upload response was invalid"), 0)
	}
	value := TrackedEvent{UserID: input.UserID, EventType: input.EventType, InsertID: insertID}
	return sdkgo.NewMutationBranch(TrackEventBranchTracked, value, nil, sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "amplitude", ProviderObjectID: insertID})
}

type eventUploadRequest struct {
	APIKey string        `json:"api_key"`
	Events []eventUpload `json:"events"`
}

type eventUpload struct {
	UserID          string            `json:"user_id"`
	EventType       string            `json:"event_type"`
	InsertID        string            `json:"insert_id"`
	EventProperties map[string]string `json:"event_properties,omitempty"`
}

func eventFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "amplitude", Operation: trackEventOperationID, Message: message}
}

func trackedBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[TrackedEvent] {
	failure := eventFailure(kind, message)
	return sdkgo.NewMutationBranch(branch, TrackedEvent{}, &failure, sdkgo.Receipt{})
}
