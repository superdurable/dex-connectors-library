// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package email

import (
	"context"
	"errors"

	"github.com/emersion/go-imap/v2"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const moveMessageOperation = "moveMessage"

// MoveMessageInput moves one message to another mailbox.
type MoveMessageInput struct {
	// Message is a reference from searchMessages; its UIDValidity must still match the source mailbox.
	Message MessageReference `json:"message"`
	// DestinationMailbox is an existing mailbox, such as Archive; the connector never creates one.
	DestinationMailbox string `json:"destinationMailbox"`
	// MessageID is the message's Message-ID without angle brackets, from searchMessages. When set, it must
	// match the message at the UID, and an attempt that finds the UID gone looks for this Message-ID in the
	// destination and selects moved instead of notFound. Blank skips both checks.
	MessageID string `json:"messageId,omitempty"`
}

// MovedMessage is the message's place in the destination mailbox.
type MovedMessage struct {
	// Destination identifies the message in the destination mailbox. Its UID and UIDValidity are zero when
	// the server reports no COPYUID, which servers without UIDPLUS omit.
	Destination MessageReference `json:"destination"`
	// WasAlreadyMoved reports that an earlier attempt had moved the message, found by its Message-ID.
	WasAlreadyMoved bool `json:"wasAlreadyMoved,omitempty"`
}

// MoveMessageOperation is the moveMessage Mutation.
type MoveMessageOperation struct {
	client *Client
}

// Definition returns the immutable connector operation definition.
func (MoveMessageOperation) Definition() sdkgo.MutationDefinition { return MoveMessageDefinition }

// IdempotencyKey uses the stable connector Call ID; IMAP has no key, and a move cannot apply twice.
func (MoveMessageOperation) IdempotencyKey(callID sdkgo.CallID, _ MoveMessageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke moves with UID MOVE, or with UID COPY, STORE \Deleted, and UID EXPUNGE on a UIDPLUS server
// without MOVE. It never runs a plain EXPUNGE, which would remove other deleted messages.
func (operation MoveMessageOperation) Invoke(call sdkgo.Call, input MoveMessageInput) sdkgo.MutationAttempt[MovedMessage] {
	if err := validateMoveMessageInput(input); err != nil {
		return sdkgo.NewMutationBranch(MoveMessageBranchDefect, MovedMessage{},
			emailFailurePointer(sdkgo.FailureValidation, moveMessageOperation, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, moveMessageOperation)
	if failure != nil {
		return sdkgo.NewMutationBranch(MoveMessageBranchDefect, MovedMessage{}, failure, sdkgo.Receipt{})
	}
	ctx, cancel := context.WithTimeout(call.Context, imapOperationTimeout)
	defer cancel()
	session, sessionFailure := operation.client.openIMAPSession(ctx, credentials, moveMessageOperation)
	if sessionFailure != nil {
		return moveAttemptForFailure(sessionFailure)
	}
	defer session.close()
	capabilities := session.client.Caps()
	if !capabilities.Has(imap.CapMove) && !capabilities.Has(imap.CapUIDPlus) {
		return sdkgo.NewMutationBranch(MoveMessageBranchProviderRejected, MovedMessage{},
			emailFailurePointer(sdkgo.FailureProviderRejection, moveMessageOperation,
				"the IMAP server supports neither MOVE nor UIDPLUS, so the message cannot be moved without expunging other messages"),
			sdkgo.Receipt{})
	}
	if _, sessionFailure := session.selectMailbox(input.Message.Mailbox, false, input.Message.UIDValidity); sessionFailure != nil {
		return moveAttemptForFailure(sessionFailure)
	}
	uid := imap.UID(input.Message.UID)
	fetched, sessionFailure := session.fetchMessages([]imap.UID{uid}, &imap.FetchOptions{UID: true, Envelope: true})
	if sessionFailure != nil {
		return moveAttemptForFailure(sessionFailure)
	}
	source := findFetchedMessage(fetched, uid)
	if source == nil {
		return operation.findAlreadyMovedMessage(session, input)
	}
	if input.MessageID != "" && (source.Envelope == nil || source.Envelope.MessageID != input.MessageID) {
		return sdkgo.NewMutationBranch(MoveMessageBranchNotFound, MovedMessage{},
			emailFailurePointer(sdkgo.FailureNotFound, moveMessageOperation, "the message at the UID has a different Message-ID"), sdkgo.Receipt{})
	}
	moved, err := session.client.Move(imap.UIDSetNum(uid), input.DestinationMailbox).Wait()
	if err != nil {
		return moveAttemptForFailure(session.classify("MOVE", err))
	}
	output := MovedMessage{Destination: MessageReference{Mailbox: input.DestinationMailbox}}
	if destinationUIDs, isUIDSet := moved.DestUIDs.(imap.UIDSet); isUIDSet && !destinationUIDs.Dynamic() {
		if numbers, _ := destinationUIDs.Nums(); len(numbers) == 1 && moved.UIDValidity != 0 {
			output.Destination.UIDValidity, output.Destination.UID = moved.UIDValidity, uint32(numbers[0])
		}
	}
	return sdkgo.NewMutationBranch(MoveMessageBranchMoved, output, nil, sdkgo.Receipt{ProviderObjectID: input.MessageID})
}

// findAlreadyMovedMessage looks for the expected Message-ID in the destination after the UID was not found.
func (operation MoveMessageOperation) findAlreadyMovedMessage(session *imapSession, input MoveMessageInput) sdkgo.MutationAttempt[MovedMessage] {
	if input.MessageID == "" {
		return sdkgo.NewMutationBranch(MoveMessageBranchNotFound, MovedMessage{}, emailFailurePointer(sdkgo.FailureNotFound, moveMessageOperation,
			"no message in the source mailbox has the UID; set messageId so a repeated move can find it in the destination"), sdkgo.Receipt{})
	}
	destination, sessionFailure := session.selectMailbox(input.DestinationMailbox, true, 0)
	if sessionFailure != nil {
		if sessionFailure.outcome == outcomeNotFound {
			return sdkgo.NewMutationBranch(MoveMessageBranchNotFound, MovedMessage{}, &sessionFailure.failure, sdkgo.Receipt{})
		}
		return moveAttemptForFailure(sessionFailure)
	}
	uid, sessionFailure := findMessageUIDByMessageID(session, input.MessageID)
	if sessionFailure != nil {
		return moveAttemptForFailure(sessionFailure)
	}
	if uid == 0 {
		return sdkgo.NewMutationBranch(MoveMessageBranchNotFound, MovedMessage{}, emailFailurePointer(sdkgo.FailureNotFound, moveMessageOperation,
			"no message has the UID in the source mailbox or the Message-ID in the destination"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(MoveMessageBranchMoved, MovedMessage{
		Destination:     MessageReference{Mailbox: input.DestinationMailbox, UIDValidity: destination.UIDValidity, UID: uint32(uid)},
		WasAlreadyMoved: true,
	}, nil, sdkgo.Receipt{ProviderObjectID: input.MessageID})
}

// findMessageUIDByMessageID returns the highest UID in the selected mailbox whose Message-ID is exactly
// messageID, or zero. SEARCH HEADER matches a substring, so each candidate's envelope is compared exactly.
func findMessageUIDByMessageID(session *imapSession, messageID string) (imap.UID, *serverFailure) {
	criteria := &imap.SearchCriteria{Header: []imap.SearchCriteriaHeaderField{{Key: "Message-ID", Value: "<" + messageID + ">"}}}
	searched, err := session.client.UIDSearch(criteria, nil).Wait()
	if err != nil {
		return 0, session.classify("SEARCH", err)
	}
	matched, isUIDSet := searched.All.(imap.UIDSet)
	if !isUIDSet || matched.Dynamic() {
		return 0, nil
	}
	candidates, _ := matched.Nums()
	if len(candidates) == 0 {
		return 0, nil
	}
	fetched, failure := session.fetchMessages(candidates, &imap.FetchOptions{UID: true, Envelope: true})
	if failure != nil {
		return 0, failure
	}
	var found imap.UID
	for _, message := range fetched {
		if message.Envelope != nil && message.Envelope.MessageID == messageID && message.UID > found {
			found = message.UID
		}
	}
	return found, nil
}

func validateMoveMessageInput(input MoveMessageInput) error {
	if err := validateMessageReference("message", input.Message); err != nil {
		return err
	}
	if err := validateMailboxName("destinationMailbox", input.DestinationMailbox); err != nil {
		return err
	}
	if isSameMailbox(input.Message.Mailbox, input.DestinationMailbox) {
		return errors.New("destinationMailbox must differ from the message's mailbox")
	}
	if input.MessageID != "" && !isMessageIdentifier(input.MessageID) {
		return errors.New("messageId must be a Message-ID without angle brackets, such as abc123@mail.example.com")
	}
	return nil
}

// moveAttemptForFailure retries every unconfirmed outcome, because a repeated move finds an applied one.
func moveAttemptForFailure(failure *serverFailure) sdkgo.MutationAttempt[MovedMessage] {
	switch failure.outcome {
	case outcomeRetryable, outcomeInvalidResponse:
		return sdkgo.NewMutationRetry[MovedMessage](failure.failure, 0)
	case outcomeNotFound:
		return sdkgo.NewMutationBranch(MoveMessageBranchNotFound, MovedMessage{}, &failure.failure, sdkgo.Receipt{})
	default:
		return sdkgo.NewMutationBranch(MoveMessageBranchProviderRejected, MovedMessage{}, &failure.failure, sdkgo.Receipt{})
	}
}
