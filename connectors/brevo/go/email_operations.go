// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package brevo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// SendEmailInput supplies one recipient and inline transactional content.
type SendEmailInput struct {
	// SenderEmail is a verified Brevo sender address.
	SenderEmail string `json:"senderEmail"`
	// SenderName is the sender's visible display name.
	SenderName string `json:"senderName"`
	// ToEmail is the recipient address.
	ToEmail string `json:"toEmail"`
	// Subject is the email subject line.
	Subject string `json:"subject"`
	// TextContent is the optional plain text body.
	TextContent string `json:"textContent,omitempty"`
	// HTMLContent is the optional HTML body.
	HTMLContent string `json:"htmlContent,omitempty"`
}

// SendTemplateEmailInput identifies a Brevo template and recipient.
type SendTemplateEmailInput struct {
	// TemplateID is the numeric ID of an active transactional template.
	TemplateID int64 `json:"templateId"`
	// ToEmail is the recipient address.
	ToEmail string `json:"toEmail"`
	// Params supplies template variables without logging their values.
	Params map[string]string `json:"params,omitempty"`
}

// EmailSendResult contains Brevo's non-secret message identifier.
type EmailSendResult struct {
	// MessageID identifies the accepted transactional email.
	MessageID string `json:"messageId"`
}

// SendEmailOperation sends inline transactional content with bounded repeats.
type SendEmailOperation struct{ client *Client }

// SendTemplateEmailOperation sends a configured transactional template with bounded repeats.
type SendTemplateEmailOperation struct{ client *Client }

// SendEmail binds inline email sending to this client.
func (client *Client) SendEmail() SendEmailOperation { return SendEmailOperation{client} }

// SendTemplateEmail binds template email sending to this client.
func (client *Client) SendTemplateEmail() SendTemplateEmailOperation {
	return SendTemplateEmailOperation{client}
}

// Definition returns the generated inline email contract.
func (SendEmailOperation) Definition() sdkgo.MutationDefinition { return SendEmailDefinition }

// Definition returns the generated template email contract.
func (SendTemplateEmailOperation) Definition() sdkgo.MutationDefinition {
	return SendTemplateEmailDefinition
}

