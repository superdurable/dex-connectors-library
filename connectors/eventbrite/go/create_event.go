// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package eventbrite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateEventInput describes one new Eventbrite draft. StartUTC and EndUTC use RFC 3339 UTC;
// Timezone is an IANA name, and Currency is an ISO 4217 code.
type CreateEventInput struct {
	OrganizationID string `json:"organizationId"`
	Name           string `json:"name"`
	StartUTC       string `json:"startUtc"`
	EndUTC         string `json:"endUtc"`
	Timezone       string `json:"timezone"`
	Currency       string `json:"currency"`
}

// CopyEventInput selects an event whose configuration will be copied into a new draft.
type CopyEventInput struct {
	EventID string `json:"eventId"`
}

// CreateEventOperation creates an Eventbrite draft with bounded duplicate sends.
type CreateEventOperation struct{ client *Client }

// CopyEventOperation copies an Eventbrite event with bounded duplicate sends.
type CopyEventOperation struct{ client *Client }

// CreateEvent binds the create mutation to this client.
func (client *Client) CreateEvent() CreateEventOperation { return CreateEventOperation{client: client} }

// CopyEvent binds the copy mutation to this client.
func (client *Client) CopyEvent() CopyEventOperation { return CopyEventOperation{client: client} }

// Definition returns the manifest-generated create contract.
func (CreateEventOperation) Definition() sdkgo.MutationDefinition { return CreateEventDefinition }

// Definition returns the manifest-generated copy contract.
func (CopyEventOperation) Definition() sdkgo.MutationDefinition { return CopyEventDefinition }

