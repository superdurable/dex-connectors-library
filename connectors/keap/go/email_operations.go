// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package keap

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// ListEmailsInput selects a bounded page of sent-email records.
type ListEmailsInput struct {
	// PageSize is the maximum records to return; zero uses 20.
	PageSize int `json:"pageSize,omitempty"`
	// PageToken is an opaque continuation token; blank starts the first page.
	PageToken string `json:"pageToken,omitempty"`
}

// EmailRecordIDInput selects a sent-email record by ID.
type EmailRecordIDInput struct {
	// EmailID is the Keap email-record ID.
	EmailID string `json:"emailId"`
}

// CreateEmailRecordInput describes an email already sent outside Keap.
type CreateEmailRecordInput struct {
	// ContactID is the receiving contact's Keap ID.
	ContactID string `json:"contactId"`
	// SentToAddress is the email address that received the message.
	SentToAddress string `json:"sentToAddress"`
	// Subject is the email subject, at most 998 bytes.
	Subject string `json:"subject"`
}

// SendEmailInput describes one email to send to one Keap contact.
type SendEmailInput struct {
	// ContactID is the receiving contact's Keap ID.
	ContactID string `json:"contactId"`
	// UserID is the Keap user on whose behalf to send.
	UserID string `json:"userId"`
	// Subject is the email subject, at most 998 bytes.
	Subject string `json:"subject"`
	// PlainContent is the plain-text message, at most 64 KiB.
	PlainContent string `json:"plainContent"`
}

// EmailRecord contains safe identifying fields from a sent-email record.
type EmailRecord struct {
	// ID is Keap's sent-email record ID.
	ID string `json:"id"`
	// Subject is the message subject.
	Subject string `json:"subject"`
	// ContactID is the receiving contact's Keap ID.
	ContactID string `json:"contactId"`
	// SentToAddress is the recipient email address.
	SentToAddress string `json:"sentToAddress"`
}

// EmailsPage contains a page of sent-email records and its continuation token.
type EmailsPage struct {
	// Emails are the returned records.
	Emails []EmailRecord `json:"emails"`
	// NextPageToken selects the next page, when present.
	NextPageToken string `json:"nextPageToken"`
}

// EmailSendReceipt confirms Keap accepted one send request.
type EmailSendReceipt struct {
	// ContactID is the recipient contact ID.
	ContactID string `json:"contactId"`
	// Accepted reports that Keap accepted the send.
	Accepted bool `json:"accepted"`
}

// EmailRecordDeletion confirms deletion of a sent-email record.
type EmailRecordDeletion struct {
	// EmailID is the deleted record ID.
	EmailID string `json:"emailId"`
	// Deleted reports Keap's acceptance of the deletion.
	Deleted bool `json:"deleted"`
}

// ListEmailsOperation reads one page of sent-email records.
type ListEmailsOperation struct{ client *Client }

// CreateEmailRecordOperation records an already-sent email with bounded sends.
type CreateEmailRecordOperation struct{ client *Client }

// SendEmailOperation sends one email with bounded sends.
type SendEmailOperation struct{ client *Client }

// DeleteEmailRecordOperation deletes one sent-email record.
type DeleteEmailRecordOperation struct{ client *Client }

// ListEmails binds an email-record list to the client.
func (client *Client) ListEmails() ListEmailsOperation { return ListEmailsOperation{client} }

// CreateEmailRecord binds record creation to the client.
func (client *Client) CreateEmailRecord() CreateEmailRecordOperation {
	return CreateEmailRecordOperation{client}
}

// SendEmail binds email sending to the client.
func (client *Client) SendEmail() SendEmailOperation { return SendEmailOperation{client} }

// DeleteEmailRecord binds record deletion to the client.
func (client *Client) DeleteEmailRecord() DeleteEmailRecordOperation {
	return DeleteEmailRecordOperation{client}
}

// Definition returns the generated query contract.
func (ListEmailsOperation) Definition() sdkgo.QueryDefinition { return ListEmailsDefinition }

// Definition returns the generated mutation contract.
func (CreateEmailRecordOperation) Definition() sdkgo.MutationDefinition {
	return CreateEmailRecordDefinition
}

// Definition returns the generated mutation contract.
func (SendEmailOperation) Definition() sdkgo.MutationDefinition { return SendEmailDefinition }

// Definition returns the generated mutation contract.
func (DeleteEmailRecordOperation) Definition() sdkgo.MutationDefinition {
	return DeleteEmailRecordDefinition
}

