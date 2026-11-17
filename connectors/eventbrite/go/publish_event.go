// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package eventbrite

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Invoke publishes the event after checking its current status, so a repeated call can
// confirm an earlier success before sending another publish request.
func (operation PublishEventOperation) Invoke(call sdkgo.Call, input PublishEventInput) sdkgo.MutationAttempt[EventMetadata] {
	if !numericID.MatchString(input.EventID) {
		return publishBranch(PublishEventBranchDefect, sdkgo.FailureValidation, "event ID is invalid")
	}
	eventPath := "/events/" + input.EventID + "/"
	current, retry, terminal := operation.client.readPublishEvent(call, eventPath)
	if retry != nil {
		return *retry
	}
	if terminal != nil {
		return *terminal
	}
	if current.Status == "live" {
		return publishSuccess(call, current)
	}
	body, status, retryAfter, err := operation.client.request(call, http.MethodPost, eventPath+"publish/")
	if errors.Is(err, errCredential) {
		return publishBranch(PublishEventBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if status == http.StatusTooManyRequests {
		return publishRetry(sdkgo.FailureRateLimit, "Eventbrite rate limited publication", retryAfter)
	}
	if status == http.StatusServiceUnavailable || status == http.StatusRequestTimeout || status >= 500 || errors.Is(err, errRequest) {
		return publishRetry(sdkgo.FailureAvailability, "Eventbrite publication outcome is not yet confirmed", retryAfter)
	}
	if status == http.StatusNotFound {
		return publishBranch(PublishEventBranchNotFound, sdkgo.FailureNotFound, "Eventbrite did not find the event")
	}
	if status != http.StatusOK {
		return publishBranch(PublishEventBranchProviderRejected, sdkgo.FailureProviderRejection, "Eventbrite rejected publication with HTTP "+strconv.Itoa(status))
	}
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return publishRetry(sdkgo.FailureResponseTooLarge, "Eventbrite accepted publication but the response exceeded the limit", 0)
	}
	if err != nil {
		return publishRetry(sdkgo.FailureTransport, "Eventbrite accepted publication but its response was interrupted", 0)
	}
	var answer struct {
		Published bool `json:"published"`
	}
	if json.Unmarshal(body, &answer) != nil || !answer.Published {
		return publishRetry(sdkgo.FailureProtocol, "Eventbrite publication response could not be confirmed", 0)
	}
	updated, retry, terminal := operation.client.readPublishEvent(call, eventPath)
	if retry != nil {
		return *retry
	}
	if terminal != nil {
		return publishRetry(sdkgo.FailureAvailability, "Eventbrite accepted publication but the event cannot yet be read", 0)
	}
	if updated.Status != "live" {
		return publishRetry(sdkgo.FailureAvailability, "Eventbrite accepted publication but the event is not yet live", 0)
	}
	return publishSuccess(call, updated)
}

func (client *Client) readPublishEvent(call sdkgo.Call, path string) (EventMetadata, *sdkgo.MutationAttempt[EventMetadata], *sdkgo.MutationAttempt[EventMetadata]) {
	body, status, retryAfter, err := client.get(call, path)
	if errors.Is(err, errCredential) {
		result := publishBranch(PublishEventBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
		return EventMetadata{}, nil, &result
	}
	if status == http.StatusTooManyRequests {
		result := publishRetry(sdkgo.FailureRateLimit, "Eventbrite rate limited the event read", retryAfter)
		return EventMetadata{}, &result, nil
	}
	if status == http.StatusRequestTimeout || status >= 500 || errors.Is(err, errRequest) {
		result := publishRetry(sdkgo.FailureAvailability, "Eventbrite event read is temporarily unavailable", retryAfter)
		return EventMetadata{}, &result, nil
	}
	if status == http.StatusNotFound {
		result := publishBranch(PublishEventBranchNotFound, sdkgo.FailureNotFound, "Eventbrite did not find the event")
		return EventMetadata{}, nil, &result
	}
	if status != http.StatusOK {
		result := publishBranch(PublishEventBranchProviderRejected, sdkgo.FailureProviderRejection, "Eventbrite rejected event read with HTTP "+strconv.Itoa(status))
		return EventMetadata{}, nil, &result
	}
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		result := publishBranch(PublishEventBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Eventbrite event response exceeded the configured limit")
		return EventMetadata{}, nil, &result
	}
	if err != nil {
		result := publishRetry(sdkgo.FailureTransport, "Eventbrite event response was interrupted", 0)
		return EventMetadata{}, &result, nil
	}
	event, err := decodeEvent(body)
	if err != nil {
		result := publishBranch(PublishEventBranchInvalidResponse, sdkgo.FailureProtocol, "Eventbrite returned invalid event JSON")
		return EventMetadata{}, nil, &result
	}
	return event, nil, nil
}

func publishSuccess(call sdkgo.Call, event EventMetadata) sdkgo.MutationAttempt[EventMetadata] {
	return sdkgo.NewMutationBranch(PublishEventBranchPublished, event, nil, sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "eventbrite", ProviderObjectID: event.ID, ObservedAt: time.Now()})
}

func publishBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[EventMetadata] {
	failure := queryFailure("publishEvent", kind, message)
	return sdkgo.NewMutationBranch(branch, EventMetadata{}, &failure, sdkgo.Receipt{})
}

func publishRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.MutationAttempt[EventMetadata] {
	return sdkgo.NewMutationRetry[EventMetadata](queryFailure("publishEvent", kind, message), delay)
}