// IdempotencyKey returns the Dex call ID for receipt correlation; Eventbrite does not deduplicate it.
func (CreateEventOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateEventInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey returns the Dex call ID for receipt correlation; Eventbrite does not deduplicate it.
func (CopyEventOperation) IdempotencyKey(callID sdkgo.CallID, _ CopyEventInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

type eventCreationBranches struct{ success, notFound, rejected, uncertain, defect sdkgo.BranchID }

// Invoke creates a draft with one fenced POST per attempt and at most call.MaxSends possible sends.
func (operation CreateEventOperation) Invoke(call sdkgo.Call, input CreateEventInput) sdkgo.MutationAttempt[EventMetadata] {
	if err := validateCreateEventInput(input); err != nil {
		return eventCreationBranch("createEvent", CreateEventBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	payload := map[string]any{"event": map[string]any{
		"name":     map[string]string{"html": html.EscapeString(input.Name)},
		"start":    map[string]string{"utc": input.StartUTC, "timezone": input.Timezone},
		"end":      map[string]string{"utc": input.EndUTC, "timezone": input.Timezone},
		"currency": input.Currency,
	}}
	body, err := json.Marshal(payload)
	if err != nil {
		return eventCreationBranch("createEvent", CreateEventBranchDefect, sdkgo.FailureLocalDefect, "event request could not be encoded")
	}
	return operation.client.invokeEventCreation(call, "createEvent", http.MethodPost,
		"/organizations/"+input.OrganizationID+"/events/", body,
		CreateEventDefinition.StepDefaults.ExecuteMethodTimeout,
		eventCreationBranches{success: CreateEventBranchCreated, rejected: CreateEventBranchProviderRejected, uncertain: CreateEventBranchUncertain, defect: CreateEventBranchDefect})
}

// Invoke copies an event into a new draft with a bounded number of fenced POSTs.
func (operation CopyEventOperation) Invoke(call sdkgo.Call, input CopyEventInput) sdkgo.MutationAttempt[EventMetadata] {
	if !numericID.MatchString(input.EventID) {
		return eventCreationBranch("copyEvent", CopyEventBranchDefect, sdkgo.FailureValidation, "event ID is invalid")
	}
	return operation.client.invokeEventCreation(call, "copyEvent", http.MethodPost,
		"/events/"+input.EventID+"/copy/", nil,
		CopyEventDefinition.StepDefaults.ExecuteMethodTimeout,
		eventCreationBranches{success: CopyEventBranchCopied, notFound: CopyEventBranchNotFound, rejected: CopyEventBranchProviderRejected, uncertain: CopyEventBranchUncertain, defect: CopyEventBranchDefect})
}

func (client *Client) invokeEventCreation(call sdkgo.Call, operation, method, path string, body []byte, timeout time.Duration, branches eventCreationBranches) sdkgo.MutationAttempt[EventMetadata] {
	fence, err := singledispatch.NewFence(call, timeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return eventCreationBranch(operation, branches.defect, sdkgo.FailureLocalDefect, "event dispatch fence is unavailable")
	}
	decision := fence.Decide()
	switch decision.Kind {
	case singledispatch.FailClosed:
		return eventCreationUncertain(call, operation, sdkgo.FailureAvailability, "an earlier event creation may have succeeded", "")
	case singledispatch.ReadBackRequired:
		return client.readBackCreatedEvent(call, operation, decision.Marker.ProviderID, branches)
	case singledispatch.ReadBackThenResendAllowed:
		if decision.Marker.ProviderID != "" {
			return client.readBackCreatedEvent(call, operation, decision.Marker.ProviderID, branches)
		}
	}
	canResendUnknown := decision.MaximumPriorSends+1 < call.MaxSends()
	hasPriorSend := decision.MaximumPriorSends > 0
	var responseBody []byte
	var status int
	var retryAfter time.Duration
	var sendErr error
	err = fence.Dispatch(func(requestContext context.Context) error {
		responseBody, status, retryAfter, sendErr = client.sendWithBody(call, requestContext, method, path, body)
		return nil
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return eventCreationRetry(operation, sdkgo.FailureAvailability, "event was not sent before a safe dispatch checkpoint", 0)
	}
	if err != nil {
		return eventCreationUncertain(call, operation, sdkgo.FailureTransport, "event dispatch outcome is unknown", "")
	}
	if errors.Is(sendErr, errCredential) {
		if hasPriorSend {
			return eventCreationUncertain(call, operation, sdkgo.FailureAuthentication, "an earlier event creation may have succeeded; reconnect the connection", "")
		}
		if err := fence.RecordNotSent(); err != nil {
			return eventCreationRetry(operation, sdkgo.FailureAvailability, "event no-send checkpoint is unavailable", 0)
		}
		return eventCreationBranch(operation, branches.defect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return eventCreationRetry(operation, sdkgo.FailureAvailability, "event resend checkpoint is unavailable", 0)
		}
		kind := sdkgo.FailureRateLimit
		if status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return eventCreationRetry(operation, kind, "Eventbrite did not accept the event request", retryAfter)
	}
	if status == http.StatusRequestTimeout || status >= 500 || errors.Is(sendErr, errRequest) {
		if canResendUnknown {
			return eventCreationRetry(operation, sdkgo.FailureAvailability, "event creation outcome is unknown; retry within the send cap", retryAfter)
		}
		return eventCreationUncertain(call, operation, sdkgo.FailureAvailability, "event creation outcome is unknown after the send cap", "")
	}
	if status == http.StatusNotFound && branches.notFound != "" {
		if hasPriorSend {
			return eventCreationUncertain(call, operation, sdkgo.FailureNotFound, "source event disappeared after an earlier copy may have succeeded", "")
		}
		return eventCreationBranch(operation, branches.notFound, sdkgo.FailureNotFound, "Eventbrite did not find the source event")
	}
	if status < 200 || status >= 300 {
		if hasPriorSend {
			return eventCreationUncertain(call, operation, sdkgo.FailureProviderRejection, "an earlier event creation may have succeeded before this rejection", "")
		}
		return eventCreationBranch(operation, branches.rejected, sdkgo.FailureProviderRejection, fmt.Sprintf("Eventbrite rejected HTTP %d", status))
	}
	if errors.Is(sendErr, providerhttp.ErrBodyTooLarge) || sendErr != nil {
		return eventCreationUncertain(call, operation, sdkgo.FailureProtocol, "Eventbrite accepted the event but its identity was not returned", "")
	}
	created, err := decodeEvent(responseBody)
	if err != nil {
		return eventCreationUncertain(call, operation, sdkgo.FailureProtocol, "Eventbrite accepted the event but returned no usable identity", "")
	}
	_ = fence.RecordProviderID(created.ID)
	return sdkgo.NewMutationBranch(branches.success, created, nil, eventCreationReceipt(call, created.ID))
}

func (client *Client) readBackCreatedEvent(call sdkgo.Call, operation, eventID string, branches eventCreationBranches) sdkgo.MutationAttempt[EventMetadata] {
	if !numericID.MatchString(eventID) {
		return eventCreationUncertain(call, operation, sdkgo.FailureProtocol, "an earlier create has no usable event ID for read-back", "")
	}
	body, status, delay, err := client.get(call, "/events/"+eventID+"/")
	if status == http.StatusTooManyRequests || status >= 500 || err != nil || status == http.StatusNotFound {
		return eventCreationRetry(operation, sdkgo.FailureAvailability, "created event is not yet readable", delay)
	}
	if status != http.StatusOK {
		return eventCreationUncertain(call, operation, sdkgo.FailureProviderRejection, "created event read-back was rejected", eventID)
	}
	event, err := decodeEvent(body)
	if err != nil || event.ID != eventID {
		return eventCreationUncertain(call, operation, sdkgo.FailureProtocol, "created event read-back did not match", eventID)
	}
	return sdkgo.NewMutationBranch(branches.success, event, nil, eventCreationReceipt(call, event.ID))
}

func eventCreationReceipt(call sdkgo.Call, eventID string) sdkgo.Receipt {
	return sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "eventbrite", ProviderObjectID: eventID, ObservedAt: time.Now()}
}

func eventCreationBranch(operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[EventMetadata] {
	failure := queryFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, EventMetadata{}, &failure, sdkgo.Receipt{})
}

func eventCreationRetry(operation string, kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.MutationAttempt[EventMetadata] {
	return sdkgo.NewMutationRetry[EventMetadata](queryFailure(operation, kind, message), delay)
}

func eventCreationUncertain(call sdkgo.Call, operation string, kind sdkgo.FailureKind, message, eventID string) sdkgo.MutationAttempt[EventMetadata] {
	return sdkgo.NewMutationUncertain(EventMetadata{}, queryFailure(operation, kind, message), eventCreationReceipt(call, eventID))
}

var currencyCodePattern = regexp.MustCompile(`^[A-Z]{3}$`)

func validateCreateEventInput(input CreateEventInput) error {
	if !numericID.MatchString(input.OrganizationID) {
		return errors.New("organization ID is invalid")
	}
	if strings.TrimSpace(input.Name) == "" || len(input.Name) > 255 {
		return errors.New("event name must be 1 to 255 bytes")
	}
	start, err := time.Parse(time.RFC3339, input.StartUTC)
	if err != nil || start.Location() != time.UTC {
		return errors.New("startUtc must be an RFC 3339 UTC timestamp")
	}
	end, err := time.Parse(time.RFC3339, input.EndUTC)
	if err != nil || end.Location() != time.UTC || !end.After(start) {
		return errors.New("endUtc must be an RFC 3339 UTC timestamp after startUtc")
	}
	if input.Timezone == "" {
		return errors.New("timezone is required")
	}
	if _, err := time.LoadLocation(input.Timezone); err != nil {
		return errors.New("timezone must be an IANA time zone")
	}
	if !currencyCodePattern.MatchString(input.Currency) {
		return errors.New("currency must be an uppercase ISO 4217 code")
	}
	return nil
}
