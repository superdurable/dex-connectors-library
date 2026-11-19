// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ses

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

const createEmailTemplateOperationID = "createEmailTemplate"

// CreateEmailTemplateInput names a new regional template and its complete content.
type CreateEmailTemplateInput struct {
	// TemplateName is the new template name.
	TemplateName string `json:"templateName"`
	// Content contains the subject and text and/or HTML body.
	Content EmailTemplateContent `json:"content"`
}

// CreateEmailTemplateOperation implements bounded SES template creation.
type CreateEmailTemplateOperation struct{ client *Client }

// CreateEmailTemplate binds the Mutation to this client.
func (client *Client) CreateEmailTemplate() CreateEmailTemplateOperation {
	return CreateEmailTemplateOperation{client}
}

// Definition returns the generated Mutation contract.
func (CreateEmailTemplateOperation) Definition() sdkgo.MutationDefinition {
	return CreateEmailTemplateDefinition
}

// IdempotencyKey identifies attempts of one Step, although SES cannot deduplicate it.
func (CreateEmailTemplateOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateEmailTemplateInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads back after an unknown create outcome and limits possible sends.
func (operation CreateEmailTemplateOperation) Invoke(call sdkgo.Call, input CreateEmailTemplateInput) sdkgo.MutationAttempt[EmailTemplate] {
	if err := validateTemplateName(input.TemplateName); err != nil {
		return templateMutationBranch[EmailTemplate](createEmailTemplateOperationID, CreateEmailTemplateBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	if err := validateTemplateContent(input.Content); err != nil {
		return templateMutationBranch[EmailTemplate](createEmailTemplateOperationID, CreateEmailTemplateBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	value := EmailTemplate{Name: input.TemplateName, Content: input.Content}
	fence, err := singledispatch.NewFence(call, CreateEmailTemplateDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return templateMutationBranch[EmailTemplate](createEmailTemplateOperationID, CreateEmailTemplateBranchDefect, sdkgo.FailureLocalDefect, "SES template creation fence could not be initialized")
	}
	decision := fence.Decide()
	switch decision.Kind {
	case singledispatch.FailClosed:
		return sdkgo.NewMutationRetry[EmailTemplate](sesFailure(createEmailTemplateOperationID, sdkgo.FailureAvailability, "SES template creation checkpoint is unavailable"), 0)
	case singledispatch.ReadBackRequired:
		return sdkgo.NewMutationUncertain(value, sesFailure(createEmailTemplateOperationID, sdkgo.FailureTransport, "SES template creation could not be confirmed after the send cap"), sdkgo.Receipt{})
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		if checkpointErr := fence.RecordNotSent(); checkpointErr != nil && !errors.Is(checkpointErr, singledispatch.ErrDispatchNotAllowed) {
			return sdkgo.NewMutationRetry[EmailTemplate](sesFailure(createEmailTemplateOperationID, sdkgo.FailureAvailability, "SES unsent checkpoint could not be recorded"), 0)
		}
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationRetry[EmailTemplate](sesFailure(createEmailTemplateOperationID, sdkgo.FailureAuthentication, "SES credentials are unavailable after an earlier send"), 0)
		}
		return templateMutationBranch[EmailTemplate](createEmailTemplateOperationID, CreateEmailTemplateBranchDefect, sdkgo.FailureAuthentication, "SES credentials are unavailable or invalid")
	}
	body, err := json.Marshal(struct {
		TemplateName    string              `json:"TemplateName"`
		TemplateContent templateWireContent `json:"TemplateContent"`
	}{TemplateName: input.TemplateName, TemplateContent: templateWireContent{Subject: input.Content.Subject, Text: input.Content.Text, HTML: input.Content.HTML}})
	if err != nil {
		return templateMutationBranch[EmailTemplate](createEmailTemplateOperationID, CreateEmailTemplateBranchDefect, sdkgo.FailureLocalDefect, "SES template create request could not be encoded")
	}
	send := &sesTemplateCreate{client: operation.client, call: call, body: body}
	if fence.Dispatch(send.dispatch) != nil {
		return sdkgo.NewMutationRetry[EmailTemplate](sesFailure(createEmailTemplateOperationID, sdkgo.FailureAvailability, "SES template create dispatch checkpoint is unavailable"), 0)
	}
	response := send.response
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[EmailTemplate](sesFailure(createEmailTemplateOperationID, sdkgo.FailureAvailability, "SES resend checkpoint could not be recorded"), 0)
		}
		kind := sdkgo.FailureRateLimit
		if response.status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return sdkgo.NewMutationRetry[EmailTemplate](sesFailure(createEmailTemplateOperationID, kind, "SES asked the connector to retry template creation"), response.retryAfter)
	}
	if errors.Is(response.err, errCredential) {
		return limitedSESUnknown(call, decision, value, sesFailure(createEmailTemplateOperationID, sdkgo.FailureProtocol, "SES template response repeated a credential"))
	}
	if response.err != nil || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return limitedSESUnknown(call, decision, value, sesFailure(createEmailTemplateOperationID, sdkgo.FailureTransport, "SES template creation outcome is unknown"))
	}
	if response.status != http.StatusOK {
		if decision.MaximumPriorSends == 0 && isDefiniteSESRefusal(response.status, response.body) {
			if fence.RecordCanResend() != nil {
				return sdkgo.NewMutationRetry[EmailTemplate](sesFailure(createEmailTemplateOperationID, sdkgo.FailureAvailability, "SES refusal checkpoint could not be recorded"), 0)
			}
			return templateMutationBranch[EmailTemplate](createEmailTemplateOperationID, CreateEmailTemplateBranchProviderRejected, sdkgo.FailureProviderRejection, "SES rejected template creation")
		}
		return limitedSESUnknown(call, decision, value, sesFailure(createEmailTemplateOperationID, sdkgo.FailureTransport, "SES template create response is inconclusive"))
	}
	return sdkgo.NewMutationBranch(CreateEmailTemplateBranchCreated, value, nil, templateReceipt(call, input.TemplateName))
}

type sesTemplateCreate struct {
	client   *Client
	call     sdkgo.Call
	body     []byte
	response providerResponse
}

func (send *sesTemplateCreate) dispatch(requestContext context.Context) error {
	send.response = send.client.requestWithContext(send.call, requestContext, http.MethodPost, "/v2/email/templates", send.body)
	return nil
}
