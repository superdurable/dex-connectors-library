// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package resend

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// CreateEventInput declares a reusable automation event type.
type CreateEventInput struct {
	// Name is the event name and cannot use Resend's reserved prefix.
	Name string `json:"name"`
	// Schema maps payload keys to string, number, boolean, or date.
	Schema map[string]string `json:"schema,omitempty"`
}

// GetEventInput selects an event type by ID or name.
type GetEventInput struct {
	// Identifier is the event ID or name.
	Identifier string `json:"identifier"`
}

// ListEventsInput selects one event type page.
type ListEventsInput struct {
	// Limit is the page size from 1 through 100; zero uses Resend's default.
	Limit int `json:"limit,omitempty"`
	// After is the exclusive event cursor.
	After string `json:"after,omitempty"`
}

// UpdateEventInput replaces an event type's payload schema.
type UpdateEventInput struct {
	// Identifier is the event ID or name.
	Identifier string `json:"identifier"`
	// Schema replaces the flat field map when non-nil.
	Schema *map[string]string `json:"schema"`
	// ClearSchema removes the provider schema instead of replacing it.
	ClearSchema bool `json:"clearSchema,omitempty"`
}

// DeleteEventInput selects an event type for removal.
type DeleteEventInput struct {
	// Identifier is the event ID or name.
	Identifier string `json:"identifier"`
}

// SendEventInput delivers one automation event for a contact.
type SendEventInput struct {
	// Event is the registered event name.
	Event string `json:"event"`
	// ContactID identifies the contact; use exactly one of ContactID or Email.
	ContactID string `json:"contactId,omitempty"`
	// Email identifies the contact; use exactly one of Email or ContactID.
	Email string `json:"email,omitempty"`
	// Payload supplies declared event values; do not include secrets.
	Payload map[string]any `json:"payload,omitempty"`
}

// EventReference identifies a created or changed event type.
type EventReference struct {
	// ID is the provider event ID.
	ID string `json:"id"`
}

// Event contains an automation event type and its schema.
type Event struct {
	// ID is the event ID.
	ID string `json:"id"`
	// Name is the event name.
	Name string `json:"name"`
	// Schema maps payload keys to supported types.
	Schema map[string]string `json:"schema"`
	// CreatedAt is the creation timestamp.
	CreatedAt string `json:"created_at"`
	// UpdatedAt is the optional update timestamp.
	UpdatedAt string `json:"updated_at"`
}

// EventPage contains one event type page.
type EventPage struct {
	// Data contains event type summaries.
	Data []Event `json:"data"`
	// HasMore reports whether another page exists.
	HasMore bool `json:"has_more"`
}

// DeletedEvent confirms one event type removal.
type DeletedEvent struct {
	// ID is the removed event ID.
	ID string `json:"id"`
	// Deleted confirms removal.
	Deleted bool `json:"deleted"`
}

// EventSendReference confirms an accepted event delivery.
type EventSendReference struct {
	// Event is the delivered event name.
	Event string `json:"event"`
}

// CreateEventOperation creates an event type with bounded resend.
type CreateEventOperation struct{ client *Client }

// GetEventOperation retrieves an event type.
type GetEventOperation struct{ client *Client }

// ListEventsOperation lists event types.
type ListEventsOperation struct{ client *Client }

// UpdateEventOperation replaces an event schema.
type UpdateEventOperation struct{ client *Client }

// DeleteEventOperation removes an event type.
type DeleteEventOperation struct{ client *Client }

// SendEventOperation delivers an event with bounded resend.
type SendEventOperation struct{ client *Client }

// CreateEvent binds event type creation to this client.
func (client *Client) CreateEvent() CreateEventOperation { return CreateEventOperation{client} }

// GetEvent binds event type retrieval to this client.
func (client *Client) GetEvent() GetEventOperation { return GetEventOperation{client} }

// ListEvents binds event type listing to this client.
func (client *Client) ListEvents() ListEventsOperation { return ListEventsOperation{client} }

// UpdateEvent binds event schema updates to this client.
func (client *Client) UpdateEvent() UpdateEventOperation { return UpdateEventOperation{client} }

// DeleteEvent binds event type removal to this client.
func (client *Client) DeleteEvent() DeleteEventOperation { return DeleteEventOperation{client} }

// SendEvent binds event delivery to this client.
func (client *Client) SendEvent() SendEventOperation { return SendEventOperation{client} }

// Definition returns the generated createEvent contract.
func (CreateEventOperation) Definition() sdkgo.MutationDefinition { return CreateEventDefinition }

// Definition returns the generated getEvent contract.
func (GetEventOperation) Definition() sdkgo.QueryDefinition { return GetEventDefinition }

