// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ses

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

const sendTemplatedEmailOperationID = "sendTemplatedEmail"

// SendTemplatedEmailInput sends a stored SES template with JSON variables.
type SendTemplatedEmailInput struct {
	// FromEmailAddress is a verified SES sender.
	FromEmailAddress string `json:"fromEmailAddress"`
	// ToEmailAddress is the recipient; sandbox accounts require a verified recipient.
	ToEmailAddress string `json:"toEmailAddress"`
	// TemplateName is the stored template name in the connection Region.
	TemplateName string `json:"templateName"`
	// TemplateData maps placeholder names to text values; blank sends an empty JSON object.
	TemplateData map[string]string `json:"templateData,omitempty"`
}

// SendTemplatedEmailOperation implements a bounded templated email send.
type SendTemplatedEmailOperation struct{ client *Client }

// SendTemplatedEmail binds the Mutation to this client.
func (client *Client) SendTemplatedEmail() SendTemplatedEmailOperation {
	return SendTemplatedEmailOperation{client}
}

// Definition returns the generated Mutation contract.
func (SendTemplatedEmailOperation) Definition() sdkgo.MutationDefinition {
	return SendTemplatedEmailDefinition
}

// IdempotencyKey identifies attempts of one Step, although SES cannot deduplicate the send.
func (SendTemplatedEmailOperation) IdempotencyKey(callID sdkgo.CallID, _ SendTemplatedEmailInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends at most the configured possible-send count and returns the provider message ID.
func (operation SendTemplatedEmailOperation) Invoke(call sdkgo.Call, input SendTemplatedEmailInput) sdkgo.MutationAttempt[SentEmail] {
	if !emailAddressPattern.MatchString(input.FromEmailAddress) || !emailAddressPattern.MatchString(input.ToEmailAddress) || validateTemplateName(input.TemplateName) != nil || len(input.TemplateData) > 100 {
		return templatedSendBranch(SendTemplatedEmailBranchDefect, sdkgo.FailureValidation, "SES sender, recipient, template name, or data is invalid")
	}
	data, err := json.Marshal(input.TemplateData)
	if err != nil || len(data) > 100*1024 {
		return templatedSendBranch(SendTemplatedEmailBranchDefect, sdkgo.FailureValidation, "SES template data exceeds 100 KiB or cannot be encoded")
	}
	if input.TemplateData == nil {
		data = []byte("{}")
	}
	fence, err := singledispatch.NewFence(call, SendTemplatedEmailDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return templatedSendBranch(SendTemplatedEmailBranchDefect, sdkgo.FailureLocalDefect, "SES templated send fence could not be initialized")
	}
	switch fence.Decide().Kind {
	case singledispatch.FailClosed:
		return sdkgo.NewMutationUncertain(SentEmail{}, sesFailure(sendTemplatedEmailOperationID, sdkgo.FailureTransport, "SES templated send checkpoint cannot be trusted"), sdkgo.Receipt{})
	case singledispatch.ReadBackRequired:
		return sdkgo.NewMutationUncertain(SentEmail{}, sesFailure(sendTemplatedEmailOperationID, sdkgo.FailureTransport, "SES templated send outcome could not be confirmed after the send cap"), sdkgo.Receipt{})
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		if checkpointErr := fence.RecordNotSent(); checkpointErr != nil && !errors.Is(checkpointErr, singledispatch.ErrDispatchNotAllowed) {
			return sdkgo.NewMutationUncertain(SentEmail{}, sesFailure(sendTemplatedEmailOperationID, sdkgo.FailureTransport, "SES unsent checkpoint could not be recorded"), sdkgo.Receipt{})
		}
		return templatedSendBranch(SendTemplatedEmailBranchDefect, sdkgo.FailureAuthentication, "SES credentials are unavailable or invalid")
	}
	body, err := json.Marshal(struct {
		FromEmailAddress string `json:"FromEmailAddress"`
		Destination      struct {
			ToAddresses []string `json:"ToAddresses"`
		} `json:"Destination"`
		Content struct {
			Template struct {
				TemplateName string `json:"TemplateName"`
				TemplateData string `json:"TemplateData"`
			} `json:"Template"`
		} `json:"Content"`
	}{FromEmailAddress: input.FromEmailAddress, Destination: struct {
		ToAddresses []string `json:"ToAddresses"`
	}{ToAddresses: []string{input.ToEmailAddress}}, Content: struct {
		Template struct {
			TemplateName string `json:"TemplateName"`
			TemplateData string `json:"TemplateData"`
		} `json:"Template"`
	}{Template: struct {
		TemplateName string `json:"TemplateName"`
		TemplateData string `json:"TemplateData"`
	}{TemplateName: input.TemplateName, TemplateData: string(data)}}})
	if err != nil {
		return templatedSendBranch(SendTemplatedEmailBranchDefect, sdkgo.FailureLocalDefect, "SES templated request could not be encoded")
	}
	send := &sesEmailSend{client: operation.client, call: call, body: body}
	if fence.Dispatch(send.dispatch) != nil {
		return sdkgo.NewMutationUncertain(SentEmail{}, sesFailure(sendTemplatedEmailOperationID, sdkgo.FailureTransport, "SES templated send dispatch could not be confirmed"), sdkgo.Receipt{})
	}
	response := send.response
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationUncertain(SentEmail{}, sesFailure(sendTemplatedEmailOperationID, sdkgo.FailureTransport, "SES resend checkpoint could not be recorded"), sdkgo.Receipt{})
		}
		kind := sdkgo.FailureRateLimit
		if response.status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return sdkgo.NewMutationRetry[SentEmail](sesFailure(sendTemplatedEmailOperationID, kind, "SES asked the connector to retry templated email"), response.retryAfter)
	}
	if errors.Is(response.err, errCredential) {
		return sdkgo.NewMutationUncertain(SentEmail{}, sesFailure(sendTemplatedEmailOperationID, sdkgo.FailureProtocol, "SES templated response repeated a credential"), sdkgo.Receipt{})
	}
	if response.err != nil || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[SentEmail](sesFailure(sendTemplatedEmailOperationID, sdkgo.FailureTransport, "SES templated send outcome is unknown"), 0)
	}
	if response.status != http.StatusOK {
		return templatedSendBranch(SendTemplatedEmailBranchProviderRejected, sdkgo.FailureProviderRejection, "SES rejected templated email")
	}
	var accepted struct {
		MessageID string `json:"MessageId"`
	}
	if json.Unmarshal(response.body, &accepted) != nil || accepted.MessageID == "" || len(accepted.MessageID) > 512 {
		return sdkgo.NewMutationUncertain(SentEmail{}, sesFailure(sendTemplatedEmailOperationID, sdkgo.FailureProtocol, "SES accepted templated email without a usable message ID"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(SendTemplatedEmailBranchSent, SentEmail{MessageID: accepted.MessageID}, nil, sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "amazon-ses", ProviderObjectID: accepted.MessageID})
}

func templatedSendBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[SentEmail] {
	failure := sesFailure(sendTemplatedEmailOperationID, kind, message)
	return sdkgo.NewMutationBranch(branch, SentEmail{}, &failure, sdkgo.Receipt{})
}
