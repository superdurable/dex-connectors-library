// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package resend

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// CreateWebhookInput subscribes an HTTPS endpoint to provider events.
type CreateWebhookInput struct {
	// Endpoint is the publicly reachable HTTPS receiver URL.
	Endpoint string `json:"endpoint"`
	// Events contains the event types to deliver.
	Events []string `json:"events"`
}

// GetWebhookInput selects one webhook.
type GetWebhookInput struct {
	// ID is the webhook ID.
	ID string `json:"id"`
}

// ListWebhooksInput selects a webhook page.
type ListWebhooksInput struct {
	// Limit is the page size from 1 through 100; zero uses Resend's default.
	Limit int `json:"limit,omitempty"`
	// After is the exclusive webhook cursor.
	After string `json:"after,omitempty"`
}

// UpdateWebhookInput replaces selected absolute webhook fields.
type UpdateWebhookInput struct {
	// ID is the webhook ID.
	ID string `json:"id"`
	// Endpoint replaces the receiver URL when non-nil.
	Endpoint *string `json:"endpoint,omitempty"`
	// Events replaces subscriptions when non-nil.
	Events *[]string `json:"events,omitempty"`
	// Status is enabled or disabled when non-nil.
	Status *string `json:"status,omitempty"`
}

// DeleteWebhookInput selects a webhook for removal.
type DeleteWebhookInput struct {
	// ID is the webhook ID.
	ID string `json:"id"`
}

// ListWebhookEventsInput selects one delivery event page.
type ListWebhookEventsInput struct {
	// WebhookID is the subscription ID.
	WebhookID string `json:"webhookId"`
	// Limit is the page size from 1 through 100; zero uses Resend's default.
	Limit int `json:"limit,omitempty"`
	// After is the exclusive event cursor.
	After string `json:"after,omitempty"`
}

// GetWebhookEventInput selects one delivery event.
type GetWebhookEventInput struct {
	// WebhookID is the subscription ID.
	WebhookID string `json:"webhookId"`
	// EventID is the delivery event ID.
	EventID string `json:"eventId"`
}

// ListWebhookEventAttemptsInput selects one delivery attempt page.
type ListWebhookEventAttemptsInput struct {
	// WebhookID is the subscription ID.
	WebhookID string `json:"webhookId"`
	// EventID is the delivery event ID.
	EventID string `json:"eventId"`
	// Limit is the page size from 1 through 100; zero uses Resend's default.
	Limit int `json:"limit,omitempty"`
	// After is the exclusive attempt cursor.
	After string `json:"after,omitempty"`
}

// ReplayWebhookEventInput requests another delivery of an existing event.
type ReplayWebhookEventInput struct {
	// WebhookID is the enabled subscription ID.
	WebhookID string `json:"webhookId"`
	// EventID is the event to replay.
	EventID string `json:"eventId"`
}

// WebhookReference identifies a created or changed webhook.
type WebhookReference struct {
	// ID is the provider webhook ID.
	ID string `json:"id"`
}

// Webhook contains subscription metadata and excludes its signing secret.
type Webhook struct {
	// ID is the webhook ID.
	ID string `json:"id"`
	// Endpoint is the HTTPS receiver URL.
	Endpoint string `json:"endpoint"`
	// Events contains subscribed provider event types.
	Events []string `json:"events"`
	// Status is enabled or disabled.
	Status string `json:"status"`
	// CreatedAt is the creation timestamp.
	CreatedAt string `json:"created_at"`
}

// WebhookPage contains one webhook page.
type WebhookPage struct {
	// Data contains webhook metadata.
	Data []Webhook `json:"data"`
	// HasMore reports whether another page exists.
	HasMore bool `json:"has_more"`
}

// DeletedWebhook confirms one removal.
type DeletedWebhook struct {
	// ID is the removed webhook ID.
	ID string `json:"id"`
	// Deleted confirms removal.
	Deleted bool `json:"deleted"`
}

// WebhookEvent contains delivery metadata without the payload.
type WebhookEvent struct {
	// ID is the event ID.
	ID string `json:"id"`
	// Type is the provider event type.
	Type string `json:"type"`
	// CreatedAt is the event timestamp.
	CreatedAt string `json:"created_at"`
	// Status is pending, attempting, success, or failed.
	Status string `json:"status"`
	// NextAttemptAt is the next scheduled attempt timestamp, when present.
	NextAttemptAt string `json:"next_attempt_at"`
}

// WebhookEventPage contains one delivery event page.
type WebhookEventPage struct {
	// Data contains delivery event metadata.
	Data []WebhookEvent `json:"data"`
	// HasMore reports whether another page exists.
	HasMore bool `json:"has_more"`
}

