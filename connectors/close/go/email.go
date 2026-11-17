// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package close

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// EmailActivity contains the public fields needed to work with a Close email.
type EmailActivity struct {
	// ID is the provider-assigned activity identifier.
	ID string `json:"id"`
	// LeadID identifies the parent lead.
	LeadID string `json:"leadId"`
	// Status is draft, outbox, scheduled, sent, inbox, or error.
	Status string `json:"status"`
	// Subject is the email subject.
	Subject string `json:"subject"`
	// BodyText is the plaintext message body.
	BodyText string `json:"bodyText"`
	// Sender is the sending email address.
	Sender string `json:"sender"`
	// To contains recipient addresses.
	To []string `json:"to"`
}

// EmailPage contains a bounded page of email activities.
type EmailPage struct {
	// Emails contains the returned activities.
	Emails []EmailActivity `json:"emails"`
	// HasMore reports whether another page exists.
	HasMore bool `json:"hasMore"`
}

// ListEmailsInput selects a page of emails on one lead.
type ListEmailsInput struct {
	// LeadID identifies the parent lead.
	LeadID string `json:"leadId"`
	// Skip is the zero-based offset.
	Skip int `json:"skip,omitempty"`
	// Limit caps emails; zero uses 100.
	Limit int `json:"limit,omitempty"`
}

// GetEmailInput identifies a Close email activity.
type GetEmailInput struct {
	// EmailID is the Close acti_ identifier.
	EmailID string `json:"emailId"`
}

// CreateEmailInput creates a draft or sends an email from Close.
type CreateEmailInput struct {
	// LeadID identifies the parent lead.
	LeadID string `json:"leadId"`
	// Status is draft or outbox; outbox sends the email.
	Status string `json:"status"`
	// Sender is required for outbox and must be an email address.
	Sender string `json:"sender,omitempty"`
	// To contains one or more recipient email addresses.
	To []string `json:"to"`
	// Subject is the nonempty subject, at most 1000 characters.
	Subject string `json:"subject"`
	// BodyText is nonempty plaintext content, at most 1048576 characters.
	BodyText string `json:"bodyText"`
}

// UpdateEmailDraftInput replaces content on an existing draft without sending it.
type UpdateEmailDraftInput struct {
	// EmailID identifies the draft.
	EmailID string `json:"emailId"`
	// Subject replaces the subject when present.
	Subject *string `json:"subject,omitempty"`
	// BodyText replaces the plaintext content when present.
	BodyText *string `json:"bodyText,omitempty"`
}

// DeleteEmailInput identifies an email activity to remove.
type DeleteEmailInput struct {
	// EmailID identifies the activity.
	EmailID string `json:"emailId"`
}

// DeletedEmail confirms that an email activity is absent.
type DeletedEmail struct {
	// EmailID identifies the absent email.
	EmailID string `json:"emailId"`
}

// ListEmailsOperation reads email activities on a lead.
type ListEmailsOperation struct{ client *Client }

// ListEmails binds this Query to the client.
func (client *Client) ListEmails() ListEmailsOperation { return ListEmailsOperation{client} }

// Definition returns the generated listEmails contract.
func (ListEmailsOperation) Definition() sdkgo.QueryDefinition { return ListEmailsDefinition }

// Invoke validates the page and classifies the Close response.
func (operation ListEmailsOperation) Invoke(call sdkgo.Call, input ListEmailsInput) sdkgo.QueryAttempt[EmailPage] {
	if !validLeadID(input.LeadID) || input.Skip < 0 || input.Limit < 0 || input.Limit > 100 {
		return failedBranch[EmailPage]("listEmails", ListEmailsBranchDefect, sdkgo.FailureValidation, "leadId, skip, or limit is invalid")
	}
	limit := input.Limit
	if limit == 0 {
		limit = 100
	}
	query := url.Values{"lead_id": {input.LeadID}, "_skip": {strconv.Itoa(input.Skip)}, "_limit": {strconv.Itoa(limit)}}
	return invokeGet(operation.client, call, "listEmails", "/activity/email/?"+query.Encode(), ListEmailsBranchListed, ListEmailsBranchProviderRejected, decodeEmailPage)
}

// GetEmailOperation reads one email activity.
type GetEmailOperation struct{ client *Client }

// GetEmail binds this Query to the client.
func (client *Client) GetEmail() GetEmailOperation { return GetEmailOperation{client} }

