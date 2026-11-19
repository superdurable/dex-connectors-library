// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package eventbrite

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// UpdateEventInput replaces one event's name with the supplied plain text.
type UpdateEventInput struct {
	EventID string `json:"eventId"`
	Name    string `json:"name"`
}

// UnpublishEventInput selects a live event to move back to draft.
type UnpublishEventInput struct {
	EventID string `json:"eventId"`
}

// CancelEventInput selects an event to cancel.
type CancelEventInput struct {
	EventID string `json:"eventId"`
}

// DeleteEventInput selects an event to remove.
type DeleteEventInput struct {
	EventID string `json:"eventId"`
}

// DeletedEvent confirms that an event ID is absent.
type DeletedEvent struct {
	EventID string `json:"eventId"`
	Deleted bool   `json:"deleted"`
}

// UpdateEventOperation replaces an event name.
type UpdateEventOperation struct{ client *Client }

// UnpublishEventOperation removes an event from publication.
type UnpublishEventOperation struct{ client *Client }

// CancelEventOperation cancels an event.
type CancelEventOperation struct{ client *Client }

// DeleteEventOperation removes an event.
type DeleteEventOperation struct{ client *Client }

// UpdateEvent binds the update mutation to this client.
func (client *Client) UpdateEvent() UpdateEventOperation { return UpdateEventOperation{client} }

// UnpublishEvent binds the unpublish mutation to this client.
func (client *Client) UnpublishEvent() UnpublishEventOperation {
	return UnpublishEventOperation{client}
}

// CancelEvent binds the cancel mutation to this client.
func (client *Client) CancelEvent() CancelEventOperation { return CancelEventOperation{client} }

// DeleteEvent binds the delete mutation to this client.
func (client *Client) DeleteEvent() DeleteEventOperation { return DeleteEventOperation{client} }

// Definition returns the manifest-generated update contract.
func (UpdateEventOperation) Definition() sdkgo.MutationDefinition { return UpdateEventDefinition }

// Definition returns the manifest-generated unpublish contract.
func (UnpublishEventOperation) Definition() sdkgo.MutationDefinition { return UnpublishEventDefinition }

// Definition returns the manifest-generated cancel contract.
func (CancelEventOperation) Definition() sdkgo.MutationDefinition { return CancelEventDefinition }

// Definition returns the manifest-generated delete contract.
func (DeleteEventOperation) Definition() sdkgo.MutationDefinition { return DeleteEventDefinition }

