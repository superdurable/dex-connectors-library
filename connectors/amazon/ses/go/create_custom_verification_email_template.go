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
	decision := fence.Decide()
	switch decision.Kind {
	case singledispatch.FailClosed:
		return sdkgo.NewMutationRetry[CustomVerificationEmailTemplate](sesFailure(operationID, sdkgo.FailureAvailability, "SES verification template checkpoint is unavailable"), 0)
	case singledispatch.ReadBackRequired:
		return sdkgo.NewMutationUncertain(input.Template, sesFailure(operationID, sdkgo.FailureTransport, "SES verification template creation could not be confirmed after the send cap"), sdkgo.Receipt{})
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		if checkpointErr := fence.RecordNotSent(); checkpointErr != nil && !errors.Is(checkpointErr, singledispatch.ErrDispatchNotAllowed) {
			return sdkgo.NewMutationRetry[CustomVerificationEmailTemplate](sesFailure(operationID, sdkgo.FailureAvailability, "SES unsent checkpoint could not be recorded"), 0)
		}
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationRetry[CustomVerificationEmailTemplate](sesFailure(operationID, sdkgo.FailureAuthentication, "SES credentials are unavailable after an earlier send"), 0)
		}
		return templateMutationBranch[CustomVerificationEmailTemplate](operationID, CreateCustomVerificationEmailTemplateBranchDefect, sdkgo.FailureAuthentication, "SES credentials are unavailable or invalid")
	}
	body, err := encodeCustomVerificationTemplate(input.Template, true)
	if err != nil {
		return templateMutationBranch[CustomVerificationEmailTemplate](operationID, CreateCustomVerificationEmailTemplateBranchDefect, sdkgo.FailureLocalDefect, "SES verification template request could not be encoded")
	}
	send := &customVerificationTemplateCreate{client: operation.client, call: call, body: body}
	if fence.Dispatch(send.dispatch) != nil {
		return sdkgo.NewMutationRetry[CustomVerificationEmailTemplate](sesFailure(operationID, sdkgo.FailureAvailability, "SES verification template dispatch checkpoint is unavailable"), 0)
	}
	response := send.response
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[CustomVerificationEmailTemplate](sesFailure(operationID, sdkgo.FailureAvailability, "SES verification template resend checkpoint could not be recorded"), 0)
		}
		kind := sdkgo.FailureRateLimit
		if response.status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return sdkgo.NewMutationRetry[CustomVerificationEmailTemplate](sesFailure(operationID, kind, "SES asked the connector to retry verification template creation"), response.retryAfter)
	}
	if errors.Is(response.err, errCredential) {
		return limitedSESUnknown(call, decision, input.Template, sesFailure(operationID, sdkgo.FailureProtocol, "SES verification template response repeated a credential"))
	}
	if response.err != nil || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return limitedSESUnknown(call, decision, input.Template, sesFailure(operationID, sdkgo.FailureTransport, "SES verification template creation outcome is unknown"))
	}
	if response.status != http.StatusOK {
		if decision.MaximumPriorSends == 0 && isDefiniteSESRefusal(response.status, response.body) {
			if fence.RecordCanResend() != nil {
				return sdkgo.NewMutationRetry[CustomVerificationEmailTemplate](sesFailure(operationID, sdkgo.FailureAvailability, "SES refusal checkpoint could not be recorded"), 0)
			}
			return templateMutationBranch[CustomVerificationEmailTemplate](operationID, CreateCustomVerificationEmailTemplateBranchProviderRejected, sdkgo.FailureProviderRejection, "SES rejected verification template creation")
		}
		return limitedSESUnknown(call, decision, input.Template, sesFailure(operationID, sdkgo.FailureTransport, "SES verification template create response is inconclusive"))
	}
	return sdkgo.NewMutationBranch(CreateCustomVerificationEmailTemplateBranchCreated, input.Template, nil, templateReceipt(call, input.Template.Name))
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
