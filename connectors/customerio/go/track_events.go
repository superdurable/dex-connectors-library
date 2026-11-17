// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package customerio

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// TrackCustomerEventInput records an action for one identified customer.
type TrackCustomerEventInput struct {
	// Identifier is a workspace ID, email, or prefixed cio_id.
	Identifier string `json:"identifier"`
	// Name is the event name referenced by Customer.io automations.
	Name string `json:"name"`
	// Data contains non-secret event properties.
	Data map[string]any `json:"data,omitempty"`
	// TimestampUnix is the optional event time in UTC Unix seconds; zero uses receipt time.
	TimestampUnix int64 `json:"timestampUnix,omitempty"`
}

// TrackAnonymousEventInput records an action before a person is identified.
type TrackAnonymousEventInput struct {
	// AnonymousID is an optional unique visitor ID that can later be linked to a person.
	AnonymousID string `json:"anonymousID,omitempty"`
	// Name is the event name referenced by Customer.io automations.
	Name string `json:"name"`
	// Data contains non-secret event properties.
	Data map[string]any `json:"data,omitempty"`
	// TimestampUnix is the optional event time in UTC Unix seconds; zero uses receipt time.
	TimestampUnix int64 `json:"timestampUnix,omitempty"`
}

// TrackEventOutput confirms which event and subject were submitted.
type TrackEventOutput struct {
	// Name is the submitted event name.
	Name string `json:"name"`
	// SubjectID is the submitted customer or anonymous identifier, when present.
	SubjectID string `json:"subjectID,omitempty"`
}

// TrackCustomerEventOperation sends an identified event with bounded repeats.
type TrackCustomerEventOperation struct{ client *Client }

// TrackAnonymousEventOperation sends an anonymous event with bounded repeats.
type TrackAnonymousEventOperation struct{ client *Client }

// TrackCustomerEvent binds the operation to this client.
func (client *Client) TrackCustomerEvent() TrackCustomerEventOperation {
	return TrackCustomerEventOperation{client}
}

// TrackAnonymousEvent binds the operation to this client.
func (client *Client) TrackAnonymousEvent() TrackAnonymousEventOperation {
	return TrackAnonymousEventOperation{client}
}

// Definition returns the generated customer-event contract.
func (TrackCustomerEventOperation) Definition() sdkgo.MutationDefinition {
	return TrackCustomerEventDefinition
}

// Definition returns the generated anonymous-event contract.
func (TrackAnonymousEventOperation) Definition() sdkgo.MutationDefinition {
	return TrackAnonymousEventDefinition
}