// IdempotencyKey derives the stable Dex call identity.
func (CreateEmailRecordOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateEmailRecordInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey derives the stable Dex call identity.
func (SendEmailOperation) IdempotencyKey(callID sdkgo.CallID, _ SendEmailInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey derives the stable Dex call identity.
func (DeleteEmailRecordOperation) IdempotencyKey(callID sdkgo.CallID, _ EmailRecordIDInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads one bounded page of sent-email records.
func (operation ListEmailsOperation) Invoke(call sdkgo.Call, input ListEmailsInput) sdkgo.QueryAttempt[EmailsPage] {
	if input.PageSize < 0 || input.PageSize > 100 || len(input.PageToken) > 2048 {
		return keapQueryBranch("listEmails", ListEmailsBranchDefect, EmailsPage{}, sdkgo.FailureValidation, "page size or token is invalid")
	}
	if input.PageSize == 0 {
		input.PageSize = 20
	}
	query := url.Values{"page_size": {strconv.Itoa(input.PageSize)}}
	if input.PageToken != "" {
		query.Set("page_token", input.PageToken)
	}
	response := operation.client.request(call, http.MethodGet, "/rest/v2/emails?"+query.Encode(), nil)
	if branch, retry, handled := classifyKeapQuery(response, "listEmails", ListEmailsBranchProviderRejected, ListEmailsBranchInvalidResponse, ListEmailsBranchDefect, EmailsPage{}); handled {
		if retry != nil {
			return *retry
		}
		return *branch
	}
	var wire struct {
		Emails        []json.RawMessage `json:"emails"`
		NextPageToken string            `json:"next_page_token"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Emails == nil {
		return keapQueryBranch("listEmails", ListEmailsBranchInvalidResponse, EmailsPage{}, sdkgo.FailureProtocol, "Keap returned invalid email list JSON")
	}
	page := EmailsPage{Emails: make([]EmailRecord, 0, len(wire.Emails)), NextPageToken: wire.NextPageToken}
	for _, raw := range wire.Emails {
		email, ok := decodeEmailRecord(raw)
		if !ok {
			return keapQueryBranch("listEmails", ListEmailsBranchInvalidResponse, EmailsPage{}, sdkgo.FailureProtocol, "Keap returned invalid email record")
		}
		page.Emails = append(page.Emails, email)
	}
	return sdkgo.NewQueryBranch(ListEmailsBranchListed, page, nil, sdkgo.Receipt{})
}

// Invoke records one previously sent email.
func (operation CreateEmailRecordOperation) Invoke(call sdkgo.Call, input CreateEmailRecordInput) sdkgo.MutationAttempt[EmailRecord] {
	if !validContactID(input.ContactID) || !validKeapEmailAddress(input.SentToAddress) || !validEmailSubject(input.Subject) {
		return keapMutationBranch("createEmailRecord", CreateEmailRecordBranchDefect, EmailRecord{}, sdkgo.FailureValidation, "contact ID, recipient, or subject is invalid")
	}
	body, _ := json.Marshal(map[string]any{"contact_id": input.ContactID, "sent_to_address": input.SentToAddress, "subject": input.Subject})
	return dispatchLimitedEmail(operation.client, call, "createEmailRecord", "/rest/v2/emails", body, CreateEmailRecordDefinition, CreateEmailRecordBranchCreated, CreateEmailRecordBranchProviderRejected, CreateEmailRecordBranchDefect, func(response providerResponse) (EmailRecord, bool) {
		if response.status != 200 && response.status != 201 {
			return EmailRecord{}, false
		}
		return decodeEmailRecord(response.body)
	})
}

// Invoke sends one plain-text email to a Keap contact.
func (operation SendEmailOperation) Invoke(call sdkgo.Call, input SendEmailInput) sdkgo.MutationAttempt[EmailSendReceipt] {
	if !validContactID(input.ContactID) || !validContactID(input.UserID) || !validEmailSubject(input.Subject) || strings.TrimSpace(input.PlainContent) == "" || len(input.PlainContent) > 65536 {
		return keapMutationBranch("sendEmail", SendEmailBranchDefect, EmailSendReceipt{}, sdkgo.FailureValidation, "contact, sender, subject, or content is invalid")
	}
	body, _ := json.Marshal(map[string]any{"contacts": []string{input.ContactID}, "user_id": input.UserID, "subject": input.Subject, "plain_content": base64.StdEncoding.EncodeToString([]byte(input.PlainContent))})
	return dispatchLimitedEmail(operation.client, call, "sendEmail", "/rest/v2/emails:send", body, SendEmailDefinition, SendEmailBranchSent, SendEmailBranchProviderRejected, SendEmailBranchDefect, func(response providerResponse) (EmailSendReceipt, bool) {
		return EmailSendReceipt{ContactID: input.ContactID, Accepted: true}, response.status == 200 || response.status == 204
	})
}

// Invoke deletes one sent-email record.
func (operation DeleteEmailRecordOperation) Invoke(call sdkgo.Call, input EmailRecordIDInput) sdkgo.MutationAttempt[EmailRecordDeletion] {
	if !validContactID(input.EmailID) {
		return keapMutationBranch("deleteEmailRecord", DeleteEmailRecordBranchDefect, EmailRecordDeletion{}, sdkgo.FailureValidation, "email ID is invalid")
	}
	response := operation.client.request(call, http.MethodDelete, "/rest/v2/emails/"+input.EmailID, nil)
	if response.status == 404 {
		return keapMutationBranch("deleteEmailRecord", DeleteEmailRecordBranchNotFound, EmailRecordDeletion{}, sdkgo.FailureNotFound, "Keap did not find the email")
	}
	if branch, retry, handled := classifyKeapMutation(response, "deleteEmailRecord", DeleteEmailRecordBranchProviderRejected, DeleteEmailRecordBranchInvalidResponse, DeleteEmailRecordBranchDefect, EmailRecordDeletion{}); handled {
		if retry != nil {
			return *retry
		}
		return *branch
	}
	if response.status != 200 && response.status != 204 {
		return keapMutationBranch("deleteEmailRecord", DeleteEmailRecordBranchProviderRejected, EmailRecordDeletion{}, sdkgo.FailureProviderRejection, "Keap rejected email deletion")
	}
	if response.status == 200 && len(response.body) > 0 && !json.Valid(response.body) {
		return keapMutationBranch("deleteEmailRecord", DeleteEmailRecordBranchInvalidResponse, EmailRecordDeletion{}, sdkgo.FailureProtocol, "Keap returned invalid deletion JSON")
	}
	return sdkgo.NewMutationBranch(DeleteEmailRecordBranchDeleted, EmailRecordDeletion{EmailID: input.EmailID, Deleted: true}, nil, sdkgo.Receipt{})
}

func dispatchLimitedEmail[T any](client *Client, call sdkgo.Call, operationName string, path string, body []byte, definition sdkgo.MutationDefinition, successBranch sdkgo.BranchID, rejectedBranch sdkgo.BranchID, defectBranch sdkgo.BranchID, decodeSuccess func(providerResponse) (T, bool)) sdkgo.MutationAttempt[T] {
	var zero T
	fence, err := singledispatch.NewFence(call, definition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return keapMutationBranch(operationName, defectBranch, zero, sdkgo.FailureLocalDefect, "email dispatch fence could not be constructed")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(zero, keapFailure(operationName, sdkgo.FailureTransport, "an earlier email request may have succeeded"), sdkgo.Receipt{})
	}
	canResend := decision.MaximumPriorSends+1 < call.MaxSends()
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = client.requestWithContext(requestContext, call, http.MethodPost, path, body)
		return response.err
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[T](keapFailure(operationName, sdkgo.FailureAvailability, "email dispatch could not start"), 0)
	}
	if errors.Is(response.err, errCredential) {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(zero, keapFailure(operationName, sdkgo.FailureAuthentication, "earlier email request may have succeeded"), sdkgo.Receipt{})
		}
		return keapMutationBranch(operationName, defectBranch, zero, sdkgo.FailureAuthentication, "Keap credentials are unavailable")
	}
	if response.status == 429 || response.status == 503 {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[T](keapFailure(operationName, sdkgo.FailureAvailability, "retry checkpoint failed"), 0)
		}
		return sdkgo.NewMutationRetry[T](keapFailure(operationName, sdkgo.FailureAvailability, "Keap did not process the email request"), response.retryAfter)
	}
	if errors.Is(response.err, errRequest) || response.status == 408 || response.status >= 500 || response.err != nil {
		failure := keapFailure(operationName, sdkgo.FailureTransport, "email request outcome is unknown")
		if canResend {
			return sdkgo.NewMutationRetry[T](failure, response.retryAfter)
		}
		return sdkgo.NewMutationUncertain(zero, failure, sdkgo.Receipt{})
	}
	if response.status < 200 || response.status >= 300 {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(zero, keapFailure(operationName, sdkgo.FailureProviderRejection, "earlier email request may have succeeded"), sdkgo.Receipt{})
		}
		return keapMutationBranch(operationName, rejectedBranch, zero, sdkgo.FailureProviderRejection, "Keap rejected the email request")
	}
	result, ok := decodeSuccess(response)
	if !ok {
		return sdkgo.NewMutationUncertain(zero, keapFailure(operationName, sdkgo.FailureProtocol, "email request outcome cannot be confirmed"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(successBranch, result, nil, sdkgo.Receipt{})
}

func decodeEmailRecord(body []byte) (EmailRecord, bool) {
	var wire struct {
		ID            string `json:"id"`
		Subject       string `json:"subject"`
		ContactID     string `json:"contact_id"`
		SentToAddress string `json:"sent_to_address"`
	}
	if json.Unmarshal(body, &wire) != nil || !validContactID(wire.ID) {
		return EmailRecord{}, false
	}
	return EmailRecord{ID: wire.ID, Subject: wire.Subject, ContactID: wire.ContactID, SentToAddress: wire.SentToAddress}, true
}

func validEmailSubject(subject string) bool {
	return strings.TrimSpace(subject) != "" && len(subject) <= 998
}

func validKeapEmailAddress(address string) bool {
	return len(address) <= 254 && strings.Contains(address, "@") && !strings.ContainsAny(address, " \r\n\t")
}
