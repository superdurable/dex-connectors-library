// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package resend

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// CreateInboxInput creates a sending and receiving inbox on a verified domain.
type CreateInboxInput struct {
	// EmailAddress is the inbox address on a verified domain.
	EmailAddress string `json:"emailAddress"`
	// Name is an optional internal name; blank uses the address.
	Name string `json:"name,omitempty"`
	// FromName is the optional recipient-visible sender name.
	FromName string `json:"fromName,omitempty"`
	// Forwarding provisions a receiving address without requiring an inbound MX record.
	Forwarding bool `json:"forwarding,omitempty"`
}

// GetInboxInput selects an inbox by ID or email address.
type GetInboxInput struct {
	// ID is the inbox ID or email address.
	ID string `json:"id"`
}

// ListInboxesInput selects one inbox page.
type ListInboxesInput struct {
	// Limit is the page size from 1 through 100; zero uses Resend's default.
	Limit int `json:"limit,omitempty"`
	// After is the exclusive inbox ID cursor.
	After string `json:"after,omitempty"`
}

// UpdateInboxInput replaces selected absolute inbox names.
type UpdateInboxInput struct {
	// ID is the inbox ID or email address.
	ID string `json:"id"`
	// Name replaces the internal name when non-nil.
	Name *string `json:"name,omitempty"`
	// FromName replaces the recipient-visible name; blank clears it.
	FromName *string `json:"fromName,omitempty"`
}

// DeleteInboxInput selects an inbox for removal.
type DeleteInboxInput struct {
	// ID is the inbox ID or email address.
	ID string `json:"id"`
}

// InboxReference identifies a created or changed inbox.
type InboxReference struct {
	// ID is the provider inbox ID.
	ID string `json:"id"`
}

// Inbox contains safe inbox metadata.
type Inbox struct {
	// ID is the inbox ID.
	ID string `json:"id"`
	// Name is the internal display name.
	Name string `json:"name"`
	// EmailAddress is the inbox address.
	EmailAddress string `json:"email_address"`
	// DomainID is the sending domain ID.
	DomainID string `json:"domain_id"`
	// ReceivingAddress is the optional forwarding destination.
	ReceivingAddress string `json:"receiving_address"`
	// FromName is the recipient-visible sender name.
	FromName string `json:"from_name"`
	// Unread counts unread threads in the inbox folder.
	Unread int `json:"unread"`
	// Drafts counts unsent drafts.
	Drafts int `json:"drafts"`
	// LastReceived is the newest received email timestamp.
	LastReceived string `json:"last_received"`
	// CreatedAt is the creation timestamp.
	CreatedAt string `json:"created_at"`
	// UpdatedAt is the update timestamp.
	UpdatedAt string `json:"updated_at"`
}

// InboxPage contains one inbox page.
type InboxPage struct {
	// Data contains safe inbox metadata.
	Data []Inbox `json:"data"`
	// HasMore reports whether another page exists.
	HasMore bool `json:"has_more"`
}

// DeletedInbox confirms one removal.
type DeletedInbox struct {
	// ID is the removed inbox ID.
	ID string `json:"id"`
	// Deleted confirms removal.
	Deleted bool `json:"deleted"`
}

// CreateInboxOperation creates an inbox with bounded resend.
type CreateInboxOperation struct{ client *Client }

// GetInboxOperation retrieves inbox metadata.
type GetInboxOperation struct{ client *Client }

// ListInboxesOperation lists inbox metadata.
type ListInboxesOperation struct{ client *Client }

// UpdateInboxOperation replaces inbox names.
type UpdateInboxOperation struct{ client *Client }

// DeleteInboxOperation removes an inbox.
type DeleteInboxOperation struct{ client *Client }

// CreateInbox binds inbox creation to this client.
func (client *Client) CreateInbox() CreateInboxOperation { return CreateInboxOperation{client} }

// GetInbox binds inbox retrieval to this client.
func (client *Client) GetInbox() GetInboxOperation { return GetInboxOperation{client} }

