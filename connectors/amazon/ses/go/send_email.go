// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ses

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

const sendEmailOperationID = "sendEmail"

var emailAddressPattern = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

// SendEmailInput sends one bounded plain text message from a verified identity.
type SendEmailInput struct {
	// FromEmailAddress is a verified SES sender shown under Verified identities.
	FromEmailAddress string `json:"fromEmailAddress"`
	// ToEmailAddress is the recipient address; sandbox accounts require a verified recipient.
	ToEmailAddress string `json:"toEmailAddress"`
	// Subject is the UTF-8 email subject, at most 998 bytes.
	Subject string `json:"subject"`
	// TextBody is the UTF-8 plain text body, at most 100 KiB.
	TextBody string `json:"textBody"`
}

// SentEmail is the provider message identity returned after SES accepts the send.
type SentEmail struct {
	// MessageID is the SES message ID; it does not prove delivery.
	MessageID string `json:"messageId"`
}

// SendEmailOperation implements the bounded simple-email send.
type SendEmailOperation struct{ client *Client }

// SendEmail binds this Mutation to the client.
func (client *Client) SendEmail() SendEmailOperation { return SendEmailOperation{client} }

// Definition returns the generated Mutation contract.
func (SendEmailOperation) Definition() sdkgo.MutationDefinition { return SendEmailDefinition }

// IdempotencyKey identifies attempts of one Step, although SES cannot deduplicate it.
func (SendEmailOperation) IdempotencyKey(callID sdkgo.CallID, _ SendEmailInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke fences each dispatch and stops after the configured possible-send cap.
func (operation SendEmailOperation) Invoke(call sdkgo.Call, input SendEmailInput) sdkgo.MutationAttempt[SentEmail] {
	if !emailAddressPattern.MatchString(input.FromEmailAddress) || !emailAddressPattern.MatchString(input.ToEmailAddress) ||
		input.Subject == "" || len(input.Subject) > 998 || input.TextBody == "" || len(input.TextBody) > 100*1024 ||
		!utf8.ValidString(input.Subject) || !utf8.ValidString(input.TextBody) || strings.ContainsAny(input.Subject, "\r\n") {
		return sendEmailBranch(SendEmailBranchDefect, sdkgo.FailureValidation, "SES sender, recipient, subject, or text body is invalid")
	}
	fence, err := singledispatch.NewFence(call, SendEmailDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return sendEmailBranch(SendEmailBranchDefect, sdkgo.FailureLocalDefect, "SES send fence could not be initialized")
	}
	switch fence.Decide().Kind {
	case singledispatch.FailClosed:
		return sdkgo.NewMutationUncertain(SentEmail{}, sesFailure(sendEmailOperationID, sdkgo.FailureTransport, "SES send checkpoint cannot be trusted"), sdkgo.Receipt{})
	case singledispatch.ReadBackRequired:
		return sdkgo.NewMutationUncertain(SentEmail{}, sesFailure(sendEmailOperationID, sdkgo.FailureTransport, "SES send outcome could not be confirmed after the send cap"), sdkgo.Receipt{})
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		if checkpointErr := fence.RecordNotSent(); checkpointErr != nil && !errors.Is(checkpointErr, singledispatch.ErrDispatchNotAllowed) {
			return sdkgo.NewMutationUncertain(SentEmail{}, sesFailure(sendEmailOperationID, sdkgo.FailureTransport, "SES unsent checkpoint could not be recorded"), sdkgo.Receipt{})
		}
		return sendEmailBranch(SendEmailBranchDefect, sdkgo.FailureAuthentication, "SES credentials are unavailable or invalid")
	}
	payload, err := json.Marshal(sesEmailRequest{
		FromEmailAddress: input.FromEmailAddress,
		Destination:      sesEmailDestination{ToAddresses: []string{input.ToEmailAddress}},
		Content: sesEmailContent{Simple: sesSimpleEmail{
			Subject: sesEmailText{Data: input.Subject, Charset: "UTF-8"},
			Body:    sesEmailBody{Text: sesEmailText{Data: input.TextBody, Charset: "UTF-8"}},
		}},
	})
	if err != nil {
		return sendEmailBranch(SendEmailBranchDefect, sdkgo.FailureLocalDefect, "SES email could not be encoded")
	}
	send := &sesEmailSend{client: operation.client, call: call, body: payload}
	if err := fence.Dispatch(send.dispatch); err != nil {
		return sdkgo.NewMutationUncertain(SentEmail{}, sesFailure(sendEmailOperationID, sdkgo.FailureTransport, "SES send dispatch could not be confirmed"), sdkgo.Receipt{})
	}
	response := send.response
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationUncertain(SentEmail{}, sesFailure(sendEmailOperationID, sdkgo.FailureTransport, "SES resend checkpoint could not be recorded"), sdkgo.Receipt{})
		}
		kind := sdkgo.FailureRateLimit
		if response.status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return sdkgo.NewMutationRetry[SentEmail](sesFailure(sendEmailOperationID, kind, "SES asked the connector to retry the email"), response.retryAfter)
	}
	if errors.Is(response.err, errCredential) {
		return sdkgo.NewMutationUncertain(SentEmail{}, sesFailure(sendEmailOperationID, sdkgo.FailureProtocol, "SES response repeated a credential"), sdkgo.Receipt{})
	}
	if response.err != nil || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[SentEmail](sesFailure(sendEmailOperationID, sdkgo.FailureTransport, "SES send outcome is unknown"), 0)
	}
	if response.status != http.StatusOK {
		return sendEmailBranch(SendEmailBranchProviderRejected, sdkgo.FailureProviderRejection, "SES rejected the email")
	}
	var accepted struct {
		MessageID string `json:"MessageId"`
	}
	if json.Unmarshal(response.body, &accepted) != nil || accepted.MessageID == "" || len(accepted.MessageID) > 512 {
		return sdkgo.NewMutationUncertain(SentEmail{}, sesFailure(sendEmailOperationID, sdkgo.FailureProtocol, "SES accepted the email without a usable message ID"), sdkgo.Receipt{})
	}
	receipt := sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "amazon-ses", ProviderObjectID: accepted.MessageID}
	return sdkgo.NewMutationBranch(SendEmailBranchSent, SentEmail{MessageID: accepted.MessageID}, nil, receipt)
}

type sesEmailSend struct {
	client   *Client
	call     sdkgo.Call
	body     []byte
	response providerResponse
}

type sesEmailText struct {
	Data    string `json:"Data"`
	Charset string `json:"Charset"`
}
type sesEmailBody struct {
	Text sesEmailText `json:"Text"`
}
type sesSimpleEmail struct {
	Subject sesEmailText `json:"Subject"`
	Body    sesEmailBody `json:"Body"`
}
type sesEmailContent struct {
	Simple sesSimpleEmail `json:"Simple"`
}
type sesEmailDestination struct {
	ToAddresses []string `json:"ToAddresses"`
}
type sesEmailRequest struct {
	FromEmailAddress string              `json:"FromEmailAddress"`
	Destination      sesEmailDestination `json:"Destination"`
	Content          sesEmailContent     `json:"Content"`
}

func (send *sesEmailSend) dispatch(requestContext context.Context) error {
	send.response = send.client.requestWithContext(send.call, requestContext, http.MethodPost, "/v2/email/outbound-emails", send.body)
	return nil
}

func sendEmailBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[SentEmail] {
	failure := sesFailure(sendEmailOperationID, kind, message)
	return sdkgo.NewMutationBranch(branch, SentEmail{}, &failure, sdkgo.Receipt{})
}
