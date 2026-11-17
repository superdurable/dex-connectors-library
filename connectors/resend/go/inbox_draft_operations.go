// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package resend

import (
	"encoding/json"
	"fmt"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"net/http"
	"net/url"
	"strings"
)

// ListInboxDraftsInput selects a page of unsent drafts.
type ListInboxDraftsInput struct {
	// InboxID is the inbox ID or address.
	InboxID string `json:"inboxId"`
	// Limit is the page size from 1 to 100.
	Limit int `json:"limit,omitempty"`
	// After is an exclusive draft ID cursor.
	After string `json:"after,omitempty"`
	// Before is an exclusive draft ID cursor.
	Before string `json:"before,omitempty"`
}

// CreateInboxDraftInput creates an unsent draft.
type CreateInboxDraftInput struct {
	// InboxID is the inbox ID or address.
	InboxID string `json:"inboxId"`
	// To lists recipients.
	To []string `json:"to,omitempty"`
	// Cc lists CC recipients.
	Cc []string `json:"cc,omitempty"`
	// Bcc lists BCC recipients.
	Bcc []string `json:"bcc,omitempty"`
	// Subject is the optional draft subject.
	Subject string `json:"subject,omitempty"`
	// Text is the optional plain-text body.
	Text string `json:"text,omitempty"`
	// Html is the optional HTML body.
	Html string `json:"html,omitempty"`
	// ThreadID identifies the reply thread when paired with ReplyToEmailID.
	ThreadID string `json:"threadId,omitempty"`
	// ReplyToEmailID identifies the replied-to email when paired with ThreadID.
	ReplyToEmailID string `json:"replyToEmailId,omitempty"`
}

// GetInboxDraftInput identifies one unsent draft.
type GetInboxDraftInput struct {
	// InboxID is the inbox ID or address.
	InboxID string `json:"inboxId"`
	// DraftID is the draft ID.
	DraftID string `json:"draftId"`
}

// UpdateInboxDraftInput replaces selected absolute draft fields.
type UpdateInboxDraftInput struct {
	// InboxID is the inbox ID or address.
	InboxID string `json:"inboxId"`
	// DraftID is the draft ID.
	DraftID string `json:"draftId"`
	// To replaces recipients when non-nil; empty clears them.
	To *[]string `json:"to,omitempty"`
	// Cc replaces CC recipients when non-nil.
	Cc *[]string `json:"cc,omitempty"`
	// Bcc replaces BCC recipients when non-nil.
	Bcc *[]string `json:"bcc,omitempty"`
	// Subject replaces the subject when non-nil.
	Subject *string `json:"subject,omitempty"`
	// Text replaces the plain-text body when non-nil.
	Text *string `json:"text,omitempty"`
	// Html replaces the HTML body when non-nil.
	Html *string `json:"html,omitempty"`
}

// DeleteInboxDraftInput identifies a draft to discard.
type DeleteInboxDraftInput GetInboxDraftInput

// SendInboxDraftInput identifies an unsent draft to send.
type SendInboxDraftInput GetInboxDraftInput

// InboxDraftReference identifies a created or updated draft.
type InboxDraftReference struct {
	// ID is the provider draft ID.
	ID string `json:"id"`
}

// InboxDraft contains metadata without message bodies or attachments.
type InboxDraft struct {
	// ID is the draft ID.
	ID string `json:"id"`
	// Type is standalone or reply.
	Type string `json:"type"`
	// ThreadID is the reply thread, if any.
	ThreadID string `json:"thread_id"`
	// ReplyToEmailID is the replied-to email, if any.
	ReplyToEmailID string `json:"reply_to_email_id"`
	// Subject is the message subject.
	Subject string `json:"subject"`
	// To lists recipients.
	To []string `json:"to"`
	// CreatedAt is the creation time.
	CreatedAt string `json:"created_at"`
	// UpdatedAt is the latest update time.
	UpdatedAt string `json:"updated_at"`
}

// InboxDraftPage is one page of unsent draft metadata.
type InboxDraftPage struct {
	// Data contains draft metadata.
	Data []InboxDraft `json:"data"`
	// HasMore indicates another page exists.
	HasMore bool `json:"has_more"`
}

// DeletedInboxDraft confirms draft removal.
type DeletedInboxDraft struct {
	// ID is the removed draft ID.
	ID string `json:"id"`
	// Deleted confirms removal.
	Deleted bool `json:"deleted"`
}

