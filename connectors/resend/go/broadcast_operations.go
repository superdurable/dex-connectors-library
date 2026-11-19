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
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// CreateBroadcastInput creates a draft broadcast; delivery uses SendBroadcast.
type CreateBroadcastInput struct {
	// Name is the draft name used to confirm an uncertain create.
	Name string `json:"name"`
	// SegmentID is the target segment ID.
	SegmentID string `json:"segmentId"`
	// From is a verified sender address.
	From string `json:"from"`
	// Subject is the email subject.
	Subject string `json:"subject"`
	// ReplyTo contains optional reply addresses.
	ReplyTo []string `json:"replyTo,omitempty"`
	// PreviewText is optional message preview text.
	PreviewText string `json:"previewText,omitempty"`
	// HTML is an optional HTML body; at least one body format is required.
	HTML string `json:"html,omitempty"`
	// Text is an optional plain-text body; at least one body format is required.
	Text string `json:"text,omitempty"`
	// TopicID optionally scopes the broadcast to a topic.
	TopicID string `json:"topicId,omitempty"`
}

// GetBroadcastInput selects a broadcast.
type GetBroadcastInput struct {
	// ID is the broadcast ID.
	ID string `json:"id"`
}

// ListBroadcastsInput selects a broadcast page.
type ListBroadcastsInput struct {
	// Limit is the page size, from 1 through 100; zero uses Resend's default.
	Limit int `json:"limit,omitempty"`
	// After is the exclusive broadcast ID cursor.
	After string `json:"after,omitempty"`
}

// UpdateBroadcastInput sets selected absolute draft fields.
type UpdateBroadcastInput struct {
	// ID is the draft broadcast ID.
	ID string `json:"id"`
	// Name replaces the draft name when non-nil.
	Name *string `json:"name,omitempty"`
	// SegmentID replaces the target segment when non-nil.
	SegmentID *string `json:"segmentId,omitempty"`
	// From replaces the verified sender when non-nil.
	From *string `json:"from,omitempty"`
	// Subject replaces the subject when non-nil.
	Subject *string `json:"subject,omitempty"`
	// ReplyTo replaces reply addresses when non-nil; an empty slice clears them.
	ReplyTo *[]string `json:"replyTo,omitempty"`
	// PreviewText replaces preview text when non-nil; blank clears it.
	PreviewText *string `json:"previewText,omitempty"`
	// HTML replaces the HTML body when non-nil; blank clears it.
	HTML *string `json:"html,omitempty"`
	// Text replaces the plain-text body when non-nil; blank clears it.
	Text *string `json:"text,omitempty"`
	// TopicID replaces the topic scope when non-nil; blank clears it.
	TopicID *string `json:"topicId,omitempty"`
}

// DeleteBroadcastInput selects a draft broadcast for deletion.
type DeleteBroadcastInput struct {
	// ID is the draft broadcast ID.
	ID string `json:"id"`
}

// SendBroadcastInput sends or schedules a draft broadcast.
type SendBroadcastInput struct {
	// ID is the draft broadcast ID.
	ID string `json:"id"`
	// ScheduledAt is an optional RFC 3339 delivery time; blank sends now.
	ScheduledAt string `json:"scheduledAt,omitempty"`
}

// CancelBroadcastInput selects a scheduled or queued broadcast.
type CancelBroadcastInput struct {
	// ID is the broadcast ID.
	ID string `json:"id"`
}

// DuplicateBroadcastInput selects a broadcast to copy into a new draft.
type DuplicateBroadcastInput struct {
	// ID is the source broadcast ID.
	ID string `json:"id"`
}

// ListBroadcastRecipientsInput filters recipients by event.
type ListBroadcastRecipientsInput struct {
	// ID is the broadcast ID.
	ID string `json:"id"`
	// Type is sent, delivered, opened, clicked, bounced, complained, unsubscribed, or suppressed.
	Type string `json:"type"`
	// Email optionally filters by an address substring.
	Email string `json:"email,omitempty"`
	// BounceType optionally filters bounced events by permanent, transient, or undetermined.
	BounceType string `json:"bounceType,omitempty"`
	// Limit is the page size, from 1 through 100; zero uses Resend's default.
	Limit int `json:"limit,omitempty"`
	// After is the exclusive opaque recipient cursor.
	After string `json:"after,omitempty"`
}