// Definition returns the generated listEvents contract.
func (ListEventsOperation) Definition() sdkgo.QueryDefinition { return ListEventsDefinition }

// Definition returns the generated updateEvent contract.
func (UpdateEventOperation) Definition() sdkgo.MutationDefinition { return UpdateEventDefinition }

// Definition returns the generated deleteEvent contract.
func (DeleteEventOperation) Definition() sdkgo.MutationDefinition { return DeleteEventDefinition }

// Definition returns the generated sendEvent contract.
func (SendEventOperation) Definition() sdkgo.MutationDefinition { return SendEventDefinition }

// IdempotencyKey correlates possible event type creation attempts.
func (CreateEventOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateEventInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates absolute schema updates.
func (UpdateEventOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateEventInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates harmless repeated deletion.
func (DeleteEventOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteEventInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates bounded delivery attempts.
func (SendEventOperation) IdempotencyKey(callID sdkgo.CallID, _ SendEventInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates an automation event type.
func (operation CreateEventOperation) Invoke(call sdkgo.Call, input CreateEventInput) sdkgo.MutationAttempt[EventReference] {
	if strings.TrimSpace(input.Name) == "" || strings.HasPrefix(input.Name, "resend:") || !validEventSchema(input.Schema) {
		return mutationFailure[EventReference]("createEvent", CreateEventBranchDefect, sdkgo.FailureValidation, "event name or schema is invalid")
	}
	fields := map[string]any{"name": input.Name}
	if input.Schema != nil {
		fields["schema"] = input.Schema
	}
	body, _ := json.Marshal(fields)
	request := resendLimitedCreate[EventReference]{client: operation.client, call: call, operation: "createEvent", path: "/events", body: body, timeout: CreateEventDefinition.StepDefaults.ExecuteMethodTimeout, created: CreateEventBranchCreated, rejected: CreateEventBranchProviderRejected, defect: CreateEventBranchDefect, decode: decodeEventReference, readBack: func(call sdkgo.Call) (EventReference, readBackState) { return operation.readBackEvent(call, input) }}
	return request.invoke()
}

// Invoke retrieves one event type.
func (operation GetEventOperation) Invoke(call sdkgo.Call, input GetEventInput) sdkgo.QueryAttempt[Event] {
	if strings.TrimSpace(input.Identifier) == "" {
		return queryFailure[Event]("getEvent", GetEventBranchDefect, sdkgo.FailureValidation, "event identifier is required")
	}
	return runQuery(operation.client, call, "getEvent", "/events/"+url.PathEscape(input.Identifier), GetEventBranchFound, GetEventBranchNotFound, GetEventBranchProviderRejected, GetEventBranchInvalidResponse, GetEventBranchDefect, decodeEvent)
}

// Invoke lists one page of event types.
func (operation ListEventsOperation) Invoke(call sdkgo.Call, input ListEventsInput) sdkgo.QueryAttempt[EventPage] {
	query, err := listQuery(input.Limit, input.After, false)
	if err != nil {
		return queryFailure[EventPage]("listEvents", ListEventsBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	return runQuery(operation.client, call, "listEvents", "/events"+query, ListEventsBranchListed, "", ListEventsBranchProviderRejected, ListEventsBranchInvalidResponse, ListEventsBranchDefect, decodeEventPage)
}

// Invoke replaces an event type's flat payload schema.
func (operation UpdateEventOperation) Invoke(call sdkgo.Call, input UpdateEventInput) sdkgo.MutationAttempt[EventReference] {
	if strings.TrimSpace(input.Identifier) == "" || (input.Schema == nil && !input.ClearSchema) || (input.Schema != nil && input.ClearSchema) || (input.Schema != nil && !validEventSchema(*input.Schema)) {
		return mutationFailure[EventReference]("updateEvent", UpdateEventBranchDefect, sdkgo.FailureValidation, "event identifier and exactly one schema action are required")
	}
	fields := map[string]any{"schema": nil}
	if input.Schema != nil {
		fields["schema"] = *input.Schema
	}
	body, _ := json.Marshal(fields)
	return runMutation(operation.client, call, "updateEvent", http.MethodPatch, "/events/"+url.PathEscape(input.Identifier), body, "", "", UpdateEventBranchUpdated, UpdateEventBranchNotFound, UpdateEventBranchProviderRejected, UpdateEventBranchInvalidResponse, UpdateEventBranchDefect, decodeEventReference)
}

// Invoke removes one event type.
func (operation DeleteEventOperation) Invoke(call sdkgo.Call, input DeleteEventInput) sdkgo.MutationAttempt[DeletedEvent] {
	if strings.TrimSpace(input.Identifier) == "" {
		return mutationFailure[DeletedEvent]("deleteEvent", DeleteEventBranchDefect, sdkgo.FailureValidation, "event identifier is required")
	}
	return runMutation(operation.client, call, "deleteEvent", http.MethodDelete, "/events/"+url.PathEscape(input.Identifier), nil, "", "", DeleteEventBranchDeleted, DeleteEventBranchNotFound, DeleteEventBranchProviderRejected, DeleteEventBranchInvalidResponse, DeleteEventBranchDefect, decodeDeletedEvent)
}

// Invoke delivers one automation event for a contact.
func (operation SendEventOperation) Invoke(call sdkgo.Call, input SendEventInput) sdkgo.MutationAttempt[EventSendReference] {
	if strings.TrimSpace(input.Event) == "" || (input.ContactID == "") == (input.Email == "") {
		return mutationFailure[EventSendReference]("sendEvent", SendEventBranchDefect, sdkgo.FailureValidation, "event and exactly one contact ID or email are required")
	}
	fields := map[string]any{"event": input.Event}
	if input.ContactID != "" {
		fields["contact_id"] = input.ContactID
	} else {
		fields["email"] = input.Email
	}
	if input.Payload != nil {
		fields["payload"] = input.Payload
	}
	body, err := json.Marshal(fields)
	if err != nil {
		return mutationFailure[EventSendReference]("sendEvent", SendEventBranchDefect, sdkgo.FailureValidation, "event payload cannot be encoded")
	}
	request := resendLimitedCreate[EventSendReference]{client: operation.client, call: call, operation: "sendEvent", path: "/events/send", body: body, timeout: SendEventDefinition.StepDefaults.ExecuteMethodTimeout, created: SendEventBranchSent, rejected: SendEventBranchProviderRejected, defect: SendEventBranchDefect, decode: decodeEventSendReference, readBack: func(sdkgo.Call) (EventSendReference, readBackState) { return EventSendReference{}, readBackAbsent }}
	return request.invoke()
}

func (operation CreateEventOperation) readBackEvent(call sdkgo.Call, input CreateEventInput) (EventReference, readBackState) {
	cursor := ""
	var confirmed EventReference
	for pageNumber := 0; pageNumber < 100; pageNumber++ {
		query, _ := listQuery(100, cursor, false)
		body, state := operation.client.readBackResource(call, "/events"+query)
		if state != readBackMatched {
			return EventReference{}, readBackRetry
		}
		page, err := decodeEventPage(body)
		if err != nil {
			return EventReference{}, readBackRetry
		}
		for _, candidate := range page.Data {
			if candidate.Name == input.Name && reflect.DeepEqual(candidate.Schema, input.Schema) {
				if confirmed.ID != "" {
					return EventReference{}, readBackConflicted
				}
				confirmed = EventReference{ID: candidate.ID}
			}
		}
		if !page.HasMore {
			if confirmed.ID != "" {
				return confirmed, readBackMatched
			}
			return EventReference{}, readBackAbsent
		}
		if len(page.Data) == 0 || page.Data[len(page.Data)-1].ID == cursor {
			return EventReference{}, readBackRetry
		}
		cursor = page.Data[len(page.Data)-1].ID
	}
	return EventReference{}, readBackRetry
}

func validEventSchema(schema map[string]string) bool {
	for key, kind := range schema {
		if strings.TrimSpace(key) == "" || (kind != "string" && kind != "number" && kind != "boolean" && kind != "date") {
			return false
		}
	}
	return true
}
func decodeEventReference(body []byte) (EventReference, error) {
	var value EventReference
	if err := json.Unmarshal(body, &value); err != nil || value.ID == "" {
		return EventReference{}, fmt.Errorf("invalid event reference")
	}
	return value, nil
}
func decodeEvent(body []byte) (Event, error) {
	var value Event
	if err := json.Unmarshal(body, &value); err != nil || value.ID == "" {
		return Event{}, fmt.Errorf("invalid event response")
	}
	return value, nil
}
func decodeEventPage(body []byte) (EventPage, error) {
	var value EventPage
	if err := json.Unmarshal(body, &value); err != nil || value.Data == nil {
		return EventPage{}, fmt.Errorf("invalid event page")
	}
	return value, nil
}
func decodeDeletedEvent(body []byte) (DeletedEvent, error) {
	var value DeletedEvent
	if err := json.Unmarshal(body, &value); err != nil || value.ID == "" || !value.Deleted {
		return DeletedEvent{}, fmt.Errorf("invalid event removal")
	}
	return value, nil
}
func decodeEventSendReference(body []byte) (EventSendReference, error) {
	var value EventSendReference
	if err := json.Unmarshal(body, &value); err != nil || value.Event == "" {
		return EventSendReference{}, fmt.Errorf("invalid event delivery response")
	}
	return value, nil
}