// WebhookEventAttempt contains delivery status without receiver response data.
type WebhookEventAttempt struct {
	// ID is the attempt ID.
	ID string `json:"id"`
	// HTTPStatusCode is the receiver response status.
	HTTPStatusCode int `json:"http_status_code"`
	// SentAt is the delivery timestamp.
	SentAt string `json:"sent_at"`
}

// WebhookEventAttemptPage contains one delivery attempt page.
type WebhookEventAttemptPage struct {
	// Data contains delivery status metadata.
	Data []WebhookEventAttempt `json:"data"`
	// HasMore reports whether another page exists.
	HasMore bool `json:"has_more"`
}

// CreateWebhookOperation creates a subscription with bounded resend.
type CreateWebhookOperation struct{ client *Client }

// GetWebhookOperation retrieves subscription metadata.
type GetWebhookOperation struct{ client *Client }

// ListWebhooksOperation lists subscription metadata.
type ListWebhooksOperation struct{ client *Client }

// UpdateWebhookOperation replaces selected subscription fields.
type UpdateWebhookOperation struct{ client *Client }

// DeleteWebhookOperation removes a subscription.
type DeleteWebhookOperation struct{ client *Client }

// ListWebhookEventsOperation lists event delivery metadata.
type ListWebhookEventsOperation struct{ client *Client }

// GetWebhookEventOperation retrieves event delivery metadata.
type GetWebhookEventOperation struct{ client *Client }

// ListWebhookEventAttemptsOperation lists event delivery attempts.
type ListWebhookEventAttemptsOperation struct{ client *Client }

// ReplayWebhookEventOperation queues a bounded replay request.
type ReplayWebhookEventOperation struct{ client *Client }

// CreateWebhook binds subscription creation to this client.
func (client *Client) CreateWebhook() CreateWebhookOperation { return CreateWebhookOperation{client} }

// GetWebhook binds subscription retrieval to this client.
func (client *Client) GetWebhook() GetWebhookOperation { return GetWebhookOperation{client} }

// ListWebhooks binds subscription listing to this client.
func (client *Client) ListWebhooks() ListWebhooksOperation { return ListWebhooksOperation{client} }

// UpdateWebhook binds subscription updates to this client.
func (client *Client) UpdateWebhook() UpdateWebhookOperation { return UpdateWebhookOperation{client} }

// DeleteWebhook binds subscription removal to this client.
func (client *Client) DeleteWebhook() DeleteWebhookOperation { return DeleteWebhookOperation{client} }

// ListWebhookEvents binds delivery event listing to this client.
func (client *Client) ListWebhookEvents() ListWebhookEventsOperation {
	return ListWebhookEventsOperation{client}
}

// GetWebhookEvent binds delivery event retrieval to this client.
func (client *Client) GetWebhookEvent() GetWebhookEventOperation {
	return GetWebhookEventOperation{client}
}

// ListWebhookEventAttempts binds delivery attempt listing to this client.
func (client *Client) ListWebhookEventAttempts() ListWebhookEventAttemptsOperation {
	return ListWebhookEventAttemptsOperation{client}
}

// ReplayWebhookEvent binds event replay to this client.
func (client *Client) ReplayWebhookEvent() ReplayWebhookEventOperation {
	return ReplayWebhookEventOperation{client}
}

// Definition returns the generated createWebhook contract.
func (CreateWebhookOperation) Definition() sdkgo.MutationDefinition { return CreateWebhookDefinition }

// Definition returns the generated getWebhook contract.
func (GetWebhookOperation) Definition() sdkgo.QueryDefinition { return GetWebhookDefinition }

// Definition returns the generated listWebhooks contract.
func (ListWebhooksOperation) Definition() sdkgo.QueryDefinition { return ListWebhooksDefinition }

// Definition returns the generated updateWebhook contract.
func (UpdateWebhookOperation) Definition() sdkgo.MutationDefinition { return UpdateWebhookDefinition }

// Definition returns the generated deleteWebhook contract.
func (DeleteWebhookOperation) Definition() sdkgo.MutationDefinition { return DeleteWebhookDefinition }

// Definition returns the generated listWebhookEvents contract.
func (ListWebhookEventsOperation) Definition() sdkgo.QueryDefinition {
	return ListWebhookEventsDefinition
}

// Definition returns the generated getWebhookEvent contract.
func (GetWebhookEventOperation) Definition() sdkgo.QueryDefinition { return GetWebhookEventDefinition }

// Definition returns the generated listWebhookEventAttempts contract.
func (ListWebhookEventAttemptsOperation) Definition() sdkgo.QueryDefinition {
	return ListWebhookEventAttemptsDefinition
}