// ListBroadcastClickedLinksInput selects a clicked-link page.
type ListBroadcastClickedLinksInput struct {
	// ID is the broadcast ID.
	ID string `json:"id"`
	// Limit is the page size, from 1 through 100; zero uses Resend's default.
	Limit int `json:"limit,omitempty"`
	// After is the exclusive opaque clicked-link cursor.
	After string `json:"after,omitempty"`
}

// BroadcastReference identifies a changed broadcast.
type BroadcastReference struct {
	// ID is the stable provider broadcast ID.
	ID string `json:"id"`
}

// Broadcast contains safe broadcast metadata without message content.
type Broadcast struct {
	// ID is the broadcast ID.
	ID string `json:"id"`
	// Name is the draft name.
	Name string `json:"name"`
	// SegmentID is the target segment ID.
	SegmentID string `json:"segment_id"`
	// From is the verified sender.
	From string `json:"from"`
	// Subject is the email subject.
	Subject string `json:"subject"`
	// Status is draft, scheduled, queued, sent, or canceled.
	Status string `json:"status"`
	// TopicID is the optional topic scope.
	TopicID string `json:"topic_id"`
	// CreatedAt is the creation timestamp.
	CreatedAt string `json:"created_at"`
	// ScheduledAt is the optional scheduled delivery timestamp.
	ScheduledAt string `json:"scheduled_at"`
	// SentAt is the optional send timestamp.
	SentAt string `json:"sent_at"`
}

// BroadcastPage contains one broadcast page.
type BroadcastPage struct {
	// Data contains broadcast summaries.
	Data []Broadcast `json:"data"`
	// HasMore reports whether another page exists.
	HasMore bool `json:"has_more"`
}

// DeletedBroadcast records a draft deletion acknowledgement.
type DeletedBroadcast struct {
	// ID is the deleted broadcast ID.
	ID string `json:"id"`
	// Deleted confirms that deletion succeeded.
	Deleted bool `json:"deleted"`
}

// BroadcastRecipient contains a recipient event summary.
type BroadcastRecipient struct {
	// ID is an opaque pagination cursor.
	ID string `json:"id"`
	// ContactID is the associated contact ID when available.
	ContactID string `json:"contact_id"`
	// Email is the recipient address.
	Email string `json:"email"`
	// Count is the number of matching events.
	Count int `json:"count"`
	// BounceType classifies a bounced recipient.
	BounceType string `json:"bounce_type"`
}

// BroadcastRecipientPage contains one recipient page.
type BroadcastRecipientPage struct {
	// Data contains recipient event summaries.
	Data []BroadcastRecipient `json:"data"`
	// HasMore reports whether another page exists.
	HasMore bool `json:"has_more"`
}

// BroadcastClickedLink counts clicks on one URL.
type BroadcastClickedLink struct {
	// ID is an opaque pagination cursor.
	ID string `json:"id"`
	// URL is the clicked destination.
	URL string `json:"url"`
	// Clicks is the total click count.
	Clicks int `json:"clicks"`
	// UniqueClicks is the distinct recipient click count.
	UniqueClicks int `json:"unique_clicks"`
}

// BroadcastClickedLinkPage contains one clicked-link page.
type BroadcastClickedLinkPage struct {
	// Data contains clicked-link counts.
	Data []BroadcastClickedLink `json:"data"`
	// HasMore reports whether another page exists.
	HasMore bool `json:"has_more"`
}

// CreateBroadcastOperation creates a draft with bounded resend.
type CreateBroadcastOperation struct{ client *Client }

// GetBroadcastOperation retrieves one broadcast.
type GetBroadcastOperation struct{ client *Client }

// ListBroadcastsOperation lists broadcasts.
type ListBroadcastsOperation struct{ client *Client }

// UpdateBroadcastOperation changes a draft.
type UpdateBroadcastOperation struct{ client *Client }

// DeleteBroadcastOperation removes a draft.
type DeleteBroadcastOperation struct{ client *Client }

// SendBroadcastOperation starts or schedules delivery.
type SendBroadcastOperation struct{ client *Client }

// CancelBroadcastOperation stops a queued or scheduled delivery.
type CancelBroadcastOperation struct{ client *Client }

// DuplicateBroadcastOperation creates a draft copy.
type DuplicateBroadcastOperation struct{ client *Client }

