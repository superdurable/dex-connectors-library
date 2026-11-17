// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package postmark

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// SendEmailInput describes one plain text transactional message.
type SendEmailInput struct {
	// From is a sender signature verified in the Postmark server.
	From string `json:"from"`
	// To is one recipient email address.
	To string `json:"to"`
	// Subject is the message subject.
	Subject string `json:"subject"`
	// TextBody is the plain text email body.
	TextBody string `json:"textBody"`
}

// SentEmail identifies one message accepted by Postmark.
type SentEmail struct {
	// MessageID is Postmark's outbound message identifier.
	MessageID string `json:"MessageID"`
	// To is the accepted recipient.
	To string `json:"To"`
}

// SendEmailOperation sends a transactional message through this client.
type SendEmailOperation struct{ client *Client }

// SendEmail binds the email send operation to this client.
func (client *Client) SendEmail() SendEmailOperation { return SendEmailOperation{client} }

// Definition returns the generated sendEmail contract.
func (SendEmailOperation) Definition() sdkgo.MutationDefinition { return SendEmailDefinition }

// IdempotencyKey supplies a stable search marker; Postmark does not deduplicate on this key.
func (SendEmailOperation) IdempotencyKey(callID sdkgo.CallID, _ SendEmailInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends at most one request per attempt and checks activity before any repeat.
func (operation SendEmailOperation) Invoke(call sdkgo.Call, input SendEmailInput) sdkgo.MutationAttempt[SentEmail] {
	if !validEmailAddress(input.From) || !validEmailAddress(input.To) || strings.TrimSpace(input.Subject) == "" || strings.TrimSpace(input.TextBody) == "" {
		return mutationFailure[SentEmail]("sendEmail", SendEmailBranchDefect, sdkgo.FailureValidation, "sender, recipient, subject, and text body are required")
	}
	fence, err := singledispatch.NewFence(call, SendEmailDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return mutationFailure[SentEmail]("sendEmail", SendEmailBranchDefect, sdkgo.FailureLocalDefect, "email dispatch fence is unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return sdkgo.NewMutationUncertain(SentEmail{}, newFailure("sendEmail", sdkgo.FailureTransport, "email dispatch checkpoint cannot be read"), sdkgo.Receipt{})
	}
	if decision.Kind != singledispatch.SendAllowed {
		found, confirmed, readErr := operation.client.findSentEmail(call, string(call.IdempotencyKey), input)
		if readErr != nil {
			return sdkgo.NewMutationRetry[SentEmail](newFailure("sendEmail", sdkgo.FailureAvailability, "outbound activity could not be checked"), 0)
		}
		if confirmed {
			return sdkgo.NewMutationBranch(SendEmailBranchSent, found, nil, sdkgo.Receipt{})
		}
		if decision.Kind == singledispatch.ReadBackRequired {
			return sdkgo.NewMutationUncertain(SentEmail{}, newFailure("sendEmail", sdkgo.FailureTransport, "email may have been sent but activity did not confirm it"), sdkgo.Receipt{})
		}
	}
	payload, err := json.Marshal(map[string]any{
		"From": input.From, "To": input.To, "Subject": input.Subject, "TextBody": input.TextBody,
		"Metadata": map[string]string{"dex_call_id": string(call.IdempotencyKey)},
	})
	if err != nil {
		return mutationFailure[SentEmail]("sendEmail", SendEmailBranchDefect, sdkgo.FailureLocalDefect, "email request cannot be encoded")
	}
	request, err := operation.client.newRequest(call, http.MethodPost, "/email", payload)
	if err != nil {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(SentEmail{}, newFailure("sendEmail", sdkgo.FailureAuthentication, "an earlier email send may have succeeded"), sdkgo.Receipt{})
		}
		return mutationFailure[SentEmail]("sendEmail", SendEmailBranchDefect, sdkgo.FailureAuthentication, "connection credentials or request are invalid")
	}
	var response *http.Response
	err = fence.Dispatch(func(requestContext context.Context) error {
		var requestErr error
		response, requestErr = operation.client.httpClient.Do(request.WithContext(requestContext))
		return requestErr
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[SentEmail](newFailure("sendEmail", sdkgo.FailureAvailability, "email dispatch did not start"), 0)
	}
	if err != nil {
		return sdkgo.NewMutationRetry[SentEmail](newFailure("sendEmail", sdkgo.FailureTransport, "email send outcome is unknown"), 0)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[SentEmail](newFailure("sendEmail", sdkgo.FailureAvailability, "email retry checkpoint could not be recorded"), 0)
		}
		kind := sdkgo.FailureAvailability
		if response.StatusCode == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return sdkgo.NewMutationRetry[SentEmail](newFailure("sendEmail", kind, "Postmark temporarily refused the send"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	}
	if response.StatusCode >= 500 || response.StatusCode == http.StatusRequestTimeout {
		return sdkgo.NewMutationRetry[SentEmail](newFailure("sendEmail", sdkgo.FailureAvailability, "email send outcome is unknown"), 0)
	}
	if response.StatusCode != http.StatusOK {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(SentEmail{}, newFailure("sendEmail", sdkgo.FailureProviderRejection, "a prior email send may have succeeded before this refusal"), sdkgo.Receipt{})
		}
		return mutationFailure[SentEmail]("sendEmail", SendEmailBranchProviderRejected, sdkgo.FailureProviderRejection, fmt.Sprintf("Postmark refused the email with HTTP %d", response.StatusCode))
	}
	content, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if err != nil {
		return sdkgo.NewMutationRetry[SentEmail](newFailure("sendEmail", sdkgo.FailureTransport, "email acceptance response was interrupted"), 0)
	}
	var sent SentEmail
	if err := json.Unmarshal(content, &sent); err != nil || sent.MessageID == "" {
		return sdkgo.NewMutationRetry[SentEmail](newFailure("sendEmail", sdkgo.FailureProtocol, "email acceptance response was incomplete"), 0)
	}
	if err := fence.RecordProviderID(sent.MessageID); err != nil {
		return sdkgo.NewMutationRetry[SentEmail](newFailure("sendEmail", sdkgo.FailureAvailability, "accepted email checkpoint could not be recorded"), 0)
	}
	return sdkgo.NewMutationBranch(SendEmailBranchSent, sent, nil, sdkgo.Receipt{})
}

func (client *Client) findSentEmail(call sdkgo.Call, marker string, input SendEmailInput) (SentEmail, bool, error) {
	query := url.Values{"count": {"50"}, "offset": {"0"}, "metadata_dex_call_id": {marker}}
	request, err := client.newRequest(call, http.MethodGet, "/messages/outbound?"+query.Encode(), nil)
	if err != nil {
		return SentEmail{}, false, err
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return SentEmail{}, false, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return SentEmail{}, false, fmt.Errorf("outbound activity returned HTTP %d", response.StatusCode)
	}
	content, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if err != nil {
		return SentEmail{}, false, err
	}
	var page struct {
		Messages []struct {
			MessageID string `json:"MessageID"`
			To        []struct {
				Email string `json:"Email"`
			} `json:"To"`
			Subject string `json:"Subject"`
		} `json:"Messages"`
	}
	if err := json.Unmarshal(content, &page); err != nil || page.Messages == nil {
		return SentEmail{}, false, fmt.Errorf("outbound activity response is invalid")
	}
	for _, message := range page.Messages {
		if message.MessageID != "" && message.Subject == input.Subject {
			for _, recipient := range message.To {
				if recipient.Email == input.To {
					return SentEmail{MessageID: message.MessageID, To: input.To}, true, nil
				}
			}
		}
	}
	return SentEmail{}, false, nil
}

func validEmailAddress(address string) bool {
	parsed, err := mail.ParseAddress(address)
	return err == nil && parsed.Address == address
}