// Definition returns the generated replayWebhookEvent contract.
func (ReplayWebhookEventOperation) Definition() sdkgo.MutationDefinition {
	return ReplayWebhookEventDefinition
}

// IdempotencyKey correlates possible subscription creation attempts.
func (CreateWebhookOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateWebhookInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates absolute subscription changes.
func (UpdateWebhookOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateWebhookInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates harmless repeated deletion.
func (DeleteWebhookOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteWebhookInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates bounded replay requests.
func (ReplayWebhookEventOperation) IdempotencyKey(callID sdkgo.CallID, _ ReplayWebhookEventInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates one webhook and discards its provider signing secret.
func (operation CreateWebhookOperation) Invoke(call sdkgo.Call, input CreateWebhookInput) sdkgo.MutationAttempt[WebhookReference] {
	if !strings.HasPrefix(input.Endpoint, "https://") || len(input.Events) == 0 {
		return mutationFailure[WebhookReference]("createWebhook", CreateWebhookBranchDefect, sdkgo.FailureValidation, "HTTPS endpoint and at least one event are required")
	}
	for _, event := range input.Events {
		if strings.TrimSpace(event) == "" {
			return mutationFailure[WebhookReference]("createWebhook", CreateWebhookBranchDefect, sdkgo.FailureValidation, "event type cannot be blank")
		}
	}
	body, _ := json.Marshal(map[string]any{"endpoint": input.Endpoint, "events": input.Events})
	request := resendLimitedCreate[WebhookReference]{client: operation.client, call: call, operation: "createWebhook", path: "/webhooks", body: body, timeout: CreateWebhookDefinition.StepDefaults.ExecuteMethodTimeout, created: CreateWebhookBranchCreated, rejected: CreateWebhookBranchProviderRejected, defect: CreateWebhookBranchDefect, decode: decodeWebhookReference, readBack: func(call sdkgo.Call) (WebhookReference, readBackState) { return operation.readBackWebhook(call, input) }}
	return request.invoke()
}

// Invoke retrieves safe subscription metadata.
func (operation GetWebhookOperation) Invoke(call sdkgo.Call, input GetWebhookInput) sdkgo.QueryAttempt[Webhook] {
	if strings.TrimSpace(input.ID) == "" {
		return queryFailure[Webhook]("getWebhook", GetWebhookBranchDefect, sdkgo.FailureValidation, "webhook ID is required")
	}
	return runQuery(operation.client, call, "getWebhook", "/webhooks/"+url.PathEscape(input.ID), GetWebhookBranchFound, GetWebhookBranchNotFound, GetWebhookBranchProviderRejected, GetWebhookBranchInvalidResponse, GetWebhookBranchDefect, decodeWebhook)
}

// Invoke lists safe subscription metadata.
func (operation ListWebhooksOperation) Invoke(call sdkgo.Call, input ListWebhooksInput) sdkgo.QueryAttempt[WebhookPage] {
	query, err := listQuery(input.Limit, input.After, false)
	if err != nil {
		return queryFailure[WebhookPage]("listWebhooks", ListWebhooksBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	return runQuery(operation.client, call, "listWebhooks", "/webhooks"+query, ListWebhooksBranchListed, "", ListWebhooksBranchProviderRejected, ListWebhooksBranchInvalidResponse, ListWebhooksBranchDefect, decodeWebhookPage)
}

// Invoke replaces selected absolute subscription fields.
func (operation UpdateWebhookOperation) Invoke(call sdkgo.Call, input UpdateWebhookInput) sdkgo.MutationAttempt[WebhookReference] {
	if strings.TrimSpace(input.ID) == "" || (input.Endpoint == nil && input.Events == nil && input.Status == nil) {
		return mutationFailure[WebhookReference]("updateWebhook", UpdateWebhookBranchDefect, sdkgo.FailureValidation, "webhook ID and at least one field are required")
	}
	fields := map[string]any{}
	if input.Endpoint != nil {
		if !strings.HasPrefix(*input.Endpoint, "https://") {
			return mutationFailure[WebhookReference]("updateWebhook", UpdateWebhookBranchDefect, sdkgo.FailureValidation, "endpoint must use HTTPS")
		}
		fields["endpoint"] = *input.Endpoint
	}
	if input.Events != nil {
		if len(*input.Events) == 0 {
			return mutationFailure[WebhookReference]("updateWebhook", UpdateWebhookBranchDefect, sdkgo.FailureValidation, "at least one event is required")
		}
		fields["events"] = *input.Events
	}
	if input.Status != nil {
		if *input.Status != "enabled" && *input.Status != "disabled" {
			return mutationFailure[WebhookReference]("updateWebhook", UpdateWebhookBranchDefect, sdkgo.FailureValidation, "status must be enabled or disabled")
		}
		fields["status"] = *input.Status
	}
	body, _ := json.Marshal(fields)
	return runMutation(operation.client, call, "updateWebhook", http.MethodPatch, "/webhooks/"+url.PathEscape(input.ID), body, "", "", UpdateWebhookBranchUpdated, UpdateWebhookBranchNotFound, UpdateWebhookBranchProviderRejected, UpdateWebhookBranchInvalidResponse, UpdateWebhookBranchDefect, decodeWebhookReference)
}

// Invoke removes a webhook subscription.
func (operation DeleteWebhookOperation) Invoke(call sdkgo.Call, input DeleteWebhookInput) sdkgo.MutationAttempt[DeletedWebhook] {
	if strings.TrimSpace(input.ID) == "" {
		return mutationFailure[DeletedWebhook]("deleteWebhook", DeleteWebhookBranchDefect, sdkgo.FailureValidation, "webhook ID is required")
	}
	return runMutation(operation.client, call, "deleteWebhook", http.MethodDelete, "/webhooks/"+url.PathEscape(input.ID), nil, "", "", DeleteWebhookBranchDeleted, DeleteWebhookBranchNotFound, DeleteWebhookBranchProviderRejected, DeleteWebhookBranchInvalidResponse, DeleteWebhookBranchDefect, decodeDeletedWebhook)
}

// Invoke lists safe event delivery metadata.
func (operation ListWebhookEventsOperation) Invoke(call sdkgo.Call, input ListWebhookEventsInput) sdkgo.QueryAttempt[WebhookEventPage] {
	if strings.TrimSpace(input.WebhookID) == "" {
		return queryFailure[WebhookEventPage]("listWebhookEvents", ListWebhookEventsBranchDefect, sdkgo.FailureValidation, "webhook ID is required")
	}
	query, err := listQuery(input.Limit, input.After, false)
	if err != nil {
		return queryFailure[WebhookEventPage]("listWebhookEvents", ListWebhookEventsBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	return runQuery(operation.client, call, "listWebhookEvents", "/webhooks/"+url.PathEscape(input.WebhookID)+"/events"+query, ListWebhookEventsBranchListed, "", ListWebhookEventsBranchProviderRejected, ListWebhookEventsBranchInvalidResponse, ListWebhookEventsBranchDefect, decodeWebhookEventPage)
}

// Invoke retrieves safe event delivery metadata.
func (operation GetWebhookEventOperation) Invoke(call sdkgo.Call, input GetWebhookEventInput) sdkgo.QueryAttempt[WebhookEvent] {
	path, ok := webhookEventPath(input.WebhookID, input.EventID)
	if !ok {
		return queryFailure[WebhookEvent]("getWebhookEvent", GetWebhookEventBranchDefect, sdkgo.FailureValidation, "webhook and event IDs are required")
	}
	return runQuery(operation.client, call, "getWebhookEvent", path, GetWebhookEventBranchFound, GetWebhookEventBranchNotFound, GetWebhookEventBranchProviderRejected, GetWebhookEventBranchInvalidResponse, GetWebhookEventBranchDefect, decodeWebhookEvent)
}

// Invoke lists delivery attempt status without receiver response bodies.
func (operation ListWebhookEventAttemptsOperation) Invoke(call sdkgo.Call, input ListWebhookEventAttemptsInput) sdkgo.QueryAttempt[WebhookEventAttemptPage] {
	path, ok := webhookEventPath(input.WebhookID, input.EventID)
	if !ok {
		return queryFailure[WebhookEventAttemptPage]("listWebhookEventAttempts", ListWebhookEventAttemptsBranchDefect, sdkgo.FailureValidation, "webhook and event IDs are required")
	}
	query, err := listQuery(input.Limit, input.After, false)
	if err != nil {
		return queryFailure[WebhookEventAttemptPage]("listWebhookEventAttempts", ListWebhookEventAttemptsBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	return runQuery(operation.client, call, "listWebhookEventAttempts", path+"/attempts"+query, ListWebhookEventAttemptsBranchListed, "", ListWebhookEventAttemptsBranchProviderRejected, ListWebhookEventAttemptsBranchInvalidResponse, ListWebhookEventAttemptsBranchDefect, decodeWebhookEventAttemptPage)
}

// Invoke queues another delivery with a bounded duplicate count.
func (operation ReplayWebhookEventOperation) Invoke(call sdkgo.Call, input ReplayWebhookEventInput) sdkgo.MutationAttempt[WebhookEvent] {
	path, ok := webhookEventPath(input.WebhookID, input.EventID)
	if !ok {
		return mutationFailure[WebhookEvent]("replayWebhookEvent", ReplayWebhookEventBranchDefect, sdkgo.FailureValidation, "webhook and event IDs are required")
	}
	request := resendLimitedCreate[WebhookEvent]{client: operation.client, call: call, operation: "replayWebhookEvent", path: path + "/replay", timeout: ReplayWebhookEventDefinition.StepDefaults.ExecuteMethodTimeout, created: ReplayWebhookEventBranchReplayed, rejected: ReplayWebhookEventBranchProviderRejected, defect: ReplayWebhookEventBranchDefect, decode: decodeWebhookEvent, readBack: func(sdkgo.Call) (WebhookEvent, readBackState) { return WebhookEvent{}, readBackAbsent }}
	return request.invoke()
}

func (operation CreateWebhookOperation) readBackWebhook(call sdkgo.Call, input CreateWebhookInput) (WebhookReference, readBackState) {
	cursor := ""
	var confirmed WebhookReference
	for pageNumber := 0; pageNumber < 100; pageNumber++ {
		query, _ := listQuery(100, cursor, false)
		body, state := operation.client.readBackResource(call, "/webhooks"+query)
		if state != readBackMatched {
			return WebhookReference{}, readBackRetry
		}
		page, err := decodeWebhookPage(body)
		if err != nil {
			return WebhookReference{}, readBackRetry
		}
		for _, candidate := range page.Data {
			if candidate.Endpoint == input.Endpoint && slices.Equal(candidate.Events, input.Events) {
				if confirmed.ID != "" {
					return WebhookReference{}, readBackConflicted
				}
				confirmed = WebhookReference{ID: candidate.ID}
			}
		}
		if !page.HasMore {
			if confirmed.ID != "" {
				return confirmed, readBackMatched
			}
			return WebhookReference{}, readBackAbsent
		}
		if len(page.Data) == 0 || page.Data[len(page.Data)-1].ID == cursor {
			return WebhookReference{}, readBackRetry
		}
		cursor = page.Data[len(page.Data)-1].ID
	}
	return WebhookReference{}, readBackRetry
}

func webhookEventPath(webhookID, eventID string) (string, bool) {
	if strings.TrimSpace(webhookID) == "" || strings.TrimSpace(eventID) == "" {
		return "", false
	}
	return "/webhooks/" + url.PathEscape(webhookID) + "/events/" + url.PathEscape(eventID), true
}
func decodeWebhookReference(body []byte) (WebhookReference, error) {
	var value WebhookReference
	if err := json.Unmarshal(body, &value); err != nil || value.ID == "" {
		return WebhookReference{}, fmt.Errorf("invalid webhook reference")
	}
	return value, nil
}
func decodeWebhook(body []byte) (Webhook, error) {
	var value Webhook
	if err := json.Unmarshal(body, &value); err != nil || value.ID == "" {
		return Webhook{}, fmt.Errorf("invalid webhook response")
	}
	return value, nil
}
func decodeWebhookPage(body []byte) (WebhookPage, error) {
	var value WebhookPage
	if err := json.Unmarshal(body, &value); err != nil || value.Data == nil {
		return WebhookPage{}, fmt.Errorf("invalid webhook page")
	}
	return value, nil
}
func decodeDeletedWebhook(body []byte) (DeletedWebhook, error) {
	var value DeletedWebhook
	if err := json.Unmarshal(body, &value); err != nil || value.ID == "" || !value.Deleted {
		return DeletedWebhook{}, fmt.Errorf("invalid webhook removal")
	}
	return value, nil
}
func decodeWebhookEvent(body []byte) (WebhookEvent, error) {
	var value WebhookEvent
	if err := json.Unmarshal(body, &value); err != nil || value.ID == "" {
		return WebhookEvent{}, fmt.Errorf("invalid webhook event")
	}
	return value, nil
}
func decodeWebhookEventPage(body []byte) (WebhookEventPage, error) {
	var value WebhookEventPage
	if err := json.Unmarshal(body, &value); err != nil || value.Data == nil {
		return WebhookEventPage{}, fmt.Errorf("invalid webhook event page")
	}
	return value, nil
}
func decodeWebhookEventAttemptPage(body []byte) (WebhookEventAttemptPage, error) {
	var value WebhookEventAttemptPage
	if err := json.Unmarshal(body, &value); err != nil || value.Data == nil {
		return WebhookEventAttemptPage{}, fmt.Errorf("invalid webhook attempt page")
	}
	return value, nil
}
