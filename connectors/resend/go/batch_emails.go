// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package resend

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// SendBatchEmailsInput contains up to 100 emails to send together.
type SendBatchEmailsInput struct {
	// Emails are the keyed batch messages, in response order.
	Emails []SendEmailInput `json:"emails"`
}

// SentEmailPage contains the provider IDs for one accepted batch.
type SentEmailPage struct {
	// Data contains one ID for every input email in the same order.
	Data []SentEmail `json:"data"`
}

// SendBatchEmailsOperation sends one keyed batch.
type SendBatchEmailsOperation struct{ client *Client }

// SendBatchEmails binds batch sending to this client.
func (client *Client) SendBatchEmails() SendBatchEmailsOperation {
	return SendBatchEmailsOperation{client}
}

// Definition returns the generated sendBatchEmails contract.
func (SendBatchEmailsOperation) Definition() sdkgo.MutationDefinition {
	return SendBatchEmailsDefinition
}

// IdempotencyKey supplies the stable provider request key.
func (SendBatchEmailsOperation) IdempotencyKey(callID sdkgo.CallID, _ SendBatchEmailsInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends one ordered batch under a provider idempotency key.
func (operation SendBatchEmailsOperation) Invoke(call sdkgo.Call, input SendBatchEmailsInput) sdkgo.MutationAttempt[SentEmailPage] {
	if len(input.Emails) == 0 || len(input.Emails) > 100 {
		return mutationFailure[SentEmailPage]("sendBatchEmails", SendBatchEmailsBranchDefect, sdkgo.FailureValidation, "batch must contain 1 to 100 emails")
	}
	for _, email := range input.Emails {
		if strings.TrimSpace(email.From) == "" || len(email.To) == 0 || len(email.To) > 50 || strings.TrimSpace(email.Subject) == "" || strings.TrimSpace(email.HTML) == "" {
			return mutationFailure[SentEmailPage]("sendBatchEmails", SendBatchEmailsBranchDefect, sdkgo.FailureValidation, "each batch email needs a sender, 1 to 50 recipients, subject, and HTML")
		}
		for _, recipient := range email.To {
			if strings.TrimSpace(recipient) == "" {
				return mutationFailure[SentEmailPage]("sendBatchEmails", SendBatchEmailsBranchDefect, sdkgo.FailureValidation, "batch recipient cannot be blank")
			}
		}
	}
	if call.IdempotencyKey == "" || len(call.IdempotencyKey) > 256 {
		return mutationFailure[SentEmailPage]("sendBatchEmails", SendBatchEmailsBranchDefect, sdkgo.FailureValidation, "idempotency key is invalid")
	}
	body, err := json.Marshal(input.Emails)
	if err != nil {
		return mutationFailure[SentEmailPage]("sendBatchEmails", SendBatchEmailsBranchDefect, sdkgo.FailureValidation, "batch cannot be encoded")
	}
	return runMutation(operation.client, call, "sendBatchEmails", http.MethodPost, "/emails/batch", body, "Idempotency-Key", string(call.IdempotencyKey), SendBatchEmailsBranchSent, "", SendBatchEmailsBranchProviderRejected, SendBatchEmailsBranchInvalidResponse, SendBatchEmailsBranchDefect, func(body []byte) (SentEmailPage, error) {
		var page SentEmailPage
		if err := json.Unmarshal(body, &page); err != nil {
			return SentEmailPage{}, err
		}
		if len(page.Data) != len(input.Emails) {
			return SentEmailPage{}, fmt.Errorf("batch response count differs from input")
		}
		for _, email := range page.Data {
			if email.ID == "" {
				return SentEmailPage{}, fmt.Errorf("batch response email ID is missing")
			}
		}
		return page, nil
	})
}