// ListBroadcastRecipientsOperation lists recipient events.
type ListBroadcastRecipientsOperation struct{ client *Client }

// ListBroadcastClickedLinksOperation lists clicked URLs.
type ListBroadcastClickedLinksOperation struct{ client *Client }

// CreateBroadcast binds draft creation to this client.
func (client *Client) CreateBroadcast() CreateBroadcastOperation {
	return CreateBroadcastOperation{client}
}

// GetBroadcast binds broadcast retrieval to this client.
func (client *Client) GetBroadcast() GetBroadcastOperation { return GetBroadcastOperation{client} }

// ListBroadcasts binds broadcast listing to this client.
func (client *Client) ListBroadcasts() ListBroadcastsOperation {
	return ListBroadcastsOperation{client}
}

// UpdateBroadcast binds draft changes to this client.
func (client *Client) UpdateBroadcast() UpdateBroadcastOperation {
	return UpdateBroadcastOperation{client}
}

// DeleteBroadcast binds draft deletion to this client.
func (client *Client) DeleteBroadcast() DeleteBroadcastOperation {
	return DeleteBroadcastOperation{client}
}

// SendBroadcast binds delivery to this client.
func (client *Client) SendBroadcast() SendBroadcastOperation { return SendBroadcastOperation{client} }

// CancelBroadcast binds cancellation to this client.
func (client *Client) CancelBroadcast() CancelBroadcastOperation {
	return CancelBroadcastOperation{client}
}

// DuplicateBroadcast binds duplication to this client.
func (client *Client) DuplicateBroadcast() DuplicateBroadcastOperation {
	return DuplicateBroadcastOperation{client}
}

// ListBroadcastRecipients binds recipient lookup to this client.
func (client *Client) ListBroadcastRecipients() ListBroadcastRecipientsOperation {
	return ListBroadcastRecipientsOperation{client}
}

// ListBroadcastClickedLinks binds clicked-link lookup to this client.
func (client *Client) ListBroadcastClickedLinks() ListBroadcastClickedLinksOperation {
	return ListBroadcastClickedLinksOperation{client}
}

// Definition returns the generated createBroadcast contract.
func (CreateBroadcastOperation) Definition() sdkgo.MutationDefinition {
	return CreateBroadcastDefinition
}

// Definition returns the generated getBroadcast contract.
func (GetBroadcastOperation) Definition() sdkgo.QueryDefinition { return GetBroadcastDefinition }

// Definition returns the generated listBroadcasts contract.
func (ListBroadcastsOperation) Definition() sdkgo.QueryDefinition { return ListBroadcastsDefinition }

// Definition returns the generated updateBroadcast contract.
func (UpdateBroadcastOperation) Definition() sdkgo.MutationDefinition {
	return UpdateBroadcastDefinition
}

// Definition returns the generated deleteBroadcast contract.
func (DeleteBroadcastOperation) Definition() sdkgo.MutationDefinition {
	return DeleteBroadcastDefinition
}

// Definition returns the generated sendBroadcast contract.
func (SendBroadcastOperation) Definition() sdkgo.MutationDefinition { return SendBroadcastDefinition }

// Definition returns the generated cancelBroadcast contract.
func (CancelBroadcastOperation) Definition() sdkgo.MutationDefinition {
	return CancelBroadcastDefinition
}

// Definition returns the generated duplicateBroadcast contract.
func (DuplicateBroadcastOperation) Definition() sdkgo.MutationDefinition {
	return DuplicateBroadcastDefinition
}

// Definition returns the generated listBroadcastRecipients contract.
func (ListBroadcastRecipientsOperation) Definition() sdkgo.QueryDefinition {
	return ListBroadcastRecipientsDefinition
}

// Definition returns the generated listBroadcastClickedLinks contract.
func (ListBroadcastClickedLinksOperation) Definition() sdkgo.QueryDefinition {
	return ListBroadcastClickedLinksDefinition
}

