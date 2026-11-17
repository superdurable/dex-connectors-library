// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package eventbrite

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateTicketClassInput describes one free ticket class and its total capacity.
type CreateTicketClassInput struct {
	EventID       string `json:"eventId"`
	Name          string `json:"name"`
	QuantityTotal int    `json:"quantityTotal"`
}

// CreateTicketClassOperation creates a free ticket class with bounded sends.
type CreateTicketClassOperation struct{ client *Client }

// CreateTicketClass binds the mutation to this client.
func (client *Client) CreateTicketClass() CreateTicketClassOperation {
	return CreateTicketClassOperation{client}
}

// Definition returns the manifest-generated operation contract.
func (CreateTicketClassOperation) Definition() sdkgo.MutationDefinition {
	return CreateTicketClassDefinition
}

// IdempotencyKey correlates a Dex call; Eventbrite does not deduplicate this create.
func (CreateTicketClassOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateTicketClassInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a free ticket class through a single-dispatch fence.
func (operation CreateTicketClassOperation) Invoke(call sdkgo.Call, input CreateTicketClassInput) sdkgo.MutationAttempt[TicketClassMetadata] {
	if !numericID.MatchString(input.EventID) || strings.TrimSpace(input.Name) == "" || len(input.Name) > 255 || input.QuantityTotal < 1 {
		return ticketCreationBranch(CreateTicketClassBranchDefect, sdkgo.FailureValidation, "event ID, name, or quantity is invalid")
	}
	requestBody, err := json.Marshal(map[string]any{"ticket_class": map[string]any{"name": input.Name, "quantity_total": input.QuantityTotal, "free": true}})
	if err != nil {
		return ticketCreationBranch(CreateTicketClassBranchDefect, sdkgo.FailureLocalDefect, "ticket class request could not be encoded")
	}
	fence, err := singledispatch.NewFence(call, CreateTicketClassDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return ticketCreationBranch(CreateTicketClassBranchDefect, sdkgo.FailureLocalDefect, "ticket class dispatch fence is unavailable")
	}
	decision := fence.Decide()
	switch decision.Kind {
	case singledispatch.FailClosed:
		return ticketCreationUncertain(call, sdkgo.FailureAvailability, "an earlier ticket class creation may have succeeded", "")
	case singledispatch.ReadBackRequired:
		return operation.client.readBackCreatedTicketClass(call, input.EventID, decision.Marker.ProviderID)
	case singledispatch.ReadBackThenResendAllowed:
		if decision.Marker.ProviderID != "" {
			return operation.client.readBackCreatedTicketClass(call, input.EventID, decision.Marker.ProviderID)
		}
	}
	canResendUnknown := decision.MaximumPriorSends+1 < call.MaxSends()
	hasPriorSend := decision.MaximumPriorSends > 0
	var responseBody []byte
	var status int
	var delay time.Duration
	var sendErr error
	err = fence.Dispatch(func(requestContext context.Context) error {
		responseBody, status, delay, sendErr = operation.client.sendWithBody(call, requestContext, http.MethodPost, "/events/"+input.EventID+"/ticket_classes/", requestBody)
		return nil
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return ticketCreationRetry(sdkgo.FailureAvailability, "ticket class was not sent before checkpoint", 0)
	}
	if err != nil {
		return ticketCreationUncertain(call, sdkgo.FailureTransport, "ticket class dispatch outcome is unknown", "")
	}
	if errors.Is(sendErr, errCredential) {
		if hasPriorSend {
			return ticketCreationUncertain(call, sdkgo.FailureAuthentication, "earlier ticket class creation may have succeeded; reconnect", "")
		}
		if fence.RecordNotSent() != nil {
			return ticketCreationRetry(sdkgo.FailureAvailability, "ticket class no-send checkpoint unavailable", 0)
		}
		return ticketCreationBranch(CreateTicketClassBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return ticketCreationRetry(sdkgo.FailureAvailability, "ticket class resend checkpoint unavailable", 0)
		}
		kind := sdkgo.FailureRateLimit
		if status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return ticketCreationRetry(kind, "Eventbrite did not accept the ticket class request", delay)
	}
	if status == http.StatusRequestTimeout || status >= 500 || errors.Is(sendErr, errRequest) {
		if canResendUnknown {
			return ticketCreationRetry(sdkgo.FailureAvailability, "ticket class creation outcome unknown within send cap", delay)
		}
		return ticketCreationUncertain(call, sdkgo.FailureAvailability, "ticket class creation outcome unknown after send cap", "")
	}
	if status < 200 || status >= 300 {
		if hasPriorSend {
			return ticketCreationUncertain(call, sdkgo.FailureProviderRejection, "earlier ticket class creation may have succeeded", "")
		}
		return ticketCreationBranch(CreateTicketClassBranchProviderRejected, sdkgo.FailureProviderRejection, "Eventbrite rejected HTTP "+strconv.Itoa(status))
	}
	if sendErr != nil {
		return ticketCreationUncertain(call, sdkgo.FailureProtocol, "ticket class accepted but identity is unavailable", "")
	}
	created, decodeErr := decodeTicketClass(responseBody)
	if decodeErr != nil || created.EventID != input.EventID {
		return ticketCreationUncertain(call, sdkgo.FailureProtocol, "ticket class accepted but identity is unusable", "")
	}
	_ = fence.RecordProviderID(created.ID)
	return sdkgo.NewMutationBranch(CreateTicketClassBranchCreated, created, nil, eventCreationReceipt(call, created.ID))
}

func (client *Client) readBackCreatedTicketClass(call sdkgo.Call, eventID, ticketClassID string) sdkgo.MutationAttempt[TicketClassMetadata] {
	if !numericID.MatchString(ticketClassID) {
		return ticketCreationUncertain(call, sdkgo.FailureProtocol, "earlier create has no usable ticket class ID", "")
	}
	body, status, delay, err := client.get(call, ticketClassPath(eventID, ticketClassID))
	if status == http.StatusTooManyRequests || status >= 500 || err != nil || status == http.StatusNotFound {
		return ticketCreationRetry(sdkgo.FailureAvailability, "created ticket class is not yet readable", delay)
	}
	if status != http.StatusOK {
		return ticketCreationUncertain(call, sdkgo.FailureProviderRejection, "ticket class read-back was rejected", ticketClassID)
	}
	created, decodeErr := decodeTicketClass(body)
	if decodeErr != nil || created.ID != ticketClassID {
		return ticketCreationUncertain(call, sdkgo.FailureProtocol, "ticket class read-back did not match", ticketClassID)
	}
	return sdkgo.NewMutationBranch(CreateTicketClassBranchCreated, created, nil, eventCreationReceipt(call, created.ID))
}

func ticketCreationBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[TicketClassMetadata] {
	failure := queryFailure("createTicketClass", kind, message)
	return sdkgo.NewMutationBranch(branch, TicketClassMetadata{}, &failure, sdkgo.Receipt{})
}
func ticketCreationRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.MutationAttempt[TicketClassMetadata] {
	return sdkgo.NewMutationRetry[TicketClassMetadata](queryFailure("createTicketClass", kind, message), delay)
}
func ticketCreationUncertain(call sdkgo.Call, kind sdkgo.FailureKind, message, ticketClassID string) sdkgo.MutationAttempt[TicketClassMetadata] {
	return sdkgo.NewMutationUncertain(TicketClassMetadata{}, queryFailure("createTicketClass", kind, message), eventCreationReceipt(call, ticketClassID))
}