// Definition returns the generated getEmail contract.
func (GetEmailOperation) Definition() sdkgo.QueryDefinition { return GetEmailDefinition }

// Invoke validates the ID and classifies the Close response.
func (operation GetEmailOperation) Invoke(call sdkgo.Call, input GetEmailInput) sdkgo.QueryAttempt[EmailActivity] {
	if !validEmailActivityID(input.EmailID) {
		return failedBranch[EmailActivity]("getEmail", GetEmailBranchDefect, sdkgo.FailureValidation, "emailId is invalid")
	}
	return invokeGet(operation.client, call, "getEmail", "/activity/email/"+input.EmailID+"/", GetEmailBranchFound, GetEmailBranchNotFound, decodeEmailActivity)
}

// CreateEmailOperation creates a draft or sends an email with bounded possible sends.
type CreateEmailOperation struct{ client *Client }

// CreateEmail binds this Mutation to the client.
func (client *Client) CreateEmail() CreateEmailOperation { return CreateEmailOperation{client} }

// Definition returns the generated createEmail contract.
func (CreateEmailOperation) Definition() sdkgo.MutationDefinition { return CreateEmailDefinition }

// IdempotencyKey correlates attempts; Close does not deduplicate this key.
func (CreateEmailOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateEmailInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a draft or sends from outbox, reconciling a known provider ID first.
func (operation CreateEmailOperation) Invoke(call sdkgo.Call, input CreateEmailInput) sdkgo.MutationAttempt[EmailActivity] {
	if !validLeadID(input.LeadID) || (input.Status != "draft" && input.Status != "outbox") ||
		len(input.To) == 0 || len(input.To) > 50 || strings.TrimSpace(input.Subject) == "" || len(input.Subject) > 1000 ||
		strings.TrimSpace(input.BodyText) == "" || len(input.BodyText) > 1048576 ||
		(input.Status == "outbox" && !validEmailAddress(input.Sender)) ||
		(input.Sender != "" && !validEmailAddress(input.Sender)) {
		return closeMutationBranch[EmailActivity]("createEmail", CreateEmailBranchDefect, sdkgo.FailureValidation, "leadId, status, sender, recipients, subject, or bodyText is invalid")
	}
	for _, recipient := range input.To {
		if !validEmailAddress(recipient) {
			return closeMutationBranch[EmailActivity]("createEmail", CreateEmailBranchDefect, sdkgo.FailureValidation, "recipient address is invalid")
		}
	}
	requestBody, err := json.Marshal(struct {
		LeadID   string   `json:"lead_id"`
		Status   string   `json:"status"`
		Sender   string   `json:"sender,omitempty"`
		To       []string `json:"to"`
		Subject  string   `json:"subject"`
		BodyText string   `json:"body_text"`
	}{input.LeadID, input.Status, input.Sender, input.To, input.Subject, input.BodyText})
	if err != nil {
		return closeMutationBranch[EmailActivity]("createEmail", CreateEmailBranchDefect, sdkgo.FailureLocalDefect, "email request could not be encoded")
	}
	return invokeCloseCreate(operation.client, call, "createEmail", "/activity/email/", requestBody, CreateEmailBranchCreated, CreateEmailBranchProviderRejected, CreateEmailBranchDefect, decodeEmailActivity, validEmailActivityID)
}

// UpdateEmailDraftOperation replaces draft content without changing its status.
type UpdateEmailDraftOperation struct{ client *Client }

// UpdateEmailDraft binds this Mutation to the client.
func (client *Client) UpdateEmailDraft() UpdateEmailDraftOperation {
	return UpdateEmailDraftOperation{client}
}

// Definition returns the generated updateEmailDraft contract.
func (UpdateEmailDraftOperation) Definition() sdkgo.MutationDefinition {
	return UpdateEmailDraftDefinition
}

// IdempotencyKey correlates attempts; absolute content replacement is repeat safe.
func (UpdateEmailDraftOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateEmailDraftInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces draft content; Close refuses modifications to non-draft emails.
func (operation UpdateEmailDraftOperation) Invoke(call sdkgo.Call, input UpdateEmailDraftInput) sdkgo.MutationAttempt[EmailActivity] {
	if !validEmailActivityID(input.EmailID) || (input.Subject == nil && input.BodyText == nil) ||
		(input.Subject != nil && (strings.TrimSpace(*input.Subject) == "" || len(*input.Subject) > 1000)) ||
		(input.BodyText != nil && (strings.TrimSpace(*input.BodyText) == "" || len(*input.BodyText) > 1048576)) {
		return closeMutationBranch[EmailActivity]("updateEmailDraft", UpdateEmailDraftBranchDefect, sdkgo.FailureValidation, "emailId and at least one valid content field are required")
	}
	requestBody, err := json.Marshal(struct {
		Subject  *string `json:"subject,omitempty"`
		BodyText *string `json:"body_text,omitempty"`
	}{input.Subject, input.BodyText})
	if err != nil {
		return closeMutationBranch[EmailActivity]("updateEmailDraft", UpdateEmailDraftBranchDefect, sdkgo.FailureLocalDefect, "email update could not be encoded")
	}
	return invokeCloseWrite(operation.client, call, "updateEmailDraft", http.MethodPut, "/activity/email/"+input.EmailID+"/", requestBody, UpdateEmailDraftBranchUpdated, UpdateEmailDraftBranchNotFound, decodeEmailActivity)
}

// DeleteEmailOperation removes one email activity.
type DeleteEmailOperation struct{ client *Client }

// DeleteEmail binds this Mutation to the client.
func (client *Client) DeleteEmail() DeleteEmailOperation { return DeleteEmailOperation{client} }

// Definition returns the generated deleteEmail contract.
func (DeleteEmailOperation) Definition() sdkgo.MutationDefinition { return DeleteEmailDefinition }

// IdempotencyKey correlates attempts; deleting an absent email is repeat safe.
func (DeleteEmailOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteEmailInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes the email and classifies absence as completed deletion.
func (operation DeleteEmailOperation) Invoke(call sdkgo.Call, input DeleteEmailInput) sdkgo.MutationAttempt[DeletedEmail] {
	if !validEmailActivityID(input.EmailID) {
		return closeMutationBranch[DeletedEmail]("deleteEmail", DeleteEmailBranchDefect, sdkgo.FailureValidation, "emailId is invalid")
	}
	return invokeCloseWrite(operation.client, call, "deleteEmail", http.MethodDelete, "/activity/email/"+input.EmailID+"/", nil, DeleteEmailBranchDeleted, DeleteEmailBranchDeleted, func(body []byte) (DeletedEmail, error) {
		var response map[string]json.RawMessage
		if err := json.Unmarshal(body, &response); err != nil || response == nil {
			return DeletedEmail{}, errors.New("invalid deletion response")
		}
		return DeletedEmail{EmailID: input.EmailID}, nil
	})
}

func validEmailActivityID(emailID string) bool {
	return strings.HasPrefix(emailID, "acti_") && safeKey.MatchString(emailID)
}

func validEmailAddress(address string) bool {
	parsed, err := mail.ParseAddress(address)
	return err == nil && parsed.Address == address && len(address) <= 320
}

func decodeEmailActivity(body []byte) (EmailActivity, error) {
	var wire struct {
		ID       string   `json:"id"`
		LeadID   string   `json:"lead_id"`
		Status   string   `json:"status"`
		Subject  string   `json:"subject"`
		BodyText string   `json:"body_text"`
		Sender   string   `json:"sender"`
		To       []string `json:"to"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return EmailActivity{}, err
	}
	if !validEmailActivityID(wire.ID) || !validLeadID(wire.LeadID) {
		return EmailActivity{}, fmt.Errorf("incomplete email activity")
	}
	return EmailActivity{ID: wire.ID, LeadID: wire.LeadID, Status: wire.Status, Subject: wire.Subject, BodyText: wire.BodyText, Sender: wire.Sender, To: wire.To}, nil
}

func decodeEmailPage(body []byte) (EmailPage, error) {
	var wire struct {
		Data    []json.RawMessage `json:"data"`
		HasMore bool              `json:"has_more"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return EmailPage{}, err
	}
	if wire.Data == nil {
		return EmailPage{}, errors.New("email page is missing")
	}
	page := EmailPage{Emails: make([]EmailActivity, 0, len(wire.Data)), HasMore: wire.HasMore}
	for _, row := range wire.Data {
		email, err := decodeEmailActivity(row)
		if err != nil {
			return EmailPage{}, err
		}
		page.Emails = append(page.Emails, email)
	}
	return page, nil
}
