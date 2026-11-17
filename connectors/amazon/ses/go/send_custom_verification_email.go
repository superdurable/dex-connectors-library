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

// SendCustomVerificationEmailInput requests one verification email.
type SendCustomVerificationEmailInput struct {
	// EmailAddress is the recipient to verify.
	EmailAddress string `json:"emailAddress"`
	// TemplateName is the existing custom verification template.
	TemplateName string `json:"templateName"`
	// ConfigurationSetName is optional; blank uses the account default.
	ConfigurationSetName string `json:"configurationSetName,omitempty"`
}

// SendCustomVerificationEmailOperation sends one bounded verification email.
type SendCustomVerificationEmailOperation struct{ client *Client }

// SendCustomVerificationEmail binds the Mutation to this client.
func (client *Client) SendCustomVerificationEmail() SendCustomVerificationEmailOperation {
	return SendCustomVerificationEmailOperation{client}
}

// Definition returns the generated Mutation contract.
func (SendCustomVerificationEmailOperation) Definition() sdkgo.MutationDefinition {
	return SendCustomVerificationEmailDefinition
}

// IdempotencyKey identifies attempts of one Step, although SES cannot deduplicate them.
func (SendCustomVerificationEmailOperation) IdempotencyKey(callID sdkgo.CallID, _ SendCustomVerificationEmailInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends at most the allowed possible-send count.
func (operation SendCustomVerificationEmailOperation) Invoke(call sdkgo.Call, input SendCustomVerificationEmailInput) sdkgo.MutationAttempt[SentEmail] {
	const operationID = "sendCustomVerificationEmail"
	branch := func(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[SentEmail] {
		return templateMutationBranch[SentEmail](operationID, branch, kind, message)
	}
	if !emailAddressPattern.MatchString(input.EmailAddress) || validateTemplateName(input.TemplateName) != nil || len(input.ConfigurationSetName) > 64 {
		return branch(SendCustomVerificationEmailBranchDefect, sdkgo.FailureValidation, "SES verification recipient, template, or configuration set is invalid")
	}
	fence, err := singledispatch.NewFence(call, SendCustomVerificationEmailDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return branch(SendCustomVerificationEmailBranchDefect, sdkgo.FailureLocalDefect, "SES verification send fence could not be initialized")
	}
	switch fence.Decide().Kind {
	case singledispatch.FailClosed:
		return sdkgo.NewMutationUncertain(SentEmail{}, sesFailure(operationID, sdkgo.FailureTransport, "SES verification send checkpoint cannot be trusted"), sdkgo.Receipt{})
	case singledispatch.ReadBackRequired:
		return sdkgo.NewMutationUncertain(SentEmail{}, sesFailure(operationID, sdkgo.FailureTransport, "SES verification send outcome could not be confirmed after the send cap"), sdkgo.Receipt{})
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		if checkpointErr := fence.RecordNotSent(); checkpointErr != nil && !errors.Is(checkpointErr, singledispatch.ErrDispatchNotAllowed) {
			return sdkgo.NewMutationUncertain(SentEmail{}, sesFailure(operationID, sdkgo.FailureTransport, "SES unsent checkpoint could not be recorded"), sdkgo.Receipt{})
		}
		return branch(SendCustomVerificationEmailBranchDefect, sdkgo.FailureAuthentication, "SES credentials are unavailable or invalid")
	}
	body, err := json.Marshal(struct {
		EmailAddress         string `json:"EmailAddress"`
		TemplateName         string `json:"TemplateName"`
		ConfigurationSetName string `json:"ConfigurationSetName,omitempty"`
	}{EmailAddress: input.EmailAddress, TemplateName: input.TemplateName, ConfigurationSetName: input.ConfigurationSetName})
	if err != nil {
		return branch(SendCustomVerificationEmailBranchDefect, sdkgo.FailureLocalDefect, "SES verification send request could not be encoded")
	}
	send := &customVerificationEmailSend{client: operation.client, call: call, body: body}
	if fence.Dispatch(send.dispatch) != nil {
		return sdkgo.NewMutationUncertain(SentEmail{}, sesFailure(operationID, sdkgo.FailureTransport, "SES verification send dispatch could not be confirmed"), sdkgo.Receipt{})
	}
	response := send.response
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationUncertain(SentEmail{}, sesFailure(operationID, sdkgo.FailureTransport, "SES verification resend checkpoint could not be recorded"), sdkgo.Receipt{})
		}
		kind := sdkgo.FailureRateLimit
		if response.status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return sdkgo.NewMutationRetry[SentEmail](sesFailure(operationID, kind, "SES asked the connector to retry verification email"), response.retryAfter)
	}
	if errors.Is(response.err, errCredential) {
		return sdkgo.NewMutationUncertain(SentEmail{}, sesFailure(operationID, sdkgo.FailureProtocol, "SES verification response repeated a credential"), sdkgo.Receipt{})
	}
	if response.err != nil || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[SentEmail](sesFailure(operationID, sdkgo.FailureTransport, "SES verification send outcome is unknown"), 0)
	}
	if response.status != http.StatusOK {
		return branch(SendCustomVerificationEmailBranchProviderRejected, sdkgo.FailureProviderRejection, "SES rejected verification email")
	}
	var accepted struct {
		MessageID string `json:"MessageId"`
	}
	if json.Unmarshal(response.body, &accepted) != nil || accepted.MessageID == "" || len(accepted.MessageID) > 512 {
		return sdkgo.NewMutationUncertain(SentEmail{}, sesFailure(operationID, sdkgo.FailureProtocol, "SES accepted verification email without a usable message ID"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(SendCustomVerificationEmailBranchSent, SentEmail{MessageID: accepted.MessageID}, nil, sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "amazon-ses", ProviderObjectID: accepted.MessageID})
}

type customVerificationEmailSend struct {
	client   *Client
	call     sdkgo.Call
	body     []byte
	response providerResponse
}

func (send *customVerificationEmailSend) dispatch(requestContext context.Context) error {
	send.response = send.client.requestWithContext(send.call, requestContext, http.MethodPost, "/v2/email/outbound-custom-verification-emails", send.body)
	return nil
}
