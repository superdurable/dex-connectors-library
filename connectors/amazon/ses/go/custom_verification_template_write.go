// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ses

import (
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// UpdateCustomVerificationEmailTemplateInput replaces one template completely.
type UpdateCustomVerificationEmailTemplateInput struct {
	// Template is the full replacement, including its existing name.
	Template CustomVerificationEmailTemplate `json:"template"`
}

// UpdateCustomVerificationEmailTemplateOperation replaces a verification template.
type UpdateCustomVerificationEmailTemplateOperation struct{ client *Client }

// UpdateCustomVerificationEmailTemplate binds the Mutation to this client.
func (client *Client) UpdateCustomVerificationEmailTemplate() UpdateCustomVerificationEmailTemplateOperation {
	return UpdateCustomVerificationEmailTemplateOperation{client}
}

// Definition returns the generated Mutation contract.
func (UpdateCustomVerificationEmailTemplateOperation) Definition() sdkgo.MutationDefinition {
	return UpdateCustomVerificationEmailTemplateDefinition
}

// IdempotencyKey identifies retries of one absolute replacement.
func (UpdateCustomVerificationEmailTemplateOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateCustomVerificationEmailTemplateInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces all template fields.
func (operation UpdateCustomVerificationEmailTemplateOperation) Invoke(call sdkgo.Call, input UpdateCustomVerificationEmailTemplateInput) sdkgo.MutationAttempt[CustomVerificationEmailTemplate] {
	const operationID = "updateCustomVerificationEmailTemplate"
	if err := validateCustomVerificationTemplate(input.Template); err != nil {
		return templateMutationBranch[CustomVerificationEmailTemplate](operationID, UpdateCustomVerificationEmailTemplateBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	body, err := encodeCustomVerificationTemplate(input.Template, false)
	if err != nil {
		return templateMutationBranch[CustomVerificationEmailTemplate](operationID, UpdateCustomVerificationEmailTemplateBranchDefect, sdkgo.FailureLocalDefect, "SES verification template could not be encoded")
	}
	response := operation.client.request(call, http.MethodPut, customVerificationTemplatePath(input.Template.Name), body)
	if attempt, failed := templateMutationFailure[CustomVerificationEmailTemplate](operationID, response, UpdateCustomVerificationEmailTemplateBranchProviderRejected, UpdateCustomVerificationEmailTemplateBranchDefect); failed {
		return attempt
	}
	return sdkgo.NewMutationBranch(UpdateCustomVerificationEmailTemplateBranchUpdated, input.Template, nil, templateReceipt(call, input.Template.Name))
}

// DeleteCustomVerificationEmailTemplateInput names one regional template.
type DeleteCustomVerificationEmailTemplateInput struct {
	// TemplateName is the exact template name in SES.
	TemplateName string `json:"templateName"`
}

// DeleteCustomVerificationEmailTemplateOperation removes a verification template.
type DeleteCustomVerificationEmailTemplateOperation struct{ client *Client }

// DeleteCustomVerificationEmailTemplate binds the Mutation to this client.
func (client *Client) DeleteCustomVerificationEmailTemplate() DeleteCustomVerificationEmailTemplateOperation {
	return DeleteCustomVerificationEmailTemplateOperation{client}
}

// Definition returns the generated Mutation contract.
func (DeleteCustomVerificationEmailTemplateOperation) Definition() sdkgo.MutationDefinition {
	return DeleteCustomVerificationEmailTemplateDefinition
}

// IdempotencyKey identifies retries of one deletion.
func (DeleteCustomVerificationEmailTemplateOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteCustomVerificationEmailTemplateInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke removes a template; an absent template is already deleted.
func (operation DeleteCustomVerificationEmailTemplateOperation) Invoke(call sdkgo.Call, input DeleteCustomVerificationEmailTemplateInput) sdkgo.MutationAttempt[DeletedEmailTemplate] {
	const operationID = "deleteCustomVerificationEmailTemplate"
	if err := validateTemplateName(input.TemplateName); err != nil {
		return templateMutationBranch[DeletedEmailTemplate](operationID, DeleteCustomVerificationEmailTemplateBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	response := operation.client.request(call, http.MethodDelete, customVerificationTemplatePath(input.TemplateName), nil)
	if response.status == http.StatusNotFound && response.err == nil {
		return sdkgo.NewMutationBranch(DeleteCustomVerificationEmailTemplateBranchDeleted, DeletedEmailTemplate{Name: input.TemplateName}, nil, templateReceipt(call, input.TemplateName))
	}
	if attempt, failed := templateMutationFailure[DeletedEmailTemplate](operationID, response, DeleteCustomVerificationEmailTemplateBranchProviderRejected, DeleteCustomVerificationEmailTemplateBranchDefect); failed {
		return attempt
	}
	return sdkgo.NewMutationBranch(DeleteCustomVerificationEmailTemplateBranchDeleted, DeletedEmailTemplate{Name: input.TemplateName}, nil, templateReceipt(call, input.TemplateName))
}
