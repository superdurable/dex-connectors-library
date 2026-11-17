// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ses

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

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
	switch fence.Decide().Kind {
	case singledispatch.FailClosed:
		return sdkgo.NewMutationUncertain(value, sesFailure(createEmailTemplateOperationID, sdkgo.FailureTransport, "SES template creation checkpoint cannot be trusted"), sdkgo.Receipt{})
	case singledispatch.ReadBackRequired, singledispatch.ReadBackThenResendAllowed:
		confirmed, exists := operation.readBack(call, input)
		if confirmed {
			return sdkgo.NewMutationBranch(CreateEmailTemplateBranchCreated, value, nil, templateReceipt(call, input.TemplateName))
		}
		if exists {
			return templateMutationBranch[EmailTemplate](createEmailTemplateOperationID, CreateEmailTemplateBranchProviderRejected, sdkgo.FailureConflict, "SES template name belongs to different content")
		}
		if fence.Decide().Kind == singledispatch.ReadBackRequired {
			return sdkgo.NewMutationUncertain(value, sesFailure(createEmailTemplateOperationID, sdkgo.FailureTransport, "SES template creation could not be confirmed after the send cap"), sdkgo.Receipt{})
		}
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		if checkpointErr := fence.RecordNotSent(); checkpointErr != nil && !errors.Is(checkpointErr, singledispatch.ErrDispatchNotAllowed) {
			return sdkgo.NewMutationUncertain(value, sesFailure(createEmailTemplateOperationID, sdkgo.FailureTransport, "SES unsent checkpoint could not be recorded"), sdkgo.Receipt{})
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
		return sdkgo.NewMutationUncertain(value, sesFailure(createEmailTemplateOperationID, sdkgo.FailureTransport, "SES template create dispatch could not be confirmed"), sdkgo.Receipt{})
	}
	response := send.response
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationUncertain(value, sesFailure(createEmailTemplateOperationID, sdkgo.FailureTransport, "SES resend checkpoint could not be recorded"), sdkgo.Receipt{})
		}
		kind := sdkgo.FailureRateLimit
		if response.status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return sdkgo.NewMutationRetry[EmailTemplate](sesFailure(createEmailTemplateOperationID, kind, "SES asked the connector to retry template creation"), response.retryAfter)
	}
	if errors.Is(response.err, errCredential) {
		return sdkgo.NewMutationUncertain(value, sesFailure(createEmailTemplateOperationID, sdkgo.FailureProtocol, "SES template response repeated a credential"), sdkgo.Receipt{})
	}
	if response.err != nil || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[EmailTemplate](sesFailure(createEmailTemplateOperationID, sdkgo.FailureTransport, "SES template creation outcome is unknown"), 0)
	}
	if response.status != http.StatusOK {
		return templateMutationBranch[EmailTemplate](createEmailTemplateOperationID, CreateEmailTemplateBranchProviderRejected, sdkgo.FailureProviderRejection, "SES rejected template creation")
	}
	return sdkgo.NewMutationBranch(CreateEmailTemplateBranchCreated, value, nil, templateReceipt(call, input.TemplateName))
}

func (operation CreateEmailTemplateOperation) readBack(call sdkgo.Call, input CreateEmailTemplateInput) (bool, bool) {
	response := operation.client.request(call, http.MethodGet, "/v2/email/templates/"+url.PathEscape(input.TemplateName), nil)
	if response.status != http.StatusOK || response.err != nil {
		return false, false
	}
	value, err := decodeEmailTemplate(input.TemplateName, response.body)
	if err != nil {
		return false, false
	}
	return value.Content == input.Content, true
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
