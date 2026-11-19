// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ses

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	updateEmailTemplateOperationID = "updateEmailTemplate"
	deleteEmailTemplateOperationID = "deleteEmailTemplate"
)

// UpdateEmailTemplateInput replaces one stored template's complete content.
type UpdateEmailTemplateInput struct {
	// TemplateName is the exact regional template name.
	TemplateName string `json:"templateName"`
	// Content replaces the subject and both optional body parts.
	Content EmailTemplateContent `json:"content"`
}

// UpdateEmailTemplateOperation implements absolute template replacement.
type UpdateEmailTemplateOperation struct{ client *Client }

// UpdateEmailTemplate binds the Mutation to this client.
func (client *Client) UpdateEmailTemplate() UpdateEmailTemplateOperation {
	return UpdateEmailTemplateOperation{client}
}

// Definition returns the generated Mutation contract.
func (UpdateEmailTemplateOperation) Definition() sdkgo.MutationDefinition {
	return UpdateEmailTemplateDefinition
}

// IdempotencyKey identifies retries of the same absolute replacement.
func (UpdateEmailTemplateOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateEmailTemplateInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces all template content with the supplied content.
func (operation UpdateEmailTemplateOperation) Invoke(call sdkgo.Call, input UpdateEmailTemplateInput) sdkgo.MutationAttempt[EmailTemplate] {
	if err := validateTemplateName(input.TemplateName); err != nil {
		return templateMutationBranch[EmailTemplate](updateEmailTemplateOperationID, UpdateEmailTemplateBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	if err := validateTemplateContent(input.Content); err != nil {
		return templateMutationBranch[EmailTemplate](updateEmailTemplateOperationID, UpdateEmailTemplateBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	body, err := encodeTemplateContent(input.Content)
	if err != nil {
		return templateMutationBranch[EmailTemplate](updateEmailTemplateOperationID, UpdateEmailTemplateBranchDefect, sdkgo.FailureLocalDefect, "SES template content could not be encoded")
	}
	response := operation.client.request(call, http.MethodPut, "/v2/email/templates/"+url.PathEscape(input.TemplateName), body)
	if attempt, isFailure := templateMutationFailure[EmailTemplate](updateEmailTemplateOperationID, response, UpdateEmailTemplateBranchProviderRejected, UpdateEmailTemplateBranchDefect); isFailure {
		return attempt
	}
	return sdkgo.NewMutationBranch(UpdateEmailTemplateBranchUpdated, EmailTemplate{Name: input.TemplateName, Content: input.Content}, nil, templateReceipt(call, input.TemplateName))
}

// DeleteEmailTemplateInput names one stored template to remove.
type DeleteEmailTemplateInput struct {
	// TemplateName is the exact regional template name.
	TemplateName string `json:"templateName"`
}

// DeletedEmailTemplate identifies a removed or already absent template.
type DeletedEmailTemplate struct {
	// Name is the template name.
	Name string `json:"name"`
}

// DeleteEmailTemplateOperation implements template deletion.
type DeleteEmailTemplateOperation struct{ client *Client }

// DeleteEmailTemplate binds the Mutation to this client.
func (client *Client) DeleteEmailTemplate() DeleteEmailTemplateOperation {
	return DeleteEmailTemplateOperation{client}
}

// Definition returns the generated Mutation contract.
func (DeleteEmailTemplateOperation) Definition() sdkgo.MutationDefinition {
	return DeleteEmailTemplateDefinition
}

// IdempotencyKey identifies retries of the same deletion.
func (DeleteEmailTemplateOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteEmailTemplateInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke removes a template; a missing template is already deleted.
func (operation DeleteEmailTemplateOperation) Invoke(call sdkgo.Call, input DeleteEmailTemplateInput) sdkgo.MutationAttempt[DeletedEmailTemplate] {
	if err := validateTemplateName(input.TemplateName); err != nil {
		return templateMutationBranch[DeletedEmailTemplate](deleteEmailTemplateOperationID, DeleteEmailTemplateBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	response := operation.client.request(call, http.MethodDelete, "/v2/email/templates/"+url.PathEscape(input.TemplateName), nil)
	if response.status == http.StatusNotFound && response.err == nil {
		return sdkgo.NewMutationBranch(DeleteEmailTemplateBranchDeleted, DeletedEmailTemplate{Name: input.TemplateName}, nil, templateReceipt(call, input.TemplateName))
	}
	if attempt, isFailure := templateMutationFailure[DeletedEmailTemplate](deleteEmailTemplateOperationID, response, DeleteEmailTemplateBranchProviderRejected, DeleteEmailTemplateBranchDefect); isFailure {
		return attempt
	}
	return sdkgo.NewMutationBranch(DeleteEmailTemplateBranchDeleted, DeletedEmailTemplate{Name: input.TemplateName}, nil, templateReceipt(call, input.TemplateName))
}

func encodeTemplateContent(content EmailTemplateContent) ([]byte, error) {
	return json.Marshal(struct {
		TemplateContent templateWireContent `json:"TemplateContent"`
	}{TemplateContent: templateWireContent{Subject: content.Subject, Text: content.Text, HTML: content.HTML}})
}

type templateWireContent struct {
	Subject string `json:"Subject"`
	Text    string `json:"Text,omitempty"`
	HTML    string `json:"Html,omitempty"`
}

func templateMutationFailure[T any](operationID string, response providerResponse, rejected, defect sdkgo.BranchID) (sdkgo.MutationAttempt[T], bool) {
	if errors.Is(response.err, errCredential) {
		return templateMutationBranch[T](operationID, defect, sdkgo.FailureAuthentication, "SES credentials are unavailable or response repeated a credential"), true
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewMutationRetry[T](sesFailure(operationID, sdkgo.FailureTransport, "SES template request failed"), 0), true
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusRequestTimeout || response.status >= 500 {
		kind := sdkgo.FailureAvailability
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return sdkgo.NewMutationRetry[T](sesFailure(operationID, kind, "SES template request is temporarily unavailable"), response.retryAfter), true
	}
	if response.status != http.StatusOK {
		return templateMutationBranch[T](operationID, rejected, sdkgo.FailureProviderRejection, "SES rejected template request HTTP "+strconv.Itoa(response.status)), true
	}
	if response.err != nil {
		return sdkgo.NewMutationRetry[T](sesFailure(operationID, sdkgo.FailureTransport, "SES template response was interrupted"), 0), true
	}
	return sdkgo.MutationAttempt[T]{}, false
}

func templateMutationBranch[T any](operationID string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	failure := sesFailure(operationID, kind, message)
	return sdkgo.NewMutationBranch(branch, *new(T), &failure, sdkgo.Receipt{})
}

func templateReceipt(call sdkgo.Call, name string) sdkgo.Receipt {
	return sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "amazon-ses", ProviderObjectID: name}
}