// IdempotencyKey preserves the call identity across bounded sends.
func (SendEmailOperation) IdempotencyKey(callID sdkgo.CallID, _ SendEmailInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey preserves the call identity across bounded sends.
func (SendTemplateEmailOperation) IdempotencyKey(callID sdkgo.CallID, _ SendTemplateEmailInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends one inline transactional email.
func (operation SendEmailOperation) Invoke(call sdkgo.Call, input SendEmailInput) sdkgo.MutationAttempt[EmailSendResult] {
	const name = "sendEmail"
	if !validEmail(input.SenderEmail) || !validEmail(input.ToEmail) || strings.TrimSpace(input.Subject) == "" || strings.TrimSpace(input.SenderName) == "" || (input.TextContent == "" && input.HTMLContent == "") {
		return brevoMutationBranch(SendEmailBranchDefect, EmailSendResult{}, name, sdkgo.FailureValidation, "verified sender, recipient, subject, and content are required")
	}
	payload, err := json.Marshal(struct {
		Sender struct {
			Email string `json:"email"`
			Name  string `json:"name"`
		} `json:"sender"`
		To []struct {
			Email string `json:"email"`
		} `json:"to"`
		Subject     string `json:"subject"`
		TextContent string `json:"textContent,omitempty"`
		HTMLContent string `json:"htmlContent,omitempty"`
	}{
		Sender: struct {
			Email string `json:"email"`
			Name  string `json:"name"`
		}{input.SenderEmail, input.SenderName},
		To: []struct {
			Email string `json:"email"`
		}{{input.ToEmail}},
		Subject: input.Subject, TextContent: input.TextContent, HTMLContent: input.HTMLContent,
	})
	if err != nil || len(payload) > 1<<20 {
		return brevoMutationBranch(SendEmailBranchDefect, EmailSendResult{}, name, sdkgo.FailureValidation, "email content is invalid or too large")
	}
	return invokeBrevoEmailSend(call, operation.client, name, payload, SendEmailDefinition.StepDefaults.ExecuteMethodTimeout, SendEmailBranchSent, SendEmailBranchProviderRejected, SendEmailBranchDefect)
}

// Invoke sends one transactional template email.
func (operation SendTemplateEmailOperation) Invoke(call sdkgo.Call, input SendTemplateEmailInput) sdkgo.MutationAttempt[EmailSendResult] {
	const name = "sendTemplateEmail"
	if input.TemplateID <= 0 || !validEmail(input.ToEmail) {
		return brevoMutationBranch(SendTemplateEmailBranchDefect, EmailSendResult{}, name, sdkgo.FailureValidation, "positive template ID and recipient are required")
	}
	for key := range input.Params {
		if strings.TrimSpace(key) == "" {
			return brevoMutationBranch(SendTemplateEmailBranchDefect, EmailSendResult{}, name, sdkgo.FailureValidation, "template parameter names cannot be blank")
		}
	}
	payload, err := json.Marshal(struct {
		TemplateID int64 `json:"templateId"`
		To         []struct {
			Email string `json:"email"`
		} `json:"to"`
		Params map[string]string `json:"params,omitempty"`
	}{input.TemplateID, []struct {
		Email string `json:"email"`
	}{{input.ToEmail}}, input.Params})
	if err != nil || len(payload) > 1<<20 {
		return brevoMutationBranch(SendTemplateEmailBranchDefect, EmailSendResult{}, name, sdkgo.FailureValidation, "template email input is invalid or too large")
	}
	return invokeBrevoEmailSend(call, operation.client, name, payload, SendTemplateEmailDefinition.StepDefaults.ExecuteMethodTimeout, SendTemplateEmailBranchSent, SendTemplateEmailBranchProviderRejected, SendTemplateEmailBranchDefect)
}

func invokeBrevoEmailSend(call sdkgo.Call, client *Client, name string, payload []byte, timeout time.Duration, sent, rejected, defect sdkgo.BranchID) sdkgo.MutationAttempt[EmailSendResult] {
	fence, err := singledispatch.NewFence(call, timeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return brevoMutationBranch(defect, EmailSendResult{}, name, sdkgo.FailureLocalDefect, "email dispatch fence could not be constructed")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(EmailSendResult{}, *brevoFailure(name, sdkgo.FailureTransport, "an earlier email may have been sent"), sdkgo.Receipt{})
	}
	request, failure := client.newRequest(call, http.MethodPost, "/smtp/email", payload, name)
	if failure != nil {
		return sdkgo.NewMutationBranch(defect, EmailSendResult{}, failure, sdkgo.Receipt{})
	}
	var response *http.Response
	err = fence.Dispatch(func(ctx context.Context) error {
		response, err = client.httpClient.Do(request.WithContext(ctx))
		return err
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[EmailSendResult](*brevoFailure(name, sdkgo.FailureAvailability, "email dispatch was not recorded or sent"), 0)
	}
	if response != nil {
		defer func() { _ = response.Body.Close() }()
		if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable {
			if checkpointError := fence.RecordCanResend(); checkpointError != nil {
				return sdkgo.NewMutationRetry[EmailSendResult](*brevoFailure(name, sdkgo.FailureAvailability, "email retry checkpoint failed"), 0)
			}
			return sdkgo.NewMutationRetry[EmailSendResult](*brevoFailure(name, sdkgo.FailureRateLimit, "Brevo asked to retry the email"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
		}
	}
	if err != nil || response == nil || isRetryable(response.StatusCode) {
		if decision.MaximumPriorSends+1 >= call.MaxSends() {
			return sdkgo.NewMutationUncertain(EmailSendResult{}, *brevoFailure(name, sdkgo.FailureTransport, "email outcome is unknown after bounded sends"), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationRetry[EmailSendResult](*brevoFailure(name, sdkgo.FailureTransport, "email outcome is unknown"), 0)
	}
	if response.StatusCode != http.StatusCreated {
		return brevoMutationBranch(rejected, EmailSendResult{}, name, failureKind(response.StatusCode), "Brevo rejected HTTP "+strconv.Itoa(response.StatusCode))
	}
	body, readError := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	var result EmailSendResult
	if readError != nil || json.Unmarshal(body, &result) != nil || result.MessageID == "" {
		return sdkgo.NewMutationUncertain(EmailSendResult{}, *brevoFailure(name, sdkgo.FailureProtocol, "Brevo accepted an email but its message ID could not be confirmed"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(sent, result, nil, sdkgo.Receipt{})
}
