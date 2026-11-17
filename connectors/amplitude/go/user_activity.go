// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package amplitude

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetUserActivityInput selects a bounded page of activity by Amplitude's numeric ID.
type GetUserActivityInput struct {
	// AmplitudeID is the numeric ID returned by Amplitude User Lookup.
	AmplitudeID string `json:"amplitudeID"`
	// Offset is the zero-based offset from the most recent event.
	Offset int `json:"offset,omitempty"`
	// Limit is the requested number of events, 1–1000; zero uses 100.
	Limit int `json:"limit,omitempty"`
	// Direction is latest or earliest; blank uses latest.
	Direction string `json:"direction,omitempty"`
}

// UserActivity contains selected counts and event names without arbitrary event properties.
type UserActivity struct {
	// AmplitudeID is the requested numeric ID.
	AmplitudeID string `json:"amplitudeID"`
	// UserID is the associated user ID.
	UserID string `json:"userID"`
	// EventCount is the user's lifetime event count.
	EventCount int `json:"eventCount"`
	// SessionCount is the user's lifetime session count.
	SessionCount int `json:"sessionCount"`
	// ReturnedEvents is the number of events in this response.
	ReturnedEvents int `json:"returnedEvents"`
	// EventTypes are event names in provider order; event property payloads are not copied.
	EventTypes []string `json:"eventTypes,omitempty"`
}

// GetUserActivityOperation reads a user's activity through the Dashboard REST API.
type GetUserActivityOperation struct{ client *Client }

// GetUserActivity binds this Query to the client.
func (client *Client) GetUserActivity() GetUserActivityOperation {
	return GetUserActivityOperation{client}
}

// Definition returns the generated Query contract.
func (GetUserActivityOperation) Definition() sdkgo.QueryDefinition { return GetUserActivityDefinition }

// Invoke reads one bounded activity response.
func (operation GetUserActivityOperation) Invoke(call sdkgo.Call, input GetUserActivityInput) sdkgo.QueryAttempt[UserActivity] {
	const operationID = "getUserActivity"
	if input.Limit == 0 {
		input.Limit = 100
	}
	if input.Direction == "" {
		input.Direction = "latest"
	}
	amplitudeNumber, err := strconv.ParseUint(input.AmplitudeID, 10, 64)
	if err != nil || amplitudeNumber == 0 || input.Limit < 1 || input.Limit > 1000 || input.Offset < 0 || input.Offset > 1000000 || (input.Direction != "latest" && input.Direction != "earliest") {
		return activityBranch(GetUserActivityBranchDefect, sdkgo.FailureValidation, "Amplitude activity input is invalid")
	}
	parameters := url.Values{"user": {input.AmplitudeID}, "offset": {strconv.Itoa(input.Offset)}, "limit": {strconv.Itoa(input.Limit)}, "direction": {input.Direction}}
	response := operation.client.getDashboard(call, "/api/2/useractivity", parameters)
	if errors.Is(response.err, errCredential) {
		return activityBranch(GetUserActivityBranchDefect, sdkgo.FailureAuthentication, "Amplitude credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[UserActivity](activityFailure(sdkgo.FailureTransport, "Amplitude activity request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[UserActivity](activityFailure(sdkgo.FailureRateLimit, "Amplitude rate limited activity"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[UserActivity](activityFailure(sdkgo.FailureAvailability, "Amplitude activity is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return sdkgo.NewQueryBranch(GetUserActivityBranchMissing, UserActivity{}, nil, sdkgo.Receipt{})
	}
	if response.status != http.StatusOK {
		return activityBranch(GetUserActivityBranchProviderRejected, sdkgo.FailureProviderRejection, "Amplitude refused activity")
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return activityBranch(GetUserActivityBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Amplitude activity exceeded the response limit")
	}
	if errors.Is(response.err, errCredentialEcho) {
		return activityBranch(GetUserActivityBranchInvalidResponse, sdkgo.FailureProtocol, "Amplitude activity contained connection credentials")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[UserActivity](activityFailure(sdkgo.FailureTransport, "Amplitude activity response was interrupted"), 0)
	}
	var document struct {
		UserData *struct {
			UserID       string `json:"user_id"`
			EventCount   int    `json:"num_events"`
			SessionCount int    `json:"num_sessions"`
		} `json:"userData"`
		Events []struct {
			EventType string `json:"event_type"`
		} `json:"events"`
	}
	if json.Unmarshal(response.body, &document) != nil || document.UserData == nil || document.Events == nil || document.UserData.EventCount < 0 || document.UserData.SessionCount < 0 || len(document.Events) > 5000 {
		return activityBranch(GetUserActivityBranchInvalidResponse, sdkgo.FailureProtocol, "Amplitude returned invalid activity JSON")
	}
	value := UserActivity{AmplitudeID: input.AmplitudeID, UserID: document.UserData.UserID, EventCount: document.UserData.EventCount, SessionCount: document.UserData.SessionCount, ReturnedEvents: len(document.Events), EventTypes: make([]string, 0, len(document.Events))}
	for _, event := range document.Events {
		if event.EventType == "" || len(event.EventType) > 1024 {
			return activityBranch(GetUserActivityBranchInvalidResponse, sdkgo.FailureProtocol, "Amplitude returned invalid event type")
		}
		value.EventTypes = append(value.EventTypes, event.EventType)
	}
	return sdkgo.NewQueryBranch(GetUserActivityBranchFound, value, nil, sdkgo.Receipt{})
}

func activityFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "amplitude", Operation: "getUserActivity", Message: message}
}

func activityBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[UserActivity] {
	failure := activityFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, UserActivity{}, &failure, sdkgo.Receipt{})
}