// ListInboxes binds inbox listing to this client.
func (client *Client) ListInboxes() ListInboxesOperation { return ListInboxesOperation{client} }

// UpdateInbox binds inbox updates to this client.
func (client *Client) UpdateInbox() UpdateInboxOperation { return UpdateInboxOperation{client} }

// DeleteInbox binds inbox removal to this client.
func (client *Client) DeleteInbox() DeleteInboxOperation { return DeleteInboxOperation{client} }

// Definition returns the generated createInbox contract.
func (CreateInboxOperation) Definition() sdkgo.MutationDefinition { return CreateInboxDefinition }

// Definition returns the generated getInbox contract.
func (GetInboxOperation) Definition() sdkgo.QueryDefinition { return GetInboxDefinition }

// Definition returns the generated listInboxes contract.
func (ListInboxesOperation) Definition() sdkgo.QueryDefinition { return ListInboxesDefinition }

// Definition returns the generated updateInbox contract.
func (UpdateInboxOperation) Definition() sdkgo.MutationDefinition { return UpdateInboxDefinition }

// Definition returns the generated deleteInbox contract.
func (DeleteInboxOperation) Definition() sdkgo.MutationDefinition { return DeleteInboxDefinition }

// IdempotencyKey correlates possible inbox creation attempts.
func (CreateInboxOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateInboxInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates absolute name updates.
func (UpdateInboxOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateInboxInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates harmless repeated deletion.
func (DeleteInboxOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteInboxInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates one inbox, optionally with forwarding.
func (operation CreateInboxOperation) Invoke(call sdkgo.Call, input CreateInboxInput) sdkgo.MutationAttempt[InboxReference] {
	if strings.TrimSpace(input.EmailAddress) == "" || !strings.Contains(input.EmailAddress, "@") {
		return mutationFailure[InboxReference]("createInbox", CreateInboxBranchDefect, sdkgo.FailureValidation, "inbox email address is required")
	}
	fields := map[string]any{"email_address": input.EmailAddress, "forwarding": input.Forwarding}
	if input.Name != "" {
		fields["name"] = input.Name
	}
	if input.FromName != "" {
		fields["from_name"] = input.FromName
	}
	body, _ := json.Marshal(fields)
	request := resendLimitedCreate[InboxReference]{client: operation.client, call: call, operation: "createInbox", path: "/inboxes", body: body, timeout: CreateInboxDefinition.StepDefaults.ExecuteMethodTimeout, created: CreateInboxBranchCreated, rejected: CreateInboxBranchProviderRejected, defect: CreateInboxBranchDefect, decode: decodeInboxReference, readBack: func(call sdkgo.Call) (InboxReference, readBackState) { return operation.readBackInbox(call, input) }}
	return request.invoke()
}

// Invoke retrieves inbox metadata by ID or email address.
func (operation GetInboxOperation) Invoke(call sdkgo.Call, input GetInboxInput) sdkgo.QueryAttempt[Inbox] {
	if strings.TrimSpace(input.ID) == "" {
		return queryFailure[Inbox]("getInbox", GetInboxBranchDefect, sdkgo.FailureValidation, "inbox ID or address is required")
	}
	return runQuery(operation.client, call, "getInbox", "/inboxes/"+url.PathEscape(input.ID), GetInboxBranchFound, GetInboxBranchNotFound, GetInboxBranchProviderRejected, GetInboxBranchInvalidResponse, GetInboxBranchDefect, decodeInbox)
}

// Invoke lists one inbox page.
func (operation ListInboxesOperation) Invoke(call sdkgo.Call, input ListInboxesInput) sdkgo.QueryAttempt[InboxPage] {
	query, err := listQuery(input.Limit, input.After, false)
	if err != nil {
		return queryFailure[InboxPage]("listInboxes", ListInboxesBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	return runQuery(operation.client, call, "listInboxes", "/inboxes"+query, ListInboxesBranchListed, "", ListInboxesBranchProviderRejected, ListInboxesBranchInvalidResponse, ListInboxesBranchDefect, decodeInboxPage)
}

// Invoke replaces selected absolute inbox names.
func (operation UpdateInboxOperation) Invoke(call sdkgo.Call, input UpdateInboxInput) sdkgo.MutationAttempt[InboxReference] {
	if strings.TrimSpace(input.ID) == "" || (input.Name == nil && input.FromName == nil) {
		return mutationFailure[InboxReference]("updateInbox", UpdateInboxBranchDefect, sdkgo.FailureValidation, "inbox ID and at least one name are required")
	}
	fields := map[string]any{}
	if input.Name != nil {
		if strings.TrimSpace(*input.Name) == "" {
			return mutationFailure[InboxReference]("updateInbox", UpdateInboxBranchDefect, sdkgo.FailureValidation, "internal name cannot be blank")
		}
		fields["name"] = *input.Name
	}
	if input.FromName != nil {
		fields["from_name"] = *input.FromName
	}
	body, _ := json.Marshal(fields)
	return runMutation(operation.client, call, "updateInbox", http.MethodPatch, "/inboxes/"+url.PathEscape(input.ID), body, "", "", UpdateInboxBranchUpdated, UpdateInboxBranchNotFound, UpdateInboxBranchProviderRejected, UpdateInboxBranchInvalidResponse, UpdateInboxBranchDefect, decodeInboxReference)
}

// Invoke removes one inbox.
func (operation DeleteInboxOperation) Invoke(call sdkgo.Call, input DeleteInboxInput) sdkgo.MutationAttempt[DeletedInbox] {
	if strings.TrimSpace(input.ID) == "" {
		return mutationFailure[DeletedInbox]("deleteInbox", DeleteInboxBranchDefect, sdkgo.FailureValidation, "inbox ID or address is required")
	}
	return runMutation(operation.client, call, "deleteInbox", http.MethodDelete, "/inboxes/"+url.PathEscape(input.ID), nil, "", "", DeleteInboxBranchDeleted, DeleteInboxBranchNotFound, DeleteInboxBranchProviderRejected, DeleteInboxBranchInvalidResponse, DeleteInboxBranchDefect, decodeDeletedInbox)
}

func (operation CreateInboxOperation) readBackInbox(call sdkgo.Call, input CreateInboxInput) (InboxReference, readBackState) {
	body, state := operation.client.readBackResource(call, "/inboxes/"+url.PathEscape(input.EmailAddress))
	if state != readBackMatched {
		return InboxReference{}, state
	}
	inbox, err := decodeInbox(body)
	if err != nil {
		return InboxReference{}, readBackRetry
	}
	expectedName := input.Name
	if expectedName == "" {
		expectedName = input.EmailAddress
	}
	if !strings.EqualFold(inbox.EmailAddress, input.EmailAddress) || inbox.Name != expectedName || inbox.FromName != input.FromName || (input.Forwarding != (inbox.ReceivingAddress != "")) {
		return InboxReference{}, readBackConflicted
	}
	return InboxReference{ID: inbox.ID}, readBackMatched
}
func decodeInboxReference(body []byte) (InboxReference, error) {
	var value InboxReference
	if err := json.Unmarshal(body, &value); err != nil || value.ID == "" {
		return InboxReference{}, fmt.Errorf("invalid inbox reference")
	}
	return value, nil
}
func decodeInbox(body []byte) (Inbox, error) {
	var value Inbox
	if err := json.Unmarshal(body, &value); err != nil || value.ID == "" || value.EmailAddress == "" {
		return Inbox{}, fmt.Errorf("invalid inbox response")
	}
	return value, nil
}
func decodeInboxPage(body []byte) (InboxPage, error) {
	var value InboxPage
	if err := json.Unmarshal(body, &value); err != nil || value.Data == nil {
		return InboxPage{}, fmt.Errorf("invalid inbox page")
	}
	return value, nil
}
func decodeDeletedInbox(body []byte) (DeletedInbox, error) {
	var value DeletedInbox
	if err := json.Unmarshal(body, &value); err != nil || value.ID == "" || !value.Deleted {
		return DeletedInbox{}, fmt.Errorf("invalid inbox removal")
	}
	return value, nil
}
