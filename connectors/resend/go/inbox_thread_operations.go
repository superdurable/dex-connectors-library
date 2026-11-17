// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package resend

import (
	"encoding/json"
	"fmt"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// ListInboxThreadsInput selects one page of thread metadata.
type ListInboxThreadsInput struct {
	// InboxID is the inbox ID or address.
	InboxID string `json:"inboxId"`
	// Folders filters by inbox, archive, spam, sent, or trash.
	Folders []string `json:"folders,omitempty"`
	// Labels filters by up to 50 label IDs.
	Labels []string `json:"labels,omitempty"`
	// Read filters by all-read or at-least-one-unread.
	Read *bool `json:"read,omitempty"`
	// Limit is the page size, from 1 through 100.
	Limit int `json:"limit,omitempty"`
	// After is an exclusive thread ID cursor.
	After string `json:"after,omitempty"`
	// Before is an exclusive thread ID cursor.
	Before string `json:"before,omitempty"`
}

// SearchInboxThreadsInput searches thread metadata.
type SearchInboxThreadsInput struct {
	// InboxID is the inbox ID or address.
	InboxID string `json:"inboxId"`
	// Folders filters by inbox, archive, spam, sent, or trash.
	Folders []string `json:"folders,omitempty"`
	// Labels filters by label IDs.
	Labels []string `json:"labels,omitempty"`
	// Read filters by all-read or at-least-one-unread.
	Read *bool `json:"read,omitempty"`
	// Limit is the page size.
	Limit int `json:"limit,omitempty"`
	// After is an exclusive thread ID cursor.
	After string `json:"after,omitempty"`
	// Before is an exclusive thread ID cursor.
	Before string `json:"before,omitempty"`
	// Query searches subject, body, senders, recipients, and attachment names.
	Query string `json:"query,omitempty"`
	// From matches one or more senders.
	From []string `json:"from,omitempty"`
	// To matches one or more recipients.
	To []string `json:"to,omitempty"`
	// Cc matches one or more CC recipients.
	Cc []string `json:"cc,omitempty"`
	// Bcc matches one or more BCC recipients.
	Bcc []string `json:"bcc,omitempty"`
	// HasAttachment filters by attachment presence.
	HasAttachment *bool `json:"hasAttachment,omitempty"`
	// StartDate is the inclusive date or ISO 8601 timestamp.
	StartDate string `json:"startDate,omitempty"`
	// EndDate is the inclusive date or ISO 8601 timestamp.
	EndDate string `json:"endDate,omitempty"`
}

// GetInboxThreadInput identifies an inbox thread.
type GetInboxThreadInput struct {
	// InboxID is the inbox ID or address.
	InboxID string `json:"inboxId"`
	// ThreadID is the thread ID.
	ThreadID string `json:"threadId"`
}

// UpdateInboxThreadInput replaces read state or folder.
type UpdateInboxThreadInput struct {
	// InboxID is the inbox ID or address.
	InboxID string `json:"inboxId"`
	// ThreadID is the thread ID.
	ThreadID string `json:"threadId"`
	// Read marks all emails read or unread when supplied.
	Read *bool `json:"read,omitempty"`
	// Folder moves the thread to inbox, archive, spam, or trash.
	Folder *string `json:"folder,omitempty"`
}

// DeleteInboxThreadInput identifies a thread to move to trash.
type DeleteInboxThreadInput GetInboxThreadInput

// ChangeInboxThreadLabelsInput supplies a thread and label IDs.
type ChangeInboxThreadLabelsInput struct {
	// InboxID is the inbox ID or address.
	InboxID string `json:"inboxId"`
	// ThreadID is the thread ID.
	ThreadID string `json:"threadId"`
	// Labels are provider label IDs; an empty set is allowed only for replacement.
	Labels []string `json:"labels"`
}

// RemoveInboxThreadLabelInput identifies one label to remove.
type RemoveInboxThreadLabelInput struct {
	// InboxID is the inbox ID or address.
	InboxID string `json:"inboxId"`
	// ThreadID is the thread ID.
	ThreadID string `json:"threadId"`
	// LabelID is the provider label ID.
	LabelID string `json:"labelId"`
}

// ListInboxThreadEmailsInput selects a page of thread email metadata.
type ListInboxThreadEmailsInput struct {
	// InboxID is the inbox ID or address.
	InboxID string `json:"inboxId"`
	// ThreadID is the thread ID.
	ThreadID string `json:"threadId"`
	// Limit is the page size, from 1 through 100.
	Limit int `json:"limit,omitempty"`
	// After is an exclusive email ID cursor.
	After string `json:"after,omitempty"`
	// Before is an exclusive email ID cursor.
	Before string `json:"before,omitempty"`
}

// GetInboxThreadEmailInput identifies one email within a thread.
type GetInboxThreadEmailInput struct {
	// InboxID is the inbox ID or address.
	InboxID string `json:"inboxId"`
	// ThreadID is the thread ID.
	ThreadID string `json:"threadId"`
	// EmailID is the email ID.
	EmailID string `json:"emailId"`
}

// ReplyInboxThreadEmailInput sends a keyed reply without a stored draft.
type ReplyInboxThreadEmailInput struct {
	// InboxID is the inbox ID or address.
	InboxID string `json:"inboxId"`
	// ThreadID is the thread ID.
	ThreadID string `json:"threadId"`
	// EmailID is the email ID.
	EmailID string `json:"emailId"`
	// Html is the HTML body; supply Html or Text.
	Html string `json:"html,omitempty"`
	// Text is the plain-text body; supply Html or Text.
	Text string `json:"text,omitempty"`
	// Subject overrides the default reply subject.
	Subject string `json:"subject,omitempty"`
	// ReplyAll copies all eligible original recipients.
	ReplyAll bool `json:"replyAll,omitempty"`
	// Cc adds CC recipients.
	Cc []string `json:"cc,omitempty"`
	// Bcc adds BCC recipients.
	Bcc []string `json:"bcc,omitempty"`
}

// ForwardInboxThreadEmailInput sends a keyed forward without a stored draft.
type ForwardInboxThreadEmailInput struct {
	// InboxID is the inbox ID or address.
	InboxID string `json:"inboxId"`
	// ThreadID is the thread ID.
	ThreadID string `json:"threadId"`
	// EmailID is the email ID.
	EmailID string `json:"emailId"`
	// To contains at least one recipient address.
	To []string `json:"to"`
	// Cc adds CC recipients.
	Cc []string `json:"cc,omitempty"`
	// Bcc adds BCC recipients.
	Bcc []string `json:"bcc,omitempty"`
	// Html is an optional HTML note above the forwarded message.
	Html string `json:"html,omitempty"`
	// Text is an optional plain-text note above the forwarded message.
	Text string `json:"text,omitempty"`
	// Subject overrides the default forward subject.
	Subject string `json:"subject,omitempty"`
}

// InboxThread is safe thread metadata without message bodies.
type InboxThread struct {
	// ID is the thread ID.
	ID string `json:"id"`
	// Subject is the latest subject.
	Subject string `json:"subject"`
	// Folder is inbox, archive, spam, sent, or trash.
	Folder string `json:"folder"`
	// Read reports whether all messages are read.
	Read bool `json:"read"`
	// EmailCount counts messages.
	EmailCount int `json:"email_count"`
	// HasAttachment reports an attachment on the latest message.
	HasAttachment bool `json:"has_attachment"`
	// HasDraft reports an unsent draft.
	HasDraft bool `json:"has_draft"`
	// ReceivedAt is the latest activity timestamp.
	ReceivedAt string `json:"received_at"`
	// MatchedEmailID is the matching email in a search result.
	MatchedEmailID string `json:"matched_email_id,omitempty"`
}

// InboxThreadPage is one page of thread summaries.
type InboxThreadPage struct {
	// Data contains thread summaries.
	Data []InboxThread `json:"data"`
	// HasMore indicates another page exists.
	HasMore bool `json:"has_more"`
}

// InboxThreadReference identifies a changed thread.
type InboxThreadReference struct {
	// ID is the thread ID.
	ID string `json:"id"`
	// Folder is the thread folder after a change.
	Folder string `json:"folder"`
	// Read reports whether all messages are read after a change.
	Read bool `json:"read"`
}

// DeletedInboxThread confirms a move to trash.
type DeletedInboxThread struct {
	// ID is the thread ID.
	ID string `json:"id"`
	// Deleted is true when this call moved the thread.
	Deleted bool `json:"deleted"`
}

// InboxThreadLabelList contains labels after a change.
type InboxThreadLabelList struct {
	// Data contains label metadata.
	Data []InboxLabel `json:"data"`
}

// InboxThreadEmail exposes message metadata without body or attachments.
type InboxThreadEmail struct {
	// ID is the message ID.
	ID string `json:"id"`
	// Direction is inbound or outbound.
	Direction string `json:"direction"`
	// From is the sender.
	From string `json:"from"`
	// To lists recipients.
	To []string `json:"to"`
	// Subject is the message subject.
	Subject string `json:"subject"`
	// Read reports read state.
	Read bool `json:"read"`
	// ReceivedAt is the arrival or send timestamp.
	ReceivedAt string `json:"received_at"`
}

// InboxThreadEmailPage is one page of email metadata.
type InboxThreadEmailPage struct {
	// Data contains email metadata.
	Data []InboxThreadEmail `json:"data"`
	// HasMore indicates another page exists.
	HasMore bool `json:"has_more"`
}

// SentInboxThreadEmail identifies a keyed sent message.
type SentInboxThreadEmail struct {
	// ID is the sent message ID.
	ID string `json:"id"`
}

// ListInboxThreadsOperation executes ListInboxThreads.
type ListInboxThreadsOperation struct{ client *Client }

// ListInboxThreads binds the operation to this client.
func (client *Client) ListInboxThreads() ListInboxThreadsOperation {
	return ListInboxThreadsOperation{client}
}

// Definition returns the generated contract.
func (ListInboxThreadsOperation) Definition() sdkgo.QueryDefinition {
	return ListInboxThreadsDefinition
}

// SearchInboxThreadsOperation executes SearchInboxThreads.
type SearchInboxThreadsOperation struct{ client *Client }

// SearchInboxThreads binds the operation to this client.
func (client *Client) SearchInboxThreads() SearchInboxThreadsOperation {
	return SearchInboxThreadsOperation{client}
}

// Definition returns the generated contract.
func (SearchInboxThreadsOperation) Definition() sdkgo.QueryDefinition {
	return SearchInboxThreadsDefinition
}

// GetInboxThreadOperation executes GetInboxThread.
type GetInboxThreadOperation struct{ client *Client }

// GetInboxThread binds the operation to this client.
func (client *Client) GetInboxThread() GetInboxThreadOperation {
	return GetInboxThreadOperation{client}
}

// Definition returns the generated contract.
func (GetInboxThreadOperation) Definition() sdkgo.QueryDefinition { return GetInboxThreadDefinition }

// UpdateInboxThreadOperation executes UpdateInboxThread.
type UpdateInboxThreadOperation struct{ client *Client }

// UpdateInboxThread binds the operation to this client.
func (client *Client) UpdateInboxThread() UpdateInboxThreadOperation {
	return UpdateInboxThreadOperation{client}
}

// Definition returns the generated contract.
func (UpdateInboxThreadOperation) Definition() sdkgo.MutationDefinition {
	return UpdateInboxThreadDefinition
}

// IdempotencyKey correlates retries for this operation.
func (UpdateInboxThreadOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateInboxThreadInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// DeleteInboxThreadOperation executes DeleteInboxThread.
type DeleteInboxThreadOperation struct{ client *Client }

// DeleteInboxThread binds the operation to this client.
func (client *Client) DeleteInboxThread() DeleteInboxThreadOperation {
	return DeleteInboxThreadOperation{client}
}

// Definition returns the generated contract.
func (DeleteInboxThreadOperation) Definition() sdkgo.MutationDefinition {
	return DeleteInboxThreadDefinition
}

// IdempotencyKey correlates retries for this operation.
func (DeleteInboxThreadOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteInboxThreadInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// AddInboxThreadLabelsOperation executes AddInboxThreadLabels.
type AddInboxThreadLabelsOperation struct{ client *Client }

// AddInboxThreadLabels binds the operation to this client.
func (client *Client) AddInboxThreadLabels() AddInboxThreadLabelsOperation {
	return AddInboxThreadLabelsOperation{client}
}

// Definition returns the generated contract.
func (AddInboxThreadLabelsOperation) Definition() sdkgo.MutationDefinition {
	return AddInboxThreadLabelsDefinition
}

// IdempotencyKey correlates retries for this operation.
func (AddInboxThreadLabelsOperation) IdempotencyKey(callID sdkgo.CallID, _ ChangeInboxThreadLabelsInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// SetInboxThreadLabelsOperation executes SetInboxThreadLabels.
type SetInboxThreadLabelsOperation struct{ client *Client }

// SetInboxThreadLabels binds the operation to this client.
func (client *Client) SetInboxThreadLabels() SetInboxThreadLabelsOperation {
	return SetInboxThreadLabelsOperation{client}
}

// Definition returns the generated contract.
func (SetInboxThreadLabelsOperation) Definition() sdkgo.MutationDefinition {
	return SetInboxThreadLabelsDefinition
}

// IdempotencyKey correlates retries for this operation.
func (SetInboxThreadLabelsOperation) IdempotencyKey(callID sdkgo.CallID, _ ChangeInboxThreadLabelsInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// RemoveInboxThreadLabelOperation executes RemoveInboxThreadLabel.
type RemoveInboxThreadLabelOperation struct{ client *Client }

// RemoveInboxThreadLabel binds the operation to this client.
func (client *Client) RemoveInboxThreadLabel() RemoveInboxThreadLabelOperation {
	return RemoveInboxThreadLabelOperation{client}
}

// Definition returns the generated contract.
func (RemoveInboxThreadLabelOperation) Definition() sdkgo.MutationDefinition {
	return RemoveInboxThreadLabelDefinition
}

// IdempotencyKey correlates retries for this operation.
func (RemoveInboxThreadLabelOperation) IdempotencyKey(callID sdkgo.CallID, _ RemoveInboxThreadLabelInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// ListInboxThreadEmailsOperation executes ListInboxThreadEmails.
type ListInboxThreadEmailsOperation struct{ client *Client }

// ListInboxThreadEmails binds the operation to this client.
func (client *Client) ListInboxThreadEmails() ListInboxThreadEmailsOperation {
	return ListInboxThreadEmailsOperation{client}
}

// Definition returns the generated contract.
func (ListInboxThreadEmailsOperation) Definition() sdkgo.QueryDefinition {
	return ListInboxThreadEmailsDefinition
}

// GetInboxThreadEmailOperation executes GetInboxThreadEmail.
type GetInboxThreadEmailOperation struct{ client *Client }

// GetInboxThreadEmail binds the operation to this client.
func (client *Client) GetInboxThreadEmail() GetInboxThreadEmailOperation {
	return GetInboxThreadEmailOperation{client}
}

// Definition returns the generated contract.
func (GetInboxThreadEmailOperation) Definition() sdkgo.QueryDefinition {
	return GetInboxThreadEmailDefinition
}

// ReplyInboxThreadEmailOperation executes ReplyInboxThreadEmail.
type ReplyInboxThreadEmailOperation struct{ client *Client }

// ReplyInboxThreadEmail binds the operation to this client.
func (client *Client) ReplyInboxThreadEmail() ReplyInboxThreadEmailOperation {
	return ReplyInboxThreadEmailOperation{client}
}

// Definition returns the generated contract.
func (ReplyInboxThreadEmailOperation) Definition() sdkgo.MutationDefinition {
	return ReplyInboxThreadEmailDefinition
}

// IdempotencyKey correlates retries for this operation.
func (ReplyInboxThreadEmailOperation) IdempotencyKey(callID sdkgo.CallID, _ ReplyInboxThreadEmailInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// ForwardInboxThreadEmailOperation executes ForwardInboxThreadEmail.
type ForwardInboxThreadEmailOperation struct{ client *Client }

// ForwardInboxThreadEmail binds the operation to this client.
func (client *Client) ForwardInboxThreadEmail() ForwardInboxThreadEmailOperation {
	return ForwardInboxThreadEmailOperation{client}
}

// Definition returns the generated contract.
func (ForwardInboxThreadEmailOperation) Definition() sdkgo.MutationDefinition {
	return ForwardInboxThreadEmailDefinition
}

// IdempotencyKey correlates retries for this operation.
func (ForwardInboxThreadEmailOperation) IdempotencyKey(callID sdkgo.CallID, _ ForwardInboxThreadEmailInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke retrieves provider ListInboxThreads metadata.
func (operation ListInboxThreadsOperation) Invoke(call sdkgo.Call, input ListInboxThreadsInput) sdkgo.QueryAttempt[InboxThreadPage] {
	path, ok := inboxThreadsQueryPath(input)
	if !ok {
		return queryFailure[InboxThreadPage]("listInboxThreads", ListInboxThreadsBranchDefect, sdkgo.FailureValidation, "inbox or thread selection is invalid")
	}
	return runQuery(operation.client, call, "listInboxThreads", path, ListInboxThreadsBranchListed, ListInboxThreadsBranchNotFound, ListInboxThreadsBranchProviderRejected, ListInboxThreadsBranchInvalidResponse, ListInboxThreadsBranchDefect, decodeInboxThreadPage)
}

// Invoke retrieves provider SearchInboxThreads metadata.
func (operation SearchInboxThreadsOperation) Invoke(call sdkgo.Call, input SearchInboxThreadsInput) sdkgo.QueryAttempt[InboxThreadPage] {
	path, ok := inboxThreadsSearchPath(input)
	if !ok {
		return queryFailure[InboxThreadPage]("searchInboxThreads", SearchInboxThreadsBranchDefect, sdkgo.FailureValidation, "inbox or thread selection is invalid")
	}
	return runQuery(operation.client, call, "searchInboxThreads", path, SearchInboxThreadsBranchListed, SearchInboxThreadsBranchNotFound, SearchInboxThreadsBranchProviderRejected, SearchInboxThreadsBranchInvalidResponse, SearchInboxThreadsBranchDefect, decodeInboxThreadPage)
}

// Invoke retrieves provider GetInboxThread metadata.
func (operation GetInboxThreadOperation) Invoke(call sdkgo.Call, input GetInboxThreadInput) sdkgo.QueryAttempt[InboxThreadReference] {
	path, ok := inboxThreadPath(input.InboxID, input.ThreadID)
	if !ok {
		return queryFailure[InboxThreadReference]("getInboxThread", GetInboxThreadBranchDefect, sdkgo.FailureValidation, "inbox or thread selection is invalid")
	}
	return runQuery(operation.client, call, "getInboxThread", path, GetInboxThreadBranchFound, GetInboxThreadBranchNotFound, GetInboxThreadBranchProviderRejected, GetInboxThreadBranchInvalidResponse, GetInboxThreadBranchDefect, decodeInboxThreadReference)
}

// Invoke retrieves provider ListInboxThreadEmails metadata.
func (operation ListInboxThreadEmailsOperation) Invoke(call sdkgo.Call, input ListInboxThreadEmailsInput) sdkgo.QueryAttempt[InboxThreadEmailPage] {
	path, ok := inboxThreadEmailsQueryPath(input)
	if !ok {
		return queryFailure[InboxThreadEmailPage]("listInboxThreadEmails", ListInboxThreadEmailsBranchDefect, sdkgo.FailureValidation, "inbox or thread selection is invalid")
	}
	return runQuery(operation.client, call, "listInboxThreadEmails", path, ListInboxThreadEmailsBranchListed, ListInboxThreadEmailsBranchNotFound, ListInboxThreadEmailsBranchProviderRejected, ListInboxThreadEmailsBranchInvalidResponse, ListInboxThreadEmailsBranchDefect, decodeInboxThreadEmailPage)
}

// Invoke retrieves provider GetInboxThreadEmail metadata.
func (operation GetInboxThreadEmailOperation) Invoke(call sdkgo.Call, input GetInboxThreadEmailInput) sdkgo.QueryAttempt[InboxThreadEmail] {
	path, ok := inboxThreadEmailPath(input.InboxID, input.ThreadID, input.EmailID)
	if !ok {
		return queryFailure[InboxThreadEmail]("getInboxThreadEmail", GetInboxThreadEmailBranchDefect, sdkgo.FailureValidation, "inbox or thread selection is invalid")
	}
	return runQuery(operation.client, call, "getInboxThreadEmail", path, GetInboxThreadEmailBranchFound, GetInboxThreadEmailBranchNotFound, GetInboxThreadEmailBranchProviderRejected, GetInboxThreadEmailBranchInvalidResponse, GetInboxThreadEmailBranchDefect, decodeInboxThreadEmail)
}

// Invoke performs UpdateInboxThread.
func (operation UpdateInboxThreadOperation) Invoke(call sdkgo.Call, input UpdateInboxThreadInput) sdkgo.MutationAttempt[InboxThreadReference] {
	path, ok := inboxThreadPath(input.InboxID, input.ThreadID)
	if !ok {
		return mutationFailure[InboxThreadReference]("updateInboxThread", UpdateInboxThreadBranchDefect, sdkgo.FailureValidation, "inbox or thread selection is invalid")
	}
	if input.Read == nil && input.Folder == nil {
		return mutationFailure[InboxThreadReference]("updateInboxThread", UpdateInboxThreadBranchDefect, sdkgo.FailureValidation, "read or folder is required")
	}
	fields := map[string]any{}
	if input.Read != nil {
		fields["read"] = *input.Read
	}
	if input.Folder != nil {
		if !validInboxThreadFolder(*input.Folder, false) {
			return mutationFailure[InboxThreadReference]("updateInboxThread", UpdateInboxThreadBranchDefect, sdkgo.FailureValidation, "folder is invalid")
		}
		fields["folder"] = *input.Folder
	}
	body, _ := json.Marshal(fields)
	return runMutation(operation.client, call, "updateInboxThread", http.MethodPatch, path, body, "", "", UpdateInboxThreadBranchUpdated, UpdateInboxThreadBranchNotFound, UpdateInboxThreadBranchProviderRejected, UpdateInboxThreadBranchInvalidResponse, UpdateInboxThreadBranchDefect, decodeInboxThreadReference)
}

// Invoke performs DeleteInboxThread.
func (operation DeleteInboxThreadOperation) Invoke(call sdkgo.Call, input DeleteInboxThreadInput) sdkgo.MutationAttempt[DeletedInboxThread] {
	path, ok := inboxThreadPath(input.InboxID, input.ThreadID)
	if !ok {
		return mutationFailure[DeletedInboxThread]("deleteInboxThread", DeleteInboxThreadBranchDefect, sdkgo.FailureValidation, "inbox or thread selection is invalid")
	}
	var body []byte
	return runMutation(operation.client, call, "deleteInboxThread", http.MethodDelete, path, body, "", "", DeleteInboxThreadBranchDeleted, DeleteInboxThreadBranchNotFound, DeleteInboxThreadBranchProviderRejected, DeleteInboxThreadBranchInvalidResponse, DeleteInboxThreadBranchDefect, decodeDeletedInboxThread)
}

// Invoke performs AddInboxThreadLabels.
func (operation AddInboxThreadLabelsOperation) Invoke(call sdkgo.Call, input ChangeInboxThreadLabelsInput) sdkgo.MutationAttempt[InboxThreadLabelList] {
	path, ok := inboxThreadLabelsPath(input.InboxID, input.ThreadID)
	if !ok {
		return mutationFailure[InboxThreadLabelList]("addInboxThreadLabels", AddInboxThreadLabelsBranchDefect, sdkgo.FailureValidation, "inbox or thread selection is invalid")
	}
	if len(input.Labels) < 1 || len(input.Labels) > 10 || hasBlankString(input.Labels) {
		return mutationFailure[InboxThreadLabelList]("addInboxThreadLabels", AddInboxThreadLabelsBranchDefect, sdkgo.FailureValidation, "label IDs are invalid")
	}
	body, _ := json.Marshal(map[string]any{"labels": input.Labels})
	return runMutation(operation.client, call, "addInboxThreadLabels", http.MethodPost, path, body, "", "", AddInboxThreadLabelsBranchUpdated, AddInboxThreadLabelsBranchNotFound, AddInboxThreadLabelsBranchProviderRejected, AddInboxThreadLabelsBranchInvalidResponse, AddInboxThreadLabelsBranchDefect, decodeInboxThreadLabelList)
}

// Invoke performs SetInboxThreadLabels.
func (operation SetInboxThreadLabelsOperation) Invoke(call sdkgo.Call, input ChangeInboxThreadLabelsInput) sdkgo.MutationAttempt[InboxThreadLabelList] {
	path, ok := inboxThreadLabelsPath(input.InboxID, input.ThreadID)
	if !ok {
		return mutationFailure[InboxThreadLabelList]("setInboxThreadLabels", SetInboxThreadLabelsBranchDefect, sdkgo.FailureValidation, "inbox or thread selection is invalid")
	}
	if len(input.Labels) < 0 || len(input.Labels) > 100 || hasBlankString(input.Labels) {
		return mutationFailure[InboxThreadLabelList]("setInboxThreadLabels", SetInboxThreadLabelsBranchDefect, sdkgo.FailureValidation, "label IDs are invalid")
	}
	body, _ := json.Marshal(map[string]any{"labels": input.Labels})
	return runMutation(operation.client, call, "setInboxThreadLabels", http.MethodPut, path, body, "", "", SetInboxThreadLabelsBranchUpdated, SetInboxThreadLabelsBranchNotFound, SetInboxThreadLabelsBranchProviderRejected, SetInboxThreadLabelsBranchInvalidResponse, SetInboxThreadLabelsBranchDefect, decodeInboxThreadLabelList)
}

// Invoke performs RemoveInboxThreadLabel.
func (operation RemoveInboxThreadLabelOperation) Invoke(call sdkgo.Call, input RemoveInboxThreadLabelInput) sdkgo.MutationAttempt[InboxThreadLabelList] {
	path, ok := inboxThreadLabelPath(input.InboxID, input.ThreadID, input.LabelID)
	if !ok {
		return mutationFailure[InboxThreadLabelList]("removeInboxThreadLabel", RemoveInboxThreadLabelBranchDefect, sdkgo.FailureValidation, "inbox or thread selection is invalid")
	}
	var body []byte
	return runMutation(operation.client, call, "removeInboxThreadLabel", http.MethodDelete, path, body, "", "", RemoveInboxThreadLabelBranchUpdated, RemoveInboxThreadLabelBranchNotFound, RemoveInboxThreadLabelBranchProviderRejected, RemoveInboxThreadLabelBranchInvalidResponse, RemoveInboxThreadLabelBranchDefect, decodeInboxThreadLabelList)
}

// Invoke performs ReplyInboxThreadEmail.
func (operation ReplyInboxThreadEmailOperation) Invoke(call sdkgo.Call, input ReplyInboxThreadEmailInput) sdkgo.MutationAttempt[SentInboxThreadEmail] {
	path, ok := inboxThreadEmailPath(input.InboxID, input.ThreadID, input.EmailID)
	if !ok {
		return mutationFailure[SentInboxThreadEmail]("replyInboxThreadEmail", ReplyInboxThreadEmailBranchDefect, sdkgo.FailureValidation, "inbox or thread selection is invalid")
	}
	if strings.TrimSpace(input.Html) == "" && strings.TrimSpace(input.Text) == "" {
		return mutationFailure[SentInboxThreadEmail]("replyInboxThreadEmail", ReplyInboxThreadEmailBranchDefect, sdkgo.FailureValidation, "HTML or text is required")
	}
	fields := map[string]any{}
	if input.Html != "" {
		fields["html"] = input.Html
	}
	if input.Text != "" {
		fields["text"] = input.Text
	}
	if input.Subject != "" {
		fields["subject"] = input.Subject
	}
	if input.ReplyAll {
		fields["reply_all"] = true
	}
	if len(input.Cc) > 0 {
		fields["cc"] = input.Cc
	}
	if len(input.Bcc) > 0 {
		fields["bcc"] = input.Bcc
	}
	body, _ := json.Marshal(fields)
	path += "/reply"
	return runMutation(operation.client, call, "replyInboxThreadEmail", http.MethodPost, path, body, "Idempotency-Key", string(call.IdempotencyKey), ReplyInboxThreadEmailBranchSent, ReplyInboxThreadEmailBranchNotFound, ReplyInboxThreadEmailBranchProviderRejected, ReplyInboxThreadEmailBranchInvalidResponse, ReplyInboxThreadEmailBranchDefect, decodeSentInboxThreadEmail)
}

// Invoke performs ForwardInboxThreadEmail.
func (operation ForwardInboxThreadEmailOperation) Invoke(call sdkgo.Call, input ForwardInboxThreadEmailInput) sdkgo.MutationAttempt[SentInboxThreadEmail] {
	path, ok := inboxThreadEmailPath(input.InboxID, input.ThreadID, input.EmailID)
	if !ok {
		return mutationFailure[SentInboxThreadEmail]("forwardInboxThreadEmail", ForwardInboxThreadEmailBranchDefect, sdkgo.FailureValidation, "inbox or thread selection is invalid")
	}
	if len(input.To) == 0 || hasBlankString(input.To) {
		return mutationFailure[SentInboxThreadEmail]("forwardInboxThreadEmail", ForwardInboxThreadEmailBranchDefect, sdkgo.FailureValidation, "recipient is required")
	}
	fields := map[string]any{"to": input.To}
	if input.Html != "" {
		fields["html"] = input.Html
	}
	if input.Text != "" {
		fields["text"] = input.Text
	}
	if input.Subject != "" {
		fields["subject"] = input.Subject
	}
	if len(input.Cc) > 0 {
		fields["cc"] = input.Cc
	}
	if len(input.Bcc) > 0 {
		fields["bcc"] = input.Bcc
	}
	body, _ := json.Marshal(fields)
	path += "/forward"
	return runMutation(operation.client, call, "forwardInboxThreadEmail", http.MethodPost, path, body, "Idempotency-Key", string(call.IdempotencyKey), ForwardInboxThreadEmailBranchSent, ForwardInboxThreadEmailBranchNotFound, ForwardInboxThreadEmailBranchProviderRejected, ForwardInboxThreadEmailBranchInvalidResponse, ForwardInboxThreadEmailBranchDefect, decodeSentInboxThreadEmail)
}
func inboxThreadsPath(inboxID string) (string, bool) {
	if strings.TrimSpace(inboxID) == "" {
		return "", false
	}
	return "/inboxes/" + url.PathEscape(inboxID) + "/threads", true
}
func inboxThreadPath(inboxID, threadID string) (string, bool) {
	path, ok := inboxThreadsPath(inboxID)
	if !ok || strings.TrimSpace(threadID) == "" {
		return "", false
	}
	return path + "/" + url.PathEscape(threadID), true
}
func inboxThreadLabelsPath(inboxID, threadID string) (string, bool) {
	path, ok := inboxThreadPath(inboxID, threadID)
	return path + "/labels", ok
}
func inboxThreadLabelPath(inboxID, threadID, labelID string) (string, bool) {
	path, ok := inboxThreadLabelsPath(inboxID, threadID)
	if !ok || strings.TrimSpace(labelID) == "" {
		return "", false
	}
	return path + "/" + url.PathEscape(labelID), true
}
func inboxThreadEmailPath(inboxID, threadID, emailID string) (string, bool) {
	path, ok := inboxThreadPath(inboxID, threadID)
	if !ok || strings.TrimSpace(emailID) == "" {
		return "", false
	}
	return path + "/emails/" + url.PathEscape(emailID), true
}
func inboxThreadEmailsQueryPath(input ListInboxThreadEmailsInput) (string, bool) {
	path, ok := inboxThreadPath(input.InboxID, input.ThreadID)
	if !ok {
		return "", false
	}
	values, ok := threadPaginationValues(input.Limit, input.After, input.Before)
	if !ok {
		return "", false
	}
	return appendQuery(path+"/emails", values), true
}
func inboxThreadsQueryPath(input ListInboxThreadsInput) (string, bool) {
	path, ok := inboxThreadsPath(input.InboxID)
	if !ok {
		return "", false
	}
	values, ok := threadPaginationValues(input.Limit, input.After, input.Before)
	if !ok || len(input.Labels) > 50 || hasBlankString(input.Labels) {
		return "", false
	}
	for _, folder := range input.Folders {
		if !validInboxThreadFolder(folder, true) {
			return "", false
		}
	}
	if len(input.Folders) > 0 {
		values.Set("folders", strings.Join(input.Folders, ","))
	}
	if len(input.Labels) > 0 {
		values.Set("labels", strings.Join(input.Labels, ","))
	}
	if input.Read != nil {
		values.Set("read", strconv.FormatBool(*input.Read))
	}
	return appendQuery(path, values), true
}
func inboxThreadsSearchPath(input SearchInboxThreadsInput) (string, bool) {
	path, ok := inboxThreadsQueryPath(ListInboxThreadsInput{InboxID: input.InboxID, Folders: input.Folders, Labels: input.Labels, Read: input.Read, Limit: input.Limit, After: input.After, Before: input.Before})
	if !ok || len(input.Query) > 256 {
		return "", false
	}
	parsed, err := url.Parse(path)
	if err != nil {
		return "", false
	}
	values := parsed.Query()
	if input.Query != "" {
		values.Set("query", input.Query)
	}
	for key, items := range map[string][]string{"from": input.From, "to": input.To, "cc": input.Cc, "bcc": input.Bcc} {
		if len(items) > 20 || hasBlankString(items) {
			return "", false
		}
		if len(items) > 0 {
			values.Set(key, strings.Join(items, ","))
		}
	}
	if input.HasAttachment != nil {
		values.Set("has_attachment", strconv.FormatBool(*input.HasAttachment))
	}
	if input.StartDate != "" {
		values.Set("start_date", input.StartDate)
	}
	if input.EndDate != "" {
		values.Set("end_date", input.EndDate)
	}
	parsed.Path += "/search"
	parsed.RawQuery = values.Encode()
	return parsed.String(), true
}
func threadPaginationValues(limit int, after, before string) (url.Values, bool) {
	if limit < 0 || limit > 100 || (after != "" && before != "") {
		return nil, false
	}
	values := url.Values{}
	if limit > 0 {
		values.Set("limit", strconv.Itoa(limit))
	}
	if after != "" {
		values.Set("after", after)
	}
	if before != "" {
		values.Set("before", before)
	}
	return values, true
}
func appendQuery(path string, values url.Values) string {
	if len(values) == 0 {
		return path
	}
	return path + "?" + values.Encode()
}
func validInboxThreadFolder(folder string, allowSent bool) bool {
	switch folder {
	case "inbox", "archive", "spam", "trash":
		return true
	case "sent":
		return allowSent
	}
	return false
}
func hasBlankString(values []string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return true
		}
	}
	return false
}
func decodeInboxThreadPage(body []byte) (InboxThreadPage, error) {
	var value InboxThreadPage
	if err := json.Unmarshal(body, &value); err != nil || value.Data == nil {
		return InboxThreadPage{}, fmt.Errorf("invalid thread page")
	}
	return value, nil
}
func decodeInboxThreadReference(body []byte) (InboxThreadReference, error) {
	var value InboxThreadReference
	if err := json.Unmarshal(body, &value); err != nil || value.ID == "" {
		return InboxThreadReference{}, fmt.Errorf("invalid thread reference")
	}
	return value, nil
}
func decodeDeletedInboxThread(body []byte) (DeletedInboxThread, error) {
	var value DeletedInboxThread
	if err := json.Unmarshal(body, &value); err != nil || value.ID == "" {
		return DeletedInboxThread{}, fmt.Errorf("invalid removed thread")
	}
	return value, nil
}
func decodeInboxThreadLabelList(body []byte) (InboxThreadLabelList, error) {
	var value InboxThreadLabelList
	if err := json.Unmarshal(body, &value); err != nil || value.Data == nil {
		return InboxThreadLabelList{}, fmt.Errorf("invalid thread labels")
	}
	return value, nil
}
func decodeInboxThreadEmailPage(body []byte) (InboxThreadEmailPage, error) {
	var value InboxThreadEmailPage
	if err := json.Unmarshal(body, &value); err != nil || value.Data == nil {
		return InboxThreadEmailPage{}, fmt.Errorf("invalid thread email page")
	}
	return value, nil
}
func decodeInboxThreadEmail(body []byte) (InboxThreadEmail, error) {
	var value InboxThreadEmail
	if err := json.Unmarshal(body, &value); err != nil || value.ID == "" {
		return InboxThreadEmail{}, fmt.Errorf("invalid thread email")
	}
	return value, nil
}
func decodeSentInboxThreadEmail(body []byte) (SentInboxThreadEmail, error) {
	var value SentInboxThreadEmail
	if err := json.Unmarshal(body, &value); err != nil || value.ID == "" {
		return SentInboxThreadEmail{}, fmt.Errorf("invalid sent thread email")
	}
	return value, nil
}
