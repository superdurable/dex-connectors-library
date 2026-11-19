// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package iterable

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// TrackEventInput identifies a user and supplies one custom event with non-secret data.
type TrackEventInput struct {
	// EventName is the non-empty event name configured in Iterable.
	EventName string `json:"eventName"`
	// Email identifies an email-based or hybrid project user.
	Email string `json:"email,omitempty"`
	// UserID identifies a user ID-based or hybrid project user.
	UserID string `json:"userId,omitempty"`
	// DataFields contains non-secret event properties.
	DataFields map[string]any `json:"dataFields,omitempty"`
	// CreatedAt is an optional Unix timestamp in seconds.
	CreatedAt int64 `json:"createdAt,omitempty"`
	// CampaignID is an optional positive campaign ID for attribution.
	CampaignID int64 `json:"campaignId,omitempty"`
}

// TrackEventOperation submits one custom event.
type TrackEventOperation struct{ client *Client }

// TrackEvent binds this mutation to the client.
func (client *Client) TrackEvent() TrackEventOperation { return TrackEventOperation{client} }

// Definition returns the manifest-generated mutation contract.
func (TrackEventOperation) Definition() sdkgo.MutationDefinition { return TrackEventDefinition }

// IdempotencyKey gives every retry the same event ID in Iterable.
func (TrackEventOperation) IdempotencyKey(callID sdkgo.CallID, _ TrackEventInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke tracks one event and retries ambiguous outcomes under its stable event ID.
func (operation TrackEventOperation) Invoke(call sdkgo.Call, input TrackEventInput) sdkgo.MutationAttempt[UserWriteResult] {
	if !validUserSelection(input.Email, input.UserID) || strings.TrimSpace(input.EventName) == "" || len(input.EventName) > 256 || input.CreatedAt < 0 || input.CampaignID < 0 {
		return userWriteBranch("trackEvent", TrackEventBranchDefect, sdkgo.FailureValidation, "one valid user, bounded event name, and nonnegative timestamps and IDs are required")
	}
	body := map[string]any{"id": string(call.IdempotencyKey), "eventName": input.EventName}
	if input.Email != "" {
		body["email"] = input.Email
	} else {
		body["userId"] = input.UserID
	}
	if len(input.DataFields) > 0 {
		body["dataFields"] = input.DataFields
	}
	if input.CreatedAt > 0 {
		body["createdAt"] = input.CreatedAt
	}
	if input.CampaignID > 0 {
		body["campaignId"] = input.CampaignID
	}
	requestBody, err := json.Marshal(body)
	if err != nil || len(requestBody) > 65536 {
		return userWriteBranch("trackEvent", TrackEventBranchDefect, sdkgo.FailureValidation, "event properties must form bounded JSON")
	}
	response := operation.client.request(call, http.MethodPost, "/events/track", requestBody)
	return classifyUserWrite(response, "trackEvent", TrackEventBranchTracked, "", TrackEventBranchProviderRejected, TrackEventBranchInvalidResponse, TrackEventBranchDefect)
}
