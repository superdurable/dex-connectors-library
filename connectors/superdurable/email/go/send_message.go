// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package email

import (
	"context"
	"errors"
	"fmt"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const sendMessageOperation = "sendMessage"

// SendMessageInput is one new plain-text message.
type SendMessageInput struct {
	// To lists bare ASCII recipient addresses; at least one is required.
	To []string `json:"to"`
	// Cc lists bare ASCII copy recipients.
	Cc []string `json:"cc,omitempty"`
	// Bcc lists blind-copy recipients; they receive the message but appear in no header.
	Bcc []string `json:"bcc,omitempty"`
	// ReplyTo is an optional bare address that replies should go to instead of the sender.
	ReplyTo string `json:"replyTo,omitempty"`
	// Subject is one line of at most MaxSubjectBytes; non-ASCII text is encoded for mail headers.
	Subject string `json:"subject"`
	// Text is the UTF-8 plain-text body of at most MaxSendTextBytes. It is sent as quoted-printable.
	Text string `json:"text"`
}

// SendMessageOperation is the sendMessage Mutation.
type SendMessageOperation struct {
	client *Client
}

// Definition returns the immutable connector operation definition.
func (SendMessageOperation) Definition() sdkgo.MutationDefinition { return SendMessageDefinition }

// IdempotencyKey uses the stable connector Call ID. SMTP has no idempotency key, so the key only forms the
// Message-ID, which a resubmission repeats; a Dex heartbeat checkpoint counts the submissions instead.
func (SendMessageOperation) IdempotencyKey(callID sdkgo.CallID, _ SendMessageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke submits the message, at most call.MaxSends() times per Step execution. A temporary refusal or a
// connection that fails before the final dot is retried and not counted. An answer lost after the final
// dot is retried while the send cap allows another submission and selects uncertain at the cap.
func (operation SendMessageOperation) Invoke(call sdkgo.Call, input SendMessageInput) sdkgo.MutationAttempt[SentMessage] {
	if err := validateSendMessageInput(input); err != nil {
		return sdkgo.NewMutationBranch(SendMessageBranchDefect, SentMessage{},
			emailFailurePointer(sdkgo.FailureValidation, sendMessageOperation, err.Error()), sdkgo.Receipt{})
	}
	decision := decideSubmission(call)
	credentials, failure := operation.client.resolveCredentials(call, sendMessageOperation)
	if failure != nil {
		if attempt, isTerminal := submissionAttemptBeforeSend(decision, sendMessageOperation, SentMessage{}); isTerminal {
			return attempt
		}
		return refuseBeforeSubmitting(decision, SendMessageBranchDefect, *failure, sendMessageOperation, SentMessage{})
	}
	sender, err := operation.client.messageSender(credentials)
	if err != nil {
		return operation.refuseLocally(decision, sdkgo.FailureValidation, err.Error(), SentMessage{})
	}
	message, err := buildNewMessage(sender, call.IdempotencyKey, input)
	if err != nil {
		return operation.refuseLocally(decision, sdkgo.FailureValidation, err.Error(), SentMessage{From: sender.address})
	}
	content, err := message.render()
	if err != nil {
		return operation.refuseLocally(decision, sdkgo.FailureLocalDefect, "the message could not be rendered", message.sentMessage())
	}
	if attempt, isTerminal := submissionAttemptBeforeSend(decision, sendMessageOperation, message.sentMessage()); isTerminal {
		return attempt
	}
	ctx, cancel := context.WithTimeout(call.Context, submissionOperationTimeout)
	defer cancel()
	return submitOnce(call, decision, sendMessageOperation, message.sentMessage(), singleSubmissionBranches{
		sent: SendMessageBranchSent, providerRejected: SendMessageBranchProviderRejected,
	}, func() submissionResult {
		return operation.client.submitMessage(ctx, credentials, message.fromAddress, message.envelopeRecipients(), content, sendMessageOperation)
	})
}

// refuseLocally selects defect for a local failure before submitting, unless the checkpoint decides first.
func (SendMessageOperation) refuseLocally(
	decision submissionDecision, kind sdkgo.FailureKind, message string, sent SentMessage,
) sdkgo.MutationAttempt[SentMessage] {
	if attempt, isTerminal := submissionAttemptBeforeSend(decision, sendMessageOperation, sent); isTerminal {
		return attempt
	}
	return refuseBeforeSubmitting(decision, SendMessageBranchDefect, emailFailure(kind, sendMessageOperation, message), sendMessageOperation, sent)
}

// buildNewMessage combines input with the connection's sender and the Step's stable Message-ID.
func buildNewMessage(sender messageSender, key sdkgo.IdempotencyKey, input SendMessageInput) (outgoingMessage, error) {
	messageID, err := deriveMessageID(key, sender.address)
	if err != nil {
		return outgoingMessage{}, err
	}
	return outgoingMessage{
		fromAddress: sender.address, fromName: sender.name, to: input.To, cc: input.Cc, bcc: input.Bcc, replyTo: input.ReplyTo,
		subject: input.Subject, text: input.Text, messageID: messageID, date: currentDateHeaderTime(),
	}, nil
}

func validateSendMessageInput(input SendMessageInput) error {
	if len(input.To) == 0 {
		return errors.New("to requires at least one recipient")
	}
	if total := len(input.To) + len(input.Cc) + len(input.Bcc); total > MaxRecipients {
		return fmt.Errorf("to, cc, and bcc hold %d recipients; at most %d are allowed", total, MaxRecipients)
	}
	if input.ReplyTo != "" && !isBareEmailAddress(input.ReplyTo) {
		return errors.New("replyTo must be one bare ASCII address such as support@example.com")
	}
	return errors.Join(
		validateRecipientList("to", input.To), validateRecipientList("cc", input.Cc), validateRecipientList("bcc", input.Bcc),
		validateSubject("subject", input.Subject), validateBodyText("text", input.Text),
	)
}