// IdempotencyKey correlates possible draft creation attempts.
func (CreateBroadcastOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateBroadcastInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates absolute draft changes.
func (UpdateBroadcastOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateBroadcastInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates harmless repeated deletion.
func (DeleteBroadcastOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteBroadcastInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates delivery requests for one broadcast ID.
func (SendBroadcastOperation) IdempotencyKey(callID sdkgo.CallID, _ SendBroadcastInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates cancellation requests for one broadcast ID.
func (CancelBroadcastOperation) IdempotencyKey(callID sdkgo.CallID, _ CancelBroadcastInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates possible draft duplication attempts.
func (DuplicateBroadcastOperation) IdempotencyKey(callID sdkgo.CallID, _ DuplicateBroadcastInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a draft broadcast without sending it.
func (operation CreateBroadcastOperation) Invoke(call sdkgo.Call, input CreateBroadcastInput) sdkgo.MutationAttempt[BroadcastReference] {
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.SegmentID) == "" || strings.TrimSpace(input.From) == "" || strings.TrimSpace(input.Subject) == "" || (input.HTML == "" && input.Text == "") {
		return mutationFailure[BroadcastReference]("createBroadcast", CreateBroadcastBranchDefect, sdkgo.FailureValidation, "name, segment, sender, subject, and message body are required")
	}
	fields := map[string]any{"name": input.Name, "segment_id": input.SegmentID, "from": input.From, "subject": input.Subject, "send": false}
	if len(input.ReplyTo) > 0 {
		fields["reply_to"] = input.ReplyTo
	}
	if input.PreviewText != "" {
		fields["preview_text"] = input.PreviewText
	}
	if input.HTML != "" {
		fields["html"] = input.HTML
	}
	if input.Text != "" {
		fields["text"] = input.Text
	}
	if input.TopicID != "" {
		fields["topic_id"] = input.TopicID
	}
	body, err := json.Marshal(fields)
	if err != nil {
		return mutationFailure[BroadcastReference]("createBroadcast", CreateBroadcastBranchDefect, sdkgo.FailureValidation, "broadcast cannot be encoded")
	}
	request := resendLimitedCreate[BroadcastReference]{client: operation.client, call: call, operation: "createBroadcast", path: "/broadcasts", body: body, timeout: CreateBroadcastDefinition.StepDefaults.ExecuteMethodTimeout, created: CreateBroadcastBranchCreated, rejected: CreateBroadcastBranchProviderRejected, defect: CreateBroadcastBranchDefect, decode: decodeBroadcastReference, readBack: func(call sdkgo.Call) (BroadcastReference, readBackState) {
		return operation.readBackBroadcast(call, input)
	}}
	return request.invoke()
}

// Invoke retrieves one broadcast's metadata.
func (operation GetBroadcastOperation) Invoke(call sdkgo.Call, input GetBroadcastInput) sdkgo.QueryAttempt[Broadcast] {
	if strings.TrimSpace(input.ID) == "" {
		return queryFailure[Broadcast]("getBroadcast", GetBroadcastBranchDefect, sdkgo.FailureValidation, "broadcast ID is required")
	}
	return runQuery(operation.client, call, "getBroadcast", "/broadcasts/"+url.PathEscape(input.ID), GetBroadcastBranchFound, GetBroadcastBranchNotFound, GetBroadcastBranchProviderRejected, GetBroadcastBranchInvalidResponse, GetBroadcastBranchDefect, decodeBroadcast)
}

// Invoke lists one broadcast page.
func (operation ListBroadcastsOperation) Invoke(call sdkgo.Call, input ListBroadcastsInput) sdkgo.QueryAttempt[BroadcastPage] {
	query, err := listQuery(input.Limit, input.After, false)
	if err != nil {
		return queryFailure[BroadcastPage]("listBroadcasts", ListBroadcastsBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	return runQuery(operation.client, call, "listBroadcasts", "/broadcasts"+query, ListBroadcastsBranchListed, "", ListBroadcastsBranchProviderRejected, ListBroadcastsBranchInvalidResponse, ListBroadcastsBranchDefect, decodeBroadcastPage)
}

// Invoke changes selected absolute draft fields.
func (operation UpdateBroadcastOperation) Invoke(call sdkgo.Call, input UpdateBroadcastInput) sdkgo.MutationAttempt[BroadcastReference] {
	if strings.TrimSpace(input.ID) == "" || (input.Name == nil && input.SegmentID == nil && input.From == nil && input.Subject == nil && input.ReplyTo == nil && input.PreviewText == nil && input.HTML == nil && input.Text == nil && input.TopicID == nil) {
		return mutationFailure[BroadcastReference]("updateBroadcast", UpdateBroadcastBranchDefect, sdkgo.FailureValidation, "broadcast ID and at least one field are required")
	}
	for _, value := range []*string{input.Name, input.SegmentID, input.From, input.Subject} {
		if value != nil && strings.TrimSpace(*value) == "" {
			return mutationFailure[BroadcastReference]("updateBroadcast", UpdateBroadcastBranchDefect, sdkgo.FailureValidation, "required broadcast fields cannot be blank")
		}
	}
	fields := map[string]any{}
	if input.Name != nil {
		fields["name"] = *input.Name
	}
	if input.SegmentID != nil {
		fields["segment_id"] = *input.SegmentID
	}
	if input.From != nil {
		fields["from"] = *input.From
	}
	if input.Subject != nil {
		fields["subject"] = *input.Subject
	}
	if input.ReplyTo != nil {
		fields["reply_to"] = *input.ReplyTo
	}
	if input.PreviewText != nil {
		fields["preview_text"] = *input.PreviewText
	}
	if input.HTML != nil {
		fields["html"] = *input.HTML
	}
	if input.Text != nil {
		fields["text"] = *input.Text
	}
	if input.TopicID != nil {
		fields["topic_id"] = *input.TopicID
	}
	body, err := json.Marshal(fields)
	if err != nil {
		return mutationFailure[BroadcastReference]("updateBroadcast", UpdateBroadcastBranchDefect, sdkgo.FailureValidation, "broadcast cannot be encoded")
	}
	return runMutation(operation.client, call, "updateBroadcast", http.MethodPatch, "/broadcasts/"+url.PathEscape(input.ID), body, "", "", UpdateBroadcastBranchUpdated, UpdateBroadcastBranchNotFound, UpdateBroadcastBranchProviderRejected, UpdateBroadcastBranchInvalidResponse, UpdateBroadcastBranchDefect, decodeBroadcastReference)
}

// Invoke deletes a draft broadcast.
func (operation DeleteBroadcastOperation) Invoke(call sdkgo.Call, input DeleteBroadcastInput) sdkgo.MutationAttempt[DeletedBroadcast] {
	if strings.TrimSpace(input.ID) == "" {
		return mutationFailure[DeletedBroadcast]("deleteBroadcast", DeleteBroadcastBranchDefect, sdkgo.FailureValidation, "broadcast ID is required")
	}
	return runMutation(operation.client, call, "deleteBroadcast", http.MethodDelete, "/broadcasts/"+url.PathEscape(input.ID), nil, "", "", DeleteBroadcastBranchDeleted, DeleteBroadcastBranchNotFound, DeleteBroadcastBranchProviderRejected, DeleteBroadcastBranchInvalidResponse, DeleteBroadcastBranchDefect, decodeDeletedBroadcast)
}

// Invoke sends or schedules one existing broadcast.
func (operation SendBroadcastOperation) Invoke(call sdkgo.Call, input SendBroadcastInput) sdkgo.MutationAttempt[BroadcastReference] {
	if strings.TrimSpace(input.ID) == "" {
		return mutationFailure[BroadcastReference]("sendBroadcast", SendBroadcastBranchDefect, sdkgo.FailureValidation, "broadcast ID is required")
	}
	var body []byte
	if input.ScheduledAt != "" {
		if _, err := time.Parse(time.RFC3339Nano, input.ScheduledAt); err != nil {
			return mutationFailure[BroadcastReference]("sendBroadcast", SendBroadcastBranchDefect, sdkgo.FailureValidation, "scheduledAt must be RFC 3339")
		}
		body, _ = json.Marshal(map[string]string{"scheduled_at": input.ScheduledAt})
	}
	return runMutation(operation.client, call, "sendBroadcast", http.MethodPost, "/broadcasts/"+url.PathEscape(input.ID)+"/send", body, "", "", SendBroadcastBranchSent, SendBroadcastBranchNotFound, SendBroadcastBranchProviderRejected, SendBroadcastBranchInvalidResponse, SendBroadcastBranchDefect, decodeBroadcastReference)
}

// Invoke cancels a scheduled or queued broadcast.
func (operation CancelBroadcastOperation) Invoke(call sdkgo.Call, input CancelBroadcastInput) sdkgo.MutationAttempt[BroadcastReference] {
	if strings.TrimSpace(input.ID) == "" {
		return mutationFailure[BroadcastReference]("cancelBroadcast", CancelBroadcastBranchDefect, sdkgo.FailureValidation, "broadcast ID is required")
	}
	return runMutation(operation.client, call, "cancelBroadcast", http.MethodPost, "/broadcasts/"+url.PathEscape(input.ID)+"/cancel", nil, "", "", CancelBroadcastBranchCancelled, CancelBroadcastBranchNotFound, CancelBroadcastBranchProviderRejected, CancelBroadcastBranchInvalidResponse, CancelBroadcastBranchDefect, decodeBroadcastReference)
}

// Invoke duplicates a broadcast into a new draft with bounded resend.
func (operation DuplicateBroadcastOperation) Invoke(call sdkgo.Call, input DuplicateBroadcastInput) sdkgo.MutationAttempt[BroadcastReference] {
	if strings.TrimSpace(input.ID) == "" {
		return mutationFailure[BroadcastReference]("duplicateBroadcast", DuplicateBroadcastBranchDefect, sdkgo.FailureValidation, "source broadcast ID is required")
	}
	request := resendLimitedCreate[BroadcastReference]{client: operation.client, call: call, operation: "duplicateBroadcast", path: "/broadcasts/" + url.PathEscape(input.ID) + "/duplicate", timeout: DuplicateBroadcastDefinition.StepDefaults.ExecuteMethodTimeout, created: DuplicateBroadcastBranchDuplicated, rejected: DuplicateBroadcastBranchProviderRejected, defect: DuplicateBroadcastBranchDefect, decode: decodeBroadcastReference, readBack: func(sdkgo.Call) (BroadcastReference, readBackState) { return BroadcastReference{}, readBackAbsent }}
	return request.invoke()
}

// Invoke lists recipients matching one broadcast event.
func (operation ListBroadcastRecipientsOperation) Invoke(call sdkgo.Call, input ListBroadcastRecipientsInput) sdkgo.QueryAttempt[BroadcastRecipientPage] {
	if strings.TrimSpace(input.ID) == "" || !oneOf(input.Type, "sent", "delivered", "opened", "clicked", "bounced", "complained", "unsubscribed", "suppressed") || input.BounceType != "" && (input.Type != "bounced" || !oneOf(input.BounceType, "permanent", "transient", "undetermined")) {
		return queryFailure[BroadcastRecipientPage]("listBroadcastRecipients", ListBroadcastRecipientsBranchDefect, sdkgo.FailureValidation, "broadcast ID, event type, or bounce filter is invalid")
	}
	query, err := listQuery(input.Limit, input.After, false)
	if err != nil {
		return queryFailure[BroadcastRecipientPage]("listBroadcastRecipients", ListBroadcastRecipientsBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	values := url.Values{}
	values.Set("type", input.Type)
	if input.Email != "" {
		values.Set("email", input.Email)
	}
	if input.BounceType != "" {
		values.Set("bounce_type", input.BounceType)
	}
	if query != "" {
		extra, err := url.ParseQuery(strings.TrimPrefix(query, "?"))
		if err != nil {
			return queryFailure[BroadcastRecipientPage]("listBroadcastRecipients", ListBroadcastRecipientsBranchDefect, sdkgo.FailureValidation, "pagination is invalid")
		}
		for key, items := range extra {
			values[key] = items
		}
	}
	return runQuery(operation.client, call, "listBroadcastRecipients", "/broadcasts/"+url.PathEscape(input.ID)+"/recipients?"+values.Encode(), ListBroadcastRecipientsBranchListed, "", ListBroadcastRecipientsBranchProviderRejected, ListBroadcastRecipientsBranchInvalidResponse, ListBroadcastRecipientsBranchDefect, decodeBroadcastRecipientPage)
}

// Invoke lists clicked URLs for one broadcast.
func (operation ListBroadcastClickedLinksOperation) Invoke(call sdkgo.Call, input ListBroadcastClickedLinksInput) sdkgo.QueryAttempt[BroadcastClickedLinkPage] {
	if strings.TrimSpace(input.ID) == "" {
		return queryFailure[BroadcastClickedLinkPage]("listBroadcastClickedLinks", ListBroadcastClickedLinksBranchDefect, sdkgo.FailureValidation, "broadcast ID is required")
	}
	query, err := listQuery(input.Limit, input.After, false)
	if err != nil {
		return queryFailure[BroadcastClickedLinkPage]("listBroadcastClickedLinks", ListBroadcastClickedLinksBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	return runQuery(operation.client, call, "listBroadcastClickedLinks", "/broadcasts/"+url.PathEscape(input.ID)+"/clicked-links"+query, ListBroadcastClickedLinksBranchListed, "", ListBroadcastClickedLinksBranchProviderRejected, ListBroadcastClickedLinksBranchInvalidResponse, ListBroadcastClickedLinksBranchDefect, decodeBroadcastClickedLinkPage)
}

func (operation CreateBroadcastOperation) readBackBroadcast(call sdkgo.Call, input CreateBroadcastInput) (BroadcastReference, readBackState) {
	cursor := ""
	for pageNumber := 0; pageNumber < 100; pageNumber++ {
		query, _ := listQuery(100, cursor, false)
		body, state := operation.client.readBackResource(call, "/broadcasts"+query)
		if state != readBackMatched {
			return BroadcastReference{}, readBackRetry
		}
		page, err := decodeBroadcastPage(body)
		if err != nil {
			return BroadcastReference{}, readBackRetry
		}
		for _, broadcast := range page.Data {
			if broadcast.Name == input.Name {
				detailBody, detailState := operation.client.readBackResource(call, "/broadcasts/"+url.PathEscape(broadcast.ID))
				if detailState != readBackMatched {
					return BroadcastReference{}, readBackRetry
				}
				var detail struct {
					Broadcast
					HTML        string   `json:"html"`
					Text        string   `json:"text"`
					ReplyTo     []string `json:"reply_to"`
					PreviewText string   `json:"preview_text"`
				}
				if err := json.Unmarshal(detailBody, &detail); err != nil {
					return BroadcastReference{}, readBackRetry
				}
				if detail.SegmentID != input.SegmentID || detail.From != input.From || detail.Subject != input.Subject || detail.HTML != input.HTML || detail.Text != input.Text || detail.TopicID != input.TopicID || detail.PreviewText != input.PreviewText || !slices.Equal(detail.ReplyTo, input.ReplyTo) {
					return BroadcastReference{}, readBackConflicted
				}
				return BroadcastReference{ID: broadcast.ID}, readBackMatched
			}
		}
		if !page.HasMore {
			return BroadcastReference{}, readBackAbsent
		}
		if len(page.Data) == 0 || page.Data[len(page.Data)-1].ID == cursor {
			return BroadcastReference{}, readBackRetry
		}
		cursor = page.Data[len(page.Data)-1].ID
	}
	return BroadcastReference{}, readBackRetry
}
func decodeBroadcastReference(body []byte) (BroadcastReference, error) {
	var value BroadcastReference
	err := json.Unmarshal(body, &value)
	if err == nil && value.ID == "" {
		err = fmt.Errorf("broadcast ID missing")
	}
	return value, err
}
func decodeBroadcast(body []byte) (Broadcast, error) {
	var value Broadcast
	err := json.Unmarshal(body, &value)
	if err == nil && value.ID == "" {
		err = fmt.Errorf("broadcast ID missing")
	}
	return value, err
}
func decodeBroadcastPage(body []byte) (BroadcastPage, error) {
	var page BroadcastPage
	err := json.Unmarshal(body, &page)
	if err == nil && page.Data == nil {
		err = fmt.Errorf("broadcast page data missing")
	}
	return page, err
}
func decodeDeletedBroadcast(body []byte) (DeletedBroadcast, error) {
	var value DeletedBroadcast
	err := json.Unmarshal(body, &value)
	if err == nil && (!value.Deleted || value.ID == "") {
		err = fmt.Errorf("broadcast deletion acknowledgement missing")
	}
	return value, err
}
func decodeBroadcastRecipientPage(body []byte) (BroadcastRecipientPage, error) {
	var page BroadcastRecipientPage
	err := json.Unmarshal(body, &page)
	if err == nil && page.Data == nil {
		err = fmt.Errorf("broadcast recipient page data missing")
	}
	return page, err
}
func decodeBroadcastClickedLinkPage(body []byte) (BroadcastClickedLinkPage, error) {
	var page BroadcastClickedLinkPage
	err := json.Unmarshal(body, &page)
	if err == nil && page.Data == nil {
		err = fmt.Errorf("broadcast clicked-link page data missing")
	}
	return page, err
}