// IdempotencyKey correlates repeated update attempts under one Dex call.
func (UpdateEventOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateEventInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates repeated unpublish attempts under one Dex call.
func (UnpublishEventOperation) IdempotencyKey(callID sdkgo.CallID, _ UnpublishEventInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates repeated cancel attempts under one Dex call.
func (CancelEventOperation) IdempotencyKey(callID sdkgo.CallID, _ CancelEventInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates repeated delete attempts under one Dex call.
func (DeleteEventOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteEventInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

type eventChangeBranches struct{ success, notFound, rejected, invalid, defect sdkgo.BranchID }

// Invoke updates the event name and confirms it by reading back the event.
func (operation UpdateEventOperation) Invoke(call sdkgo.Call, input UpdateEventInput) sdkgo.MutationAttempt[EventMetadata] {
	branches := eventChangeBranches{UpdateEventBranchUpdated, UpdateEventBranchNotFound, UpdateEventBranchProviderRejected, UpdateEventBranchInvalidResponse, UpdateEventBranchDefect}
	if !numericID.MatchString(input.EventID) || strings.TrimSpace(input.Name) == "" || len(input.Name) > 255 {
		return eventChangeBranch("updateEvent", branches.defect, sdkgo.FailureValidation, "event ID or bounded name is invalid")
	}
	path := "/events/" + input.EventID + "/"
	current, attempt := operation.client.readEventForChange(call, "updateEvent", path, branches)
	if attempt != nil {
		return *attempt
	}
	if current.Name == input.Name {
		return eventChangeSuccess(call, branches.success, current)
	}
	payload := map[string]any{"event": map[string]any{"name": map[string]string{"html": html.EscapeString(input.Name)}}}
	body, status, delay, err := operation.client.requestJSON(call, http.MethodPost, path, payload)
	if attempt := classifyEventChangeResponse("updateEvent", branches, status, delay, err); attempt != nil {
		return *attempt
	}
	updated, err := decodeEvent(body)
	if err != nil || updated.ID != input.EventID {
		return eventChangeRetry("updateEvent", sdkgo.FailureProtocol, "Eventbrite update answer could not be confirmed", 0)
	}
	confirmed, attempt := operation.client.readEventForChange(call, "updateEvent", path, branches)
	if attempt != nil {
		return eventChangeRetry("updateEvent", sdkgo.FailureAvailability, "updated event cannot yet be read", 0)
	}
	if confirmed.Name != input.Name {
		return eventChangeRetry("updateEvent", sdkgo.FailureAvailability, "updated name is not yet visible", 0)
	}
	return eventChangeSuccess(call, branches.success, confirmed)
}

// Invoke moves a live event back to draft and confirms the resulting status.
func (operation UnpublishEventOperation) Invoke(call sdkgo.Call, input UnpublishEventInput) sdkgo.MutationAttempt[EventMetadata] {
	branches := eventChangeBranches{UnpublishEventBranchUnpublished, UnpublishEventBranchNotFound, UnpublishEventBranchProviderRejected, UnpublishEventBranchInvalidResponse, UnpublishEventBranchDefect}
	return operation.client.changeEventState(call, "unpublishEvent", input.EventID, "unpublish", "unpublished", "draft", branches)
}

// Invoke cancels an event and confirms the resulting status.
func (operation CancelEventOperation) Invoke(call sdkgo.Call, input CancelEventInput) sdkgo.MutationAttempt[EventMetadata] {
	branches := eventChangeBranches{CancelEventBranchCanceled, CancelEventBranchNotFound, CancelEventBranchProviderRejected, CancelEventBranchInvalidResponse, CancelEventBranchDefect}
	return operation.client.changeEventState(call, "cancelEvent", input.EventID, "cancel", "canceled", "canceled", branches)
}

func (client *Client) changeEventState(call sdkgo.Call, operation, eventID, action, responseField, targetStatus string, branches eventChangeBranches) sdkgo.MutationAttempt[EventMetadata] {
	if !numericID.MatchString(eventID) {
		return eventChangeBranch(operation, branches.defect, sdkgo.FailureValidation, "event ID is invalid")
	}
	path := "/events/" + eventID + "/"
	current, attempt := client.readEventForChange(call, operation, path, branches)
	if attempt != nil {
		return *attempt
	}
	if current.Status == targetStatus {
		return eventChangeSuccess(call, branches.success, current)
	}
	body, status, delay, err := client.request(call, http.MethodPost, path+action+"/")
	if attempt := classifyEventChangeResponse(operation, branches, status, delay, err); attempt != nil {
		return *attempt
	}
	var answer map[string]bool
	if json.Unmarshal(body, &answer) != nil || !answer[responseField] {
		return eventChangeRetry(operation, sdkgo.FailureProtocol, "Eventbrite action answer could not be confirmed", 0)
	}
	confirmed, attempt := client.readEventForChange(call, operation, path, branches)
	if attempt != nil {
		return eventChangeRetry(operation, sdkgo.FailureAvailability, "changed event cannot yet be read", 0)
	}
	if confirmed.Status != targetStatus {
		return eventChangeRetry(operation, sdkgo.FailureAvailability, "event state change is not yet visible", 0)
	}
	return eventChangeSuccess(call, branches.success, confirmed)
}

func (client *Client) readEventForChange(call sdkgo.Call, operation, path string, branches eventChangeBranches) (EventMetadata, *sdkgo.MutationAttempt[EventMetadata]) {
	body, status, delay, err := client.get(call, path)
	if attempt := classifyEventChangeResponse(operation, branches, status, delay, err); attempt != nil {
		return EventMetadata{}, attempt
	}
	event, err := decodeEvent(body)
	if err != nil {
		result := eventChangeBranch(operation, branches.invalid, sdkgo.FailureProtocol, "Eventbrite returned invalid event JSON")
		return EventMetadata{}, &result
	}
	return event, nil
}

func classifyEventChangeResponse(operation string, branches eventChangeBranches, status int, delay time.Duration, err error) *sdkgo.MutationAttempt[EventMetadata] {
	var result sdkgo.MutationAttempt[EventMetadata]
	switch {
	case errors.Is(err, errCredential):
		result = eventChangeBranch(operation, branches.defect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	case status == http.StatusTooManyRequests:
		result = eventChangeRetry(operation, sdkgo.FailureRateLimit, "Eventbrite rate limited the event request", delay)
	case status == http.StatusRequestTimeout || status >= 500 || errors.Is(err, errRequest):
		result = eventChangeRetry(operation, sdkgo.FailureAvailability, "Eventbrite event outcome is not yet confirmed", delay)
	case status == http.StatusNotFound && branches.notFound != "":
		result = eventChangeBranch(operation, branches.notFound, sdkgo.FailureNotFound, "Eventbrite did not find the event")
	case status != http.StatusOK:
		result = eventChangeBranch(operation, branches.rejected, sdkgo.FailureProviderRejection, "Eventbrite rejected HTTP "+strconv.Itoa(status))
	case errors.Is(err, providerhttp.ErrBodyTooLarge):
		result = eventChangeRetry(operation, sdkgo.FailureResponseTooLarge, "Eventbrite event response exceeded the configured limit", 0)
	case err != nil:
		result = eventChangeRetry(operation, sdkgo.FailureTransport, "Eventbrite event response was interrupted", 0)
	default:
		return nil
	}
	return &result
}

func eventChangeSuccess(call sdkgo.Call, branch sdkgo.BranchID, event EventMetadata) sdkgo.MutationAttempt[EventMetadata] {
	return sdkgo.NewMutationBranch(branch, event, nil, sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "eventbrite", ProviderObjectID: event.ID, ObservedAt: time.Now()})
}

func eventChangeBranch(operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[EventMetadata] {
	failure := queryFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, EventMetadata{}, &failure, sdkgo.Receipt{})
}

func eventChangeRetry(operation string, kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.MutationAttempt[EventMetadata] {
	return sdkgo.NewMutationRetry[EventMetadata](queryFailure(operation, kind, message), delay)
}

// Invoke deletes an event. A missing event is already in the requested state.
func (operation DeleteEventOperation) Invoke(call sdkgo.Call, input DeleteEventInput) sdkgo.MutationAttempt[DeletedEvent] {
	if !numericID.MatchString(input.EventID) {
		return deleteEventBranch(DeleteEventBranchDefect, sdkgo.FailureValidation, "event ID is invalid")
	}
	body, status, delay, err := operation.client.request(call, http.MethodDelete, "/events/"+input.EventID+"/")
	switch {
	case errors.Is(err, errCredential):
		return deleteEventBranch(DeleteEventBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	case status == http.StatusTooManyRequests:
		return deleteEventRetry(sdkgo.FailureRateLimit, "Eventbrite rate limited deletion", delay)
	case status == http.StatusRequestTimeout || status >= 500 || errors.Is(err, errRequest):
		return deleteEventRetry(sdkgo.FailureAvailability, "Eventbrite deletion outcome is not yet confirmed", delay)
	case status == http.StatusNotFound, status == http.StatusNoContent:
		return deleteEventSuccess(call, input.EventID)
	case status != http.StatusOK:
		return deleteEventBranch(DeleteEventBranchProviderRejected, sdkgo.FailureProviderRejection, fmt.Sprintf("Eventbrite rejected deletion with HTTP %d", status))
	case errors.Is(err, providerhttp.ErrBodyTooLarge):
		return deleteEventRetry(sdkgo.FailureResponseTooLarge, "Eventbrite deletion answer exceeded the configured limit", 0)
	case err != nil:
		return deleteEventRetry(sdkgo.FailureTransport, "Eventbrite deletion answer was interrupted", 0)
	}
	var answer struct {
		Deleted *bool `json:"deleted"`
	}
	if json.Unmarshal(body, &answer) != nil || answer.Deleted == nil {
		return deleteEventRetry(sdkgo.FailureProtocol, "Eventbrite deletion answer could not be confirmed", 0)
	}
	if !*answer.Deleted {
		return deleteEventBranch(DeleteEventBranchProviderRejected, sdkgo.FailureProviderRejection, "Eventbrite did not delete the event")
	}
	return deleteEventSuccess(call, input.EventID)
}

func deleteEventSuccess(call sdkgo.Call, eventID string) sdkgo.MutationAttempt[DeletedEvent] {
	return sdkgo.NewMutationBranch(DeleteEventBranchDeleted, DeletedEvent{EventID: eventID, Deleted: true}, nil,
		sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "eventbrite", ProviderObjectID: eventID, ObservedAt: time.Now()})
}

func deleteEventBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[DeletedEvent] {
	failure := queryFailure("deleteEvent", kind, message)
	return sdkgo.NewMutationBranch(branch, DeletedEvent{}, &failure, sdkgo.Receipt{})
}

func deleteEventRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.MutationAttempt[DeletedEvent] {
	return sdkgo.NewMutationRetry[DeletedEvent](queryFailure("deleteEvent", kind, message), delay)
}
