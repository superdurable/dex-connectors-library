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
// Message-ID; single submission comes from a Dex heartbeat checkpoint instead.
func (SendMessageOperation) IdempotencyKey(callID sdkgo.CallID, _ SendMessageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke submits the message at most once per Step execution. A temporary refusal or a connection that
// fails before the final dot is retried; an answer lost after the final dot selects uncertain.
func (operation SendMessageOperation) Invoke(call sdkgo.Call, input SendMessageInput) sdkgo.MutationAttempt[SentMessage] {
	if err := validateSendMessageInput(input); err != nil {
		return sdkgo.NewMutationBranch(SendMessageBranchDefect, SentMessage{},
			emailFailurePointer(sdkgo.FailureValidation, sendMessageOperation, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, sendMessageOperation)
	if failure != nil {
		return sdkgo.NewMutationBranch(SendMessageBranchDefect, SentMessage{}, failure, sdkgo.Receipt{})
	}
	sender, err := operation.client.messageSender(credentials)
	if err != nil {
		return sdkgo.NewMutationBranch(SendMessageBranchDefect, SentMessage{},
			emailFailurePointer(sdkgo.FailureValidation, sendMessageOperation, err.Error()), sdkgo.Receipt{})
	}
	message, err := buildNewMessage(sender, call.IdempotencyKey, input)
	if err != nil {
		return sdkgo.NewMutationBranch(SendMessageBranchDefect, SentMessage{},
			emailFailurePointer(sdkgo.FailureValidation, sendMessageOperation, err.Error()), sdkgo.Receipt{})
	}
	content, err := message.render()
	if err != nil {
		return sdkgo.NewMutationBranch(SendMessageBranchDefect, SentMessage{},
			emailFailurePointer(sdkgo.FailureLocalDefect, sendMessageOperation, "the message could not be rendered"), sdkgo.Receipt{})
	}
	if attempt, isTerminal := submissionAttemptBeforeSend(claimSingleSubmission(call), sendMessageOperation, message.sentMessage()); isTerminal {
		return attempt
	}
	ctx, cancel := context.WithTimeout(call.Context, submissionOperationTimeout)
	defer cancel()
	result := operation.client.submitMessage(ctx, credentials, message.fromAddress, message.envelopeRecipients(), content, sendMessageOperation)
	return submissionAttemptForResult(call, result, message.sentMessage(), singleSubmissionBranches{
		sent: SendMessageBranchSent, providerRejected: SendMessageBranchProviderRejected,
	})
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