// SentInboxDraft identifies the sent email.
type SentInboxDraft struct {
	// ID is the sent email ID.
	ID string `json:"id"`
}

// ListInboxDraftsOperation performs the ListInboxDrafts provider operation.
type ListInboxDraftsOperation struct{ client *Client }

// ListInboxDrafts binds this operation to the client.
func (client *Client) ListInboxDrafts() ListInboxDraftsOperation {
	return ListInboxDraftsOperation{client}
}

// Definition returns the generated contract.
func (ListInboxDraftsOperation) Definition() sdkgo.QueryDefinition { return ListInboxDraftsDefinition }

// CreateInboxDraftOperation performs the CreateInboxDraft provider operation.
type CreateInboxDraftOperation struct{ client *Client }

// CreateInboxDraft binds this operation to the client.
func (client *Client) CreateInboxDraft() CreateInboxDraftOperation {
	return CreateInboxDraftOperation{client}
}

// Definition returns the generated contract.
func (CreateInboxDraftOperation) Definition() sdkgo.MutationDefinition {
	return CreateInboxDraftDefinition
}

// IdempotencyKey correlates retries for this operation.
func (CreateInboxDraftOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateInboxDraftInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// GetInboxDraftOperation performs the GetInboxDraft provider operation.
type GetInboxDraftOperation struct{ client *Client }

// GetInboxDraft binds this operation to the client.
func (client *Client) GetInboxDraft() GetInboxDraftOperation { return GetInboxDraftOperation{client} }

// Definition returns the generated contract.
func (GetInboxDraftOperation) Definition() sdkgo.QueryDefinition { return GetInboxDraftDefinition }

// UpdateInboxDraftOperation performs the UpdateInboxDraft provider operation.
type UpdateInboxDraftOperation struct{ client *Client }

// UpdateInboxDraft binds this operation to the client.
func (client *Client) UpdateInboxDraft() UpdateInboxDraftOperation {
	return UpdateInboxDraftOperation{client}
}

// Definition returns the generated contract.
func (UpdateInboxDraftOperation) Definition() sdkgo.MutationDefinition {
	return UpdateInboxDraftDefinition
}

// IdempotencyKey correlates retries for this operation.
func (UpdateInboxDraftOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateInboxDraftInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// DeleteInboxDraftOperation performs the DeleteInboxDraft provider operation.
type DeleteInboxDraftOperation struct{ client *Client }

// DeleteInboxDraft binds this operation to the client.
func (client *Client) DeleteInboxDraft() DeleteInboxDraftOperation {
	return DeleteInboxDraftOperation{client}
}

// Definition returns the generated contract.
func (DeleteInboxDraftOperation) Definition() sdkgo.MutationDefinition {
	return DeleteInboxDraftDefinition
}

// IdempotencyKey correlates retries for this operation.
func (DeleteInboxDraftOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteInboxDraftInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// SendInboxDraftOperation performs the SendInboxDraft provider operation.
type SendInboxDraftOperation struct{ client *Client }

// SendInboxDraft binds this operation to the client.
func (client *Client) SendInboxDraft() SendInboxDraftOperation {
	return SendInboxDraftOperation{client}
}

// Definition returns the generated contract.
func (SendInboxDraftOperation) Definition() sdkgo.MutationDefinition { return SendInboxDraftDefinition }

// IdempotencyKey correlates retries for this operation.
func (SendInboxDraftOperation) IdempotencyKey(callID sdkgo.CallID, _ SendInboxDraftInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke lists one page of unsent draft metadata.
func (operation ListInboxDraftsOperation) Invoke(call sdkgo.Call, input ListInboxDraftsInput) sdkgo.QueryAttempt[InboxDraftPage] {
	path, ok := inboxDraftsPath(input.InboxID)
	if !ok {
		return queryFailure[InboxDraftPage]("listInboxDrafts", ListInboxDraftsBranchDefect, sdkgo.FailureValidation, "inbox ID is required")
	}
	values, ok := threadPaginationValues(input.Limit, input.After, input.Before)
	if !ok {
		return queryFailure[InboxDraftPage]("listInboxDrafts", ListInboxDraftsBranchDefect, sdkgo.FailureValidation, "pagination is invalid")
	}
	return runQuery(operation.client, call, "listInboxDrafts", appendQuery(path, values), ListInboxDraftsBranchListed, ListInboxDraftsBranchNotFound, ListInboxDraftsBranchProviderRejected, ListInboxDraftsBranchInvalidResponse, ListInboxDraftsBranchDefect, decodeInboxDraftPage)
}

// Invoke creates an unsent draft with a provider idempotency key.
func (operation CreateInboxDraftOperation) Invoke(call sdkgo.Call, input CreateInboxDraftInput) sdkgo.MutationAttempt[InboxDraftReference] {
	path, ok := inboxDraftsPath(input.InboxID)
	if !ok || (input.ThreadID == "") != (input.ReplyToEmailID == "") || len(input.To)+len(input.Cc)+len(input.Bcc) > 50 || hasBlankString(input.To) || hasBlankString(input.Cc) || hasBlankString(input.Bcc) || (len(input.To)+len(input.Cc)+len(input.Bcc) == 0 && strings.TrimSpace(input.Subject) == "" && strings.TrimSpace(input.Text) == "" && strings.TrimSpace(input.Html) == "") {
		return mutationFailure[InboxDraftReference]("createInboxDraft", CreateInboxDraftBranchDefect, sdkgo.FailureValidation, "draft fields are invalid")
	}
	fields := map[string]any{}
	if len(input.To) > 0 {
		fields["to"] = input.To
	}
	if len(input.Cc) > 0 {
		fields["cc"] = input.Cc
	}
	if len(input.Bcc) > 0 {
		fields["bcc"] = input.Bcc
	}
	if input.Subject != "" {
		fields["subject"] = input.Subject
	}
	if input.Text != "" {
		fields["text"] = input.Text
	}
	if input.Html != "" {
		fields["html"] = input.Html
	}
	if input.ThreadID != "" {
		fields["thread_id"] = input.ThreadID
		fields["reply_to_email_id"] = input.ReplyToEmailID
	}
	body, _ := json.Marshal(fields)
	return runMutation(operation.client, call, "createInboxDraft", http.MethodPost, path, body, "Idempotency-Key", string(call.IdempotencyKey), CreateInboxDraftBranchCreated, CreateInboxDraftBranchNotFound, CreateInboxDraftBranchProviderRejected, CreateInboxDraftBranchInvalidResponse, CreateInboxDraftBranchDefect, decodeInboxDraftReference)
}

// Invoke retrieves one draft without message bodies or attachments.
func (operation GetInboxDraftOperation) Invoke(call sdkgo.Call, input GetInboxDraftInput) sdkgo.QueryAttempt[InboxDraft] {
	path, ok := inboxDraftPath(input.InboxID, input.DraftID)
	if !ok {
		return queryFailure[InboxDraft]("getInboxDraft", GetInboxDraftBranchDefect, sdkgo.FailureValidation, "inbox and draft IDs are required")
	}
	return runQuery(operation.client, call, "getInboxDraft", path, GetInboxDraftBranchFound, GetInboxDraftBranchNotFound, GetInboxDraftBranchProviderRejected, GetInboxDraftBranchInvalidResponse, GetInboxDraftBranchDefect, decodeInboxDraft)
}

// Invoke replaces selected absolute draft fields.
func (operation UpdateInboxDraftOperation) Invoke(call sdkgo.Call, input UpdateInboxDraftInput) sdkgo.MutationAttempt[InboxDraftReference] {
	path, ok := inboxDraftPath(input.InboxID, input.DraftID)
	if !ok || (input.To == nil && input.Cc == nil && input.Bcc == nil && input.Subject == nil && input.Text == nil && input.Html == nil) {
		return mutationFailure[InboxDraftReference]("updateInboxDraft", UpdateInboxDraftBranchDefect, sdkgo.FailureValidation, "draft ID and one field are required")
	}
	fields := map[string]any{}
	if input.To != nil {
		if hasBlankString(*input.To) {
			return mutationFailure[InboxDraftReference]("updateInboxDraft", UpdateInboxDraftBranchDefect, sdkgo.FailureValidation, "recipient is blank")
		}
		fields["to"] = *input.To
	}
	if input.Cc != nil {
		if hasBlankString(*input.Cc) {
			return mutationFailure[InboxDraftReference]("updateInboxDraft", UpdateInboxDraftBranchDefect, sdkgo.FailureValidation, "CC recipient is blank")
		}
		fields["cc"] = *input.Cc
	}
	if input.Bcc != nil {
		if hasBlankString(*input.Bcc) {
			return mutationFailure[InboxDraftReference]("updateInboxDraft", UpdateInboxDraftBranchDefect, sdkgo.FailureValidation, "BCC recipient is blank")
		}
		fields["bcc"] = *input.Bcc
	}
	if input.Subject != nil {
		fields["subject"] = *input.Subject
	}
	if input.Text != nil {
		fields["text"] = *input.Text
	}
	if input.Html != nil {
		fields["html"] = *input.Html
	}
	body, _ := json.Marshal(fields)
	return runMutation(operation.client, call, "updateInboxDraft", http.MethodPatch, path, body, "", "", UpdateInboxDraftBranchUpdated, UpdateInboxDraftBranchNotFound, UpdateInboxDraftBranchProviderRejected, UpdateInboxDraftBranchInvalidResponse, UpdateInboxDraftBranchDefect, decodeInboxDraftReference)
}

// Invoke discards an unsent draft.
func (operation DeleteInboxDraftOperation) Invoke(call sdkgo.Call, input DeleteInboxDraftInput) sdkgo.MutationAttempt[DeletedInboxDraft] {
	path, ok := inboxDraftPath(input.InboxID, input.DraftID)
	if !ok {
		return mutationFailure[DeletedInboxDraft]("deleteInboxDraft", DeleteInboxDraftBranchDefect, sdkgo.FailureValidation, "inbox and draft IDs are required")
	}
	return runMutation(operation.client, call, "deleteInboxDraft", http.MethodDelete, path, nil, "", "", DeleteInboxDraftBranchDeleted, DeleteInboxDraftBranchNotFound, DeleteInboxDraftBranchProviderRejected, DeleteInboxDraftBranchInvalidResponse, DeleteInboxDraftBranchDefect, decodeDeletedInboxDraft)
}

// Invoke sends an unsent draft once with a provider idempotency key.
func (operation SendInboxDraftOperation) Invoke(call sdkgo.Call, input SendInboxDraftInput) sdkgo.MutationAttempt[SentInboxDraft] {
	path, ok := inboxDraftPath(input.InboxID, input.DraftID)
	if !ok {
		return mutationFailure[SentInboxDraft]("sendInboxDraft", SendInboxDraftBranchDefect, sdkgo.FailureValidation, "inbox and draft IDs are required")
	}
	return runMutation(operation.client, call, "sendInboxDraft", http.MethodPost, path+"/send", nil, "Idempotency-Key", string(call.IdempotencyKey), SendInboxDraftBranchSent, SendInboxDraftBranchNotFound, SendInboxDraftBranchProviderRejected, SendInboxDraftBranchInvalidResponse, SendInboxDraftBranchDefect, decodeSentInboxDraft)
}
func inboxDraftsPath(inboxID string) (string, bool) {
	if strings.TrimSpace(inboxID) == "" {
		return "", false
	}
	return "/inboxes/" + url.PathEscape(inboxID) + "/drafts", true
}
func inboxDraftPath(inboxID, draftID string) (string, bool) {
	path, ok := inboxDraftsPath(inboxID)
	if !ok || strings.TrimSpace(draftID) == "" {
		return "", false
	}
	return path + "/" + url.PathEscape(draftID), true
}
func decodeInboxDraftPage(body []byte) (InboxDraftPage, error) {
	var value InboxDraftPage
	if err := json.Unmarshal(body, &value); err != nil || value.Data == nil {
		return InboxDraftPage{}, fmt.Errorf("invalid draft page")
	}
	return value, nil
}
func decodeInboxDraft(body []byte) (InboxDraft, error) {
	var value InboxDraft
	if err := json.Unmarshal(body, &value); err != nil || value.ID == "" {
		return InboxDraft{}, fmt.Errorf("invalid draft")
	}
	return value, nil
}
func decodeInboxDraftReference(body []byte) (InboxDraftReference, error) {
	var value InboxDraftReference
	if err := json.Unmarshal(body, &value); err != nil || value.ID == "" {
		return InboxDraftReference{}, fmt.Errorf("invalid draft reference")
	}
	return value, nil
}
func decodeDeletedInboxDraft(body []byte) (DeletedInboxDraft, error) {
	var value DeletedInboxDraft
	if err := json.Unmarshal(body, &value); err != nil || value.ID == "" || !value.Deleted {
		return DeletedInboxDraft{}, fmt.Errorf("invalid draft removal")
	}
	return value, nil
}
func decodeSentInboxDraft(body []byte) (SentInboxDraft, error) {
	var value SentInboxDraft
	if err := json.Unmarshal(body, &value); err != nil || value.ID == "" {
		return SentInboxDraft{}, fmt.Errorf("invalid sent draft")
	}
	return value, nil
}