// IdempotencyKey correlates attempts; Customer.io does not consume this key.
func (TrackCustomerEventOperation) IdempotencyKey(id sdkgo.CallID, _ TrackCustomerEventInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// IdempotencyKey correlates attempts; Customer.io does not consume this key.
func (TrackAnonymousEventOperation) IdempotencyKey(id sdkgo.CallID, _ TrackAnonymousEventInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// Invoke sends an event associated with one customer.
func (operation TrackCustomerEventOperation) Invoke(call sdkgo.Call, input TrackCustomerEventInput) sdkgo.MutationAttempt[TrackEventOutput] {
	if !validCustomerIdentifier(input.Identifier) || !validEventName(input.Name) || input.TimestampUnix < 0 {
		return eventBranch("trackCustomerEvent", TrackCustomerEventBranchDefect, sdkgo.FailureValidation, "invalid customer event input")
	}
	payload, err := json.Marshal(struct {
		Name          string         `json:"name"`
		Data          map[string]any `json:"data,omitempty"`
		TimestampUnix int64          `json:"timestamp,omitempty"`
	}{input.Name, input.Data, input.TimestampUnix})
	if err != nil || len(payload) > 32*1024 {
		return eventBranch("trackCustomerEvent", TrackCustomerEventBranchDefect, sdkgo.FailureValidation, "invalid or oversized customer event data")
	}
	return operation.client.sendTrackEvent(call, "trackCustomerEvent", TrackCustomerEventDefinition, "/api/v1/customers/"+url.PathEscape(input.Identifier)+"/events", payload, TrackEventOutput{Name: input.Name, SubjectID: input.Identifier}, TrackCustomerEventBranchTracked, TrackCustomerEventBranchProviderRejected, TrackCustomerEventBranchDefect)
}

// Invoke sends an event that can later be associated with a person.
func (operation TrackAnonymousEventOperation) Invoke(call sdkgo.Call, input TrackAnonymousEventInput) sdkgo.MutationAttempt[TrackEventOutput] {
	if (input.AnonymousID != "" && !validCustomerIdentifier(input.AnonymousID)) || !validEventName(input.Name) || input.TimestampUnix < 0 {
		return eventBranch("trackAnonymousEvent", TrackAnonymousEventBranchDefect, sdkgo.FailureValidation, "invalid anonymous event input")
	}
	payload, err := json.Marshal(struct {
		Name          string         `json:"name"`
		AnonymousID   string         `json:"anonymous_id,omitempty"`
		Data          map[string]any `json:"data,omitempty"`
		TimestampUnix int64          `json:"timestamp,omitempty"`
	}{input.Name, input.AnonymousID, input.Data, input.TimestampUnix})
	if err != nil || len(payload) > 32*1024 {
		return eventBranch("trackAnonymousEvent", TrackAnonymousEventBranchDefect, sdkgo.FailureValidation, "invalid or oversized anonymous event data")
	}
	return operation.client.sendTrackEvent(call, "trackAnonymousEvent", TrackAnonymousEventDefinition, "/api/v1/events", payload, TrackEventOutput{Name: input.Name, SubjectID: input.AnonymousID}, TrackAnonymousEventBranchTracked, TrackAnonymousEventBranchProviderRejected, TrackAnonymousEventBranchDefect)
}

func validEventName(name string) bool {
	return name != "" && len(name) <= 100 && utf8.ValidString(name) && strings.TrimSpace(name) == name && !strings.ContainsAny(name, "\r\n\t")
}

func (client *Client) sendTrackEvent(call sdkgo.Call, operation string, definition sdkgo.MutationDefinition, path string, payload []byte, output TrackEventOutput, trackedBranch sdkgo.BranchID, rejectedBranch sdkgo.BranchID, defectBranch sdkgo.BranchID) sdkgo.MutationAttempt[TrackEventOutput] {
	fence, err := singledispatch.NewFence(call, definition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return eventBranch(operation, defectBranch, sdkgo.FailureLocalDefect, "event dispatch fence unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return eventUncertain(operation)
	}
	credentials, err := client.resolveTrackCredentials(call)
	if err != nil {
		if fence.RecordNotSent() != nil {
			return sdkgo.NewMutationRetry[TrackEventOutput](trackWriteFailure(operation, sdkgo.FailureAvailability, "event credential checkpoint unavailable"), 0)
		}
		if decision.MaximumPriorSends > 0 {
			return eventUncertain(operation)
		}
		return eventBranch(operation, defectBranch, sdkgo.FailureAuthentication, "Customer.io Track credentials are unavailable or invalid")
	}
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = client.sendTrackRequest(requestContext, credentials, http.MethodPost, path, payload)
		return response.err
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[TrackEventOutput](trackWriteFailure(operation, sdkgo.FailureAvailability, "event dispatch did not start"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[TrackEventOutput](trackWriteFailure(operation, sdkgo.FailureAvailability, "event retry checkpoint unavailable"), 0)
		}
		kind := sdkgo.FailureAvailability
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return sdkgo.NewMutationRetry[TrackEventOutput](trackWriteFailure(operation, kind, "Customer.io did not accept the event"), response.retryAfter)
	}
	if response.err != nil || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[TrackEventOutput](trackWriteFailure(operation, sdkgo.FailureTransport, "event outcome is unknown"), 0)
	}
	if response.status != http.StatusOK {
		if decision.MaximumPriorSends > 0 {
			return eventUncertain(operation)
		}
		branch := rejectedBranch
		kind := sdkgo.FailureProviderRejection
		if response.status == http.StatusUnauthorized {
			branch, kind = defectBranch, sdkgo.FailureAuthentication
		}
		return eventBranch(operation, branch, kind, "Customer.io rejected HTTP "+strconv.Itoa(response.status))
	}
	return sdkgo.NewMutationBranch(trackedBranch, output, nil, sdkgo.Receipt{})
}

func eventUncertain(operation string) sdkgo.MutationAttempt[TrackEventOutput] {
	return sdkgo.NewMutationUncertain(TrackEventOutput{}, trackWriteFailure(operation, sdkgo.FailureTransport, "previous event send cannot be confirmed"), sdkgo.Receipt{})
}

func eventBranch(operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[TrackEventOutput] {
	failure := trackWriteFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, TrackEventOutput{}, &failure, sdkgo.Receipt{})
}
