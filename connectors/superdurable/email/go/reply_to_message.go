// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package email

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	replyToMessageOperation = "replyToMessage"

	replySubjectPrefix = "Re: "
)

// ReplyToMessageInput is one plain-text reply to a message in an IMAP mailbox.
type ReplyToMessageInput struct {
	// Message is a reference from searchMessages; its UIDValidity must still match the mailbox.
	Message MessageReference `json:"message"`
	// Text is the UTF-8 plain-text reply of at most MaxSendTextBytes. The connector does not quote the
	// original message; include any quotation in Text.
	Text string `json:"text"`
	// IsReplyAll also addresses the original To and Cc recipients, except the connection's own sender address.
	IsReplyAll bool `json:"isReplyAll,omitempty"`
	// Cc lists additional bare ASCII copy recipients.
	Cc []string `json:"cc,omitempty"`
}

// ReplyToMessageOperation is the replyToMessage Mutation.
type ReplyToMessageOperation struct {
	client *Client
}

// Definition returns the immutable connector operation definition.
func (ReplyToMessageOperation) Definition() sdkgo.MutationDefinition { return ReplyToMessageDefinition }

// IdempotencyKey uses the stable connector Call ID. SMTP has no idempotency key, so the key only forms the
// Message-ID; single submission comes from a Dex heartbeat checkpoint instead.
func (ReplyToMessageOperation) IdempotencyKey(callID sdkgo.CallID, _ ReplyToMessageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke checks for an earlier submission first, then reads the source headers over IMAP without setting
// \Seen, builds the threaded reply, and submits it at most once per Step execution.
func (operation ReplyToMessageOperation) Invoke(call sdkgo.Call, input ReplyToMessageInput) sdkgo.MutationAttempt[SentMessage] {
	if err := validateReplyToMessageInput(input); err != nil {
		return sdkgo.NewMutationBranch(ReplyToMessageBranchDefect, SentMessage{},
			emailFailurePointer(sdkgo.FailureValidation, replyToMessageOperation, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, replyToMessageOperation)
	if failure != nil {
		return sdkgo.NewMutationBranch(ReplyToMessageBranchDefect, SentMessage{}, failure, sdkgo.Receipt{})
	}
	sender, err := operation.client.messageSender(credentials)
	if err != nil {
		return sdkgo.NewMutationBranch(ReplyToMessageBranchDefect, SentMessage{},
			emailFailurePointer(sdkgo.FailureValidation, replyToMessageOperation, err.Error()), sdkgo.Receipt{})
	}
	messageID, err := deriveMessageID(call.IdempotencyKey, sender.address)
	if err != nil {
		return sdkgo.NewMutationBranch(ReplyToMessageBranchDefect, SentMessage{},
			emailFailurePointer(sdkgo.FailureLocalDefect, replyToMessageOperation, err.Error()), sdkgo.Receipt{})
	}
	// An earlier attempt may have submitted already; the source may since have moved, so read nothing first.
	if hasEarlierSubmissionClaim(call) {
		attempt, _ := submissionAttemptBeforeSend(submissionAlreadyClaimed, replyToMessageOperation,
			SentMessage{MessageID: messageID, From: sender.address})
		return attempt
	}
	ctx, cancel := context.WithTimeout(call.Context, submissionOperationTimeout)
	defer cancel()
	source, readFailure := operation.readReplySource(ctx, credentials, input.Message)
	if readFailure != nil {
		return replyAttemptForReadFailure(readFailure)
	}
	message, err := buildReply(sender, messageID, source, input)
	if err != nil {
		return sdkgo.NewMutationBranch(ReplyToMessageBranchInvalidResponse, SentMessage{},
			emailFailurePointer(sdkgo.FailureProtocol, replyToMessageOperation, err.Error()), sdkgo.Receipt{})
	}
	content, err := message.render()
	if err != nil {
		return sdkgo.NewMutationBranch(ReplyToMessageBranchDefect, SentMessage{},
			emailFailurePointer(sdkgo.FailureLocalDefect, replyToMessageOperation, "the reply could not be rendered"), sdkgo.Receipt{})
	}
	if attempt, isTerminal := submissionAttemptBeforeSend(claimSingleSubmission(call), replyToMessageOperation, message.sentMessage()); isTerminal {
		return attempt
	}
	result := operation.client.submitMessage(ctx, credentials, sender.address, message.envelopeRecipients(), content, replyToMessageOperation)
	return submissionAttemptForResult(call, result, message.sentMessage(), singleSubmissionBranches{
		sent: ReplyToMessageBranchSent, providerRejected: ReplyToMessageBranchProviderRejected,
	})
}

// readReplySource reads the source message's envelope and References header in its own IMAP session.
func (operation ReplyToMessageOperation) readReplySource(ctx context.Context, credentials Credentials, reference MessageReference) (Message, *serverFailure) {
	readCtx, cancel := context.WithTimeout(ctx, imapOperationTimeout)
	defer cancel()
	session, failure := operation.client.openIMAPSession(readCtx, credentials, replyToMessageOperation)
	if failure != nil {
		return Message{}, failure
	}
	defer session.close()
	return readMessage(session, reference, false)
}

// buildReply addresses the reply to Reply-To or From, threads it with In-Reply-To and References, and adds Re:.
func buildReply(sender messageSender, messageID string, source Message, input ReplyToMessageInput) (outgoingMessage, error) {
	primary := source.ReplyTo
	if len(primary) == 0 && source.From.Address != "" {
		primary = []EmailAddress{source.From}
	}
	to := keepReplyRecipients(primary, nil, sender.address)
	if len(to) == 0 {
		return outgoingMessage{}, errors.New("the source message has no Reply-To or From address the connector can reply to")
	}
	var cc []string
	if input.IsReplyAll {
		cc = keepReplyRecipients(append(append([]EmailAddress(nil), source.To...), source.Cc...), to, sender.address)
	}
	cc = append(cc, keepAdditionalRecipients(input.Cc, append(append([]string(nil), to...), cc...))...)
	if total := len(to) + len(cc); total > MaxRecipients {
		return outgoingMessage{}, fmt.Errorf("the reply would have %d recipients; at most %d are allowed", total, MaxRecipients)
	}
	message := outgoingMessage{
		fromAddress: sender.address, fromName: sender.name, to: to, cc: cc, subject: buildReplySubject(source.Subject),
		text: input.Text, messageID: messageID,
	}
	if isMessageIdentifier(source.MessageID) {
		message.inReplyTo = source.MessageID
		references := append(append([]string(nil), source.References...), source.MessageID)
		if len(references) > maximumReferences {
			references = references[len(references)-maximumReferences:]
		}
		message.references = references
	}
	message.date = currentDateHeaderTime()
	return message, nil
}

// keepReplyRecipients keeps bare ASCII addresses that are not the sender and not already addressed.
func keepReplyRecipients(candidates []EmailAddress, alreadyAddressed []string, sender string) []string {
	seen := map[string]bool{strings.ToLower(sender): true}
	for _, address := range alreadyAddressed {
		seen[strings.ToLower(address)] = true
	}
	var kept []string
	for _, candidate := range candidates {
		canonical := strings.ToLower(candidate.Address)
		if seen[canonical] || !isBareEmailAddress(candidate.Address) {
			continue
		}
		seen[canonical] = true
		kept = append(kept, candidate.Address)
	}
	return kept
}

// keepAdditionalRecipients keeps the input's extra Cc addresses that are not already addressed.
func keepAdditionalRecipients(additional []string, alreadyAddressed []string) []string {
	seen := map[string]bool{}
	for _, address := range alreadyAddressed {
		seen[strings.ToLower(address)] = true
	}
	var kept []string
	for _, address := range additional {
		if seen[strings.ToLower(address)] {
			continue
		}
		seen[strings.ToLower(address)] = true
		kept = append(kept, address)
	}
	return kept
}

// buildReplySubject adds Re: unless the subject already starts with it in any letter case.
func buildReplySubject(subject string) string {
	trimmed := strings.TrimSpace(subject)
	if len(trimmed) >= 3 && strings.EqualFold(trimmed[:3], "re:") {
		return truncateUTF8(trimmed, MaxSubjectBytes)
	}
	return truncateUTF8(strings.TrimSpace(replySubjectPrefix+trimmed), MaxSubjectBytes)
}

func validateReplyToMessageInput(input ReplyToMessageInput) error {
	if err := validateMessageReference("message", input.Message); err != nil {
		return err
	}
	if len(input.Cc) > MaxRecipients {
		return fmt.Errorf("cc holds %d recipients; at most %d are allowed", len(input.Cc), MaxRecipients)
	}
	return errors.Join(validateRecipientList("cc", input.Cc), validateBodyText("text", input.Text))
}

// replyAttemptForReadFailure maps a failed source read; nothing was submitted, so nothing is uncertain.
func replyAttemptForReadFailure(failure *serverFailure) sdkgo.MutationAttempt[SentMessage] {
	switch failure.outcome {
	case outcomeRetryable:
		return sdkgo.NewMutationRetry[SentMessage](failure.failure, 0)
	case outcomeNotFound:
		return sdkgo.NewMutationBranch(ReplyToMessageBranchNotFound, SentMessage{}, &failure.failure, sdkgo.Receipt{})
	case outcomeInvalidResponse:
		return sdkgo.NewMutationBranch(ReplyToMessageBranchInvalidResponse, SentMessage{}, &failure.failure, sdkgo.Receipt{})
	default:
		return sdkgo.NewMutationBranch(ReplyToMessageBranchProviderRejected, SentMessage{}, &failure.failure, sdkgo.Receipt{})
	}
}
