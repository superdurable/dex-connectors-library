// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sendgrid

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// SendMailInput describes one transactional email without binary attachments.
type SendMailInput struct {
	// From is a verified SendGrid sender email address.
	From string `json:"from"`
	// To contains one or more recipient email addresses.
	To []string `json:"to"`
	// CC contains optional copy recipients.
	CC []string `json:"cc,omitempty"`
	// BCC contains optional blind-copy recipients.
	BCC []string `json:"bcc,omitempty"`
	// Subject is the nonblank email subject.
	Subject string `json:"subject"`
	// Text is the plain-text body; blank is allowed when HTML is nonblank.
	Text string `json:"text,omitempty"`
	// HTML is the HTML body; blank is allowed when Text is nonblank.
	HTML string `json:"html,omitempty"`
}

// SendMailOutput reports that SendGrid accepted the email for delivery.
type SendMailOutput struct {
	// Accepted means SendGrid returned HTTP 202; it does not prove delivery.
	Accepted bool `json:"accepted"`
}

// SendMailOperation sends one transactional email through SendGrid.
type SendMailOperation struct{ client *Client }

// Definition returns the generated operation contract.
func (SendMailOperation) Definition() sdkgo.MutationDefinition { return SendMailDefinition }

// IdempotencyKey derives the stable receipt key from the call ID.
func (SendMailOperation) IdempotencyKey(callID sdkgo.CallID, _ SendMailInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// SendMail binds the operation to this client.
func (client *Client) SendMail() SendMailOperation { return SendMailOperation{client} }

// Invoke bounds possible duplicate sends to the Step's maximum send count.
func (operation SendMailOperation) Invoke(call sdkgo.Call, input SendMailInput) sdkgo.MutationAttempt[SendMailOutput] {
	if !isValidMailInput(input) {
		return mutationBranch("sendMail", SendMailBranchDefect, SendMailOutput{}, sdkgo.FailureValidation, "mail sender, recipients, subject, or content is invalid")
	}
	fence, err := singledispatch.NewFence(call, SendMailDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return mutationBranch("sendMail", SendMailBranchDefect, SendMailOutput{}, sdkgo.FailureLocalDefect, "mail dispatch fence could not be built")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return uncertainMail(call, "an earlier SendGrid mail send may have succeeded")
	}
	payload := encodeMailPayload(input)
	var status int
	var retryAfter time.Duration
	err = fence.Dispatch(func(requestContext context.Context) error {
		var requestErr error
		_, status, retryAfter, requestErr = operation.client.doWithContext(requestContext, call, http.MethodPost, "/v3/mail/send", nil, payload)
		return requestErr
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[SendMailOutput](queryFailure("sendMail", sdkgo.FailureAvailability, "mail request was not sent"), 0)
	}
	if status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable || errors.Is(err, errCredential) {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[SendMailOutput](queryFailure("sendMail", sdkgo.FailureAvailability, "mail resend checkpoint failed"), 0)
		}
		return sdkgo.NewMutationRetry[SendMailOutput](queryFailure("sendMail", sdkgo.FailureAvailability, "mail request was not processed"), retryAfter)
	}
	if err != nil || status >= 500 {
		if decision.MaximumPriorSends+1 < call.MaxSends() {
			return sdkgo.NewMutationRetry[SendMailOutput](queryFailure("sendMail", sdkgo.FailureTransport, "mail send outcome is unknown"), 0)
		}
		return uncertainMail(call, "mail send outcome is unknown after bounded sends")
	}
	if status == http.StatusAccepted {
		return sdkgo.NewMutationBranch(SendMailBranchAccepted, SendMailOutput{Accepted: true}, nil, sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "sendgrid"})
	}
	if status >= 200 && status < 300 || decision.MaximumPriorSends > 0 {
		return uncertainMail(call, "an earlier or unexpected SendGrid mail send may have succeeded")
	}
	return mutationBranch("sendMail", SendMailBranchProviderRejected, SendMailOutput{}, sdkgo.FailureProviderRejection, "provider rejected the email request")
}

func isValidMailInput(input SendMailInput) bool {
	if !isMailAddress(input.From) || len(input.To) == 0 || len(input.To)+len(input.CC)+len(input.BCC) > 1000 ||
		strings.TrimSpace(input.Subject) == "" || len(input.Subject) > 998 ||
		(input.Text == "" && input.HTML == "") || len(input.Text)+len(input.HTML) > 1<<20 {
		return false
	}
	for _, address := range append(append(append([]string{}, input.To...), input.CC...), input.BCC...) {
		if !isMailAddress(address) {
			return false
		}
	}
	return true
}

func isMailAddress(value string) bool {
	parsed, err := mail.ParseAddress(value)
	return err == nil && parsed.Address == value && len(value) <= 254
}

func encodeMailPayload(input SendMailInput) []byte {
	addresses := func(values []string) []map[string]string {
		result := make([]map[string]string, 0, len(values))
		for _, value := range values {
			result = append(result, map[string]string{"email": value})
		}
		return result
	}
	personalization := map[string]any{"to": addresses(input.To)}
	if len(input.CC) > 0 {
		personalization["cc"] = addresses(input.CC)
	}
	if len(input.BCC) > 0 {
		personalization["bcc"] = addresses(input.BCC)
	}
	content := make([]map[string]string, 0, 2)
	if input.Text != "" {
		content = append(content, map[string]string{"type": "text/plain", "value": input.Text})
	}
	if input.HTML != "" {
		content = append(content, map[string]string{"type": "text/html", "value": input.HTML})
	}
	payload, _ := json.Marshal(map[string]any{"personalizations": []any{personalization}, "from": map[string]string{"email": input.From}, "subject": input.Subject, "content": content})
	return payload
}

func uncertainMail(call sdkgo.Call, message string) sdkgo.MutationAttempt[SendMailOutput] {
	return sdkgo.NewMutationUncertain(SendMailOutput{}, queryFailure("sendMail", sdkgo.FailureTransport, message), sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "sendgrid"})
}
