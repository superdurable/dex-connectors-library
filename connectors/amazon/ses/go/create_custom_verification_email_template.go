// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ses

import (
	"context"
	"errors"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateCustomVerificationEmailTemplateInput supplies a complete new template.
type CreateCustomVerificationEmailTemplateInput struct {
	// Template includes the new name, sender, subject, content, and redirect URLs.
	Template CustomVerificationEmailTemplate `json:"template"`
}

// CreateCustomVerificationEmailTemplateOperation creates a verification template.
type CreateCustomVerificationEmailTemplateOperation struct{ client *Client }

// CreateCustomVerificationEmailTemplate binds the Mutation to this client.
func (client *Client) CreateCustomVerificationEmailTemplate() CreateCustomVerificationEmailTemplateOperation {
	return CreateCustomVerificationEmailTemplateOperation{client}
}

// Definition returns the generated Mutation contract.
func (CreateCustomVerificationEmailTemplateOperation) Definition() sdkgo.MutationDefinition {
	return CreateCustomVerificationEmailTemplateDefinition
}

// IdempotencyKey identifies attempts of one Step, although SES cannot deduplicate them.
func (CreateCustomVerificationEmailTemplateOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateCustomVerificationEmailTemplateInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads back unknown results and caps possible create requests.
func (operation CreateCustomVerificationEmailTemplateOperation) Invoke(call sdkgo.Call, input CreateCustomVerificationEmailTemplateInput) sdkgo.MutationAttempt[CustomVerificationEmailTemplate] {
	const operationID = "createCustomVerificationEmailTemplate"
	if err := validateCustomVerificationTemplate(input.Template); err != nil {
		return templateMutationBranch[CustomVerificationEmailTemplate](operationID, CreateCustomVerificationEmailTemplateBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	fence, err := singledispatch.NewFence(call, CreateCustomVerificationEmailTemplateDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return templateMutationBranch[CustomVerificationEmailTemplate](operationID, CreateCustomVerificationEmailTemplateBranchDefect, sdkgo.FailureLocalDefect, "SES verification template fence could not be initialized")
	}
	switch fence.Decide().Kind {
	case singledispatch.FailClosed:
		return sdkgo.NewMutationUncertain(input.Template, sesFailure(operationID, sdkgo.FailureTransport, "SES verification template checkpoint cannot be trusted"), sdkgo.Receipt{})
	case singledispatch.ReadBackRequired, singledispatch.ReadBackThenResendAllowed:
		confirmed, exists := operation.readBack(call, input.Template)
		if confirmed {
			return sdkgo.NewMutationBranch(CreateCustomVerificationEmailTemplateBranchCreated, input.Template, nil, templateReceipt(call, input.Template.Name))
		}
		if exists {
			return templateMutationBranch[CustomVerificationEmailTemplate](operationID, CreateCustomVerificationEmailTemplateBranchProviderRejected, sdkgo.FailureConflict, "SES verification template name belongs to different content")
		}
		if fence.Decide().Kind == singledispatch.ReadBackRequired {
			return sdkgo.NewMutationUncertain(input.Template, sesFailure(operationID, sdkgo.FailureTransport, "SES verification template creation could not be confirmed after the send cap"), sdkgo.Receipt{})
		}
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		if checkpointErr := fence.RecordNotSent(); checkpointErr != nil && !errors.Is(checkpointErr, singledispatch.ErrDispatchNotAllowed) {
			return sdkgo.NewMutationUncertain(input.Template, sesFailure(operationID, sdkgo.FailureTransport, "SES unsent checkpoint could not be recorded"), sdkgo.Receipt{})
		}
		return templateMutationBranch[CustomVerificationEmailTemplate](operationID, CreateCustomVerificationEmailTemplateBranchDefect, sdkgo.FailureAuthentication, "SES credentials are unavailable or invalid")
	}
	body, err := encodeCustomVerificationTemplate(input.Template, true)
	if err != nil {
		return templateMutationBranch[CustomVerificationEmailTemplate](operationID, CreateCustomVerificationEmailTemplateBranchDefect, sdkgo.FailureLocalDefect, "SES verification template request could not be encoded")
	}
	send := &customVerificationTemplateCreate{client: operation.client, call: call, body: body}
	if fence.Dispatch(send.dispatch) != nil {
		return sdkgo.NewMutationUncertain(input.Template, sesFailure(operationID, sdkgo.FailureTransport, "SES verification template dispatch could not be confirmed"), sdkgo.Receipt{})
	}
	response := send.response
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationUncertain(input.Template, sesFailure(operationID, sdkgo.FailureTransport, "SES verification template resend checkpoint could not be recorded"), sdkgo.Receipt{})
		}
		kind := sdkgo.FailureRateLimit
		if response.status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return sdkgo.NewMutationRetry[CustomVerificationEmailTemplate](sesFailure(operationID, kind, "SES asked the connector to retry verification template creation"), response.retryAfter)
	}
	if errors.Is(response.err, errCredential) {
		return sdkgo.NewMutationUncertain(input.Template, sesFailure(operationID, sdkgo.FailureProtocol, "SES verification template response repeated a credential"), sdkgo.Receipt{})
	}
	if response.err != nil || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[CustomVerificationEmailTemplate](sesFailure(operationID, sdkgo.FailureTransport, "SES verification template creation outcome is unknown"), 0)
	}
	if response.status != http.StatusOK {
		return templateMutationBranch[CustomVerificationEmailTemplate](operationID, CreateCustomVerificationEmailTemplateBranchProviderRejected, sdkgo.FailureProviderRejection, "SES rejected verification template creation")
	}
	return sdkgo.NewMutationBranch(CreateCustomVerificationEmailTemplateBranchCreated, input.Template, nil, templateReceipt(call, input.Template.Name))
}

func (operation CreateCustomVerificationEmailTemplateOperation) readBack(call sdkgo.Call, expected CustomVerificationEmailTemplate) (bool, bool) {
	response := operation.client.request(call, http.MethodGet, customVerificationTemplatePath(expected.Name), nil)
	if response.status != http.StatusOK || response.err != nil {
		return false, false
	}
	actual, err := decodeCustomVerificationTemplate(expected.Name, response.body)
	if err != nil {
		return false, false
	}
	return actual == expected, true
}

type customVerificationTemplateCreate struct {
	client   *Client
	call     sdkgo.Call
	body     []byte
	response providerResponse
}

func (send *customVerificationTemplateCreate) dispatch(requestContext context.Context) error {
	send.response = send.client.requestWithContext(send.call, requestContext, http.MethodPost, customVerificationTemplatesPath, send.body)
	return nil
}
