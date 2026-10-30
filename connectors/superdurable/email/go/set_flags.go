// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package email

import (
	"context"
	"errors"
	"strings"

	"github.com/emersion/go-imap/v2"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const setFlagsOperation = "setFlags"

// SetFlagsInput sets message flags to absolute values. A nil field leaves that flag unchanged; at least
// one field is required.
type SetFlagsInput struct {
	// Message is a reference from searchMessages; its UIDValidity must still match the mailbox.
	Message MessageReference `json:"message"`
	// IsSeen sets (true) or clears (false) \Seen, which mail clients show as read.
	IsSeen *bool `json:"isSeen,omitempty"`
	// IsFlagged sets or clears \Flagged, which mail clients show as starred or flagged.
	IsFlagged *bool `json:"isFlagged,omitempty"`
	// IsAnswered sets or clears \Answered, which mail clients show as replied.
	IsAnswered *bool `json:"isAnswered,omitempty"`
}

// MessageFlags is the message's flags after the change, read back from the server.
type MessageFlags struct {
	// Message identifies the changed message.
	Message MessageReference `json:"message"`
	// IsSeen reports \Seen.
	IsSeen bool `json:"isSeen"`
	// IsFlagged reports \Flagged.
	IsFlagged bool `json:"isFlagged"`
	// IsAnswered reports \Answered.
	IsAnswered bool `json:"isAnswered"`
	// WasAlreadyApplied reports that the message already held the requested flags, so nothing was written.
	WasAlreadyApplied bool `json:"wasAlreadyApplied,omitempty"`
}

// SetFlagsOperation is the setFlags Mutation.
type SetFlagsOperation struct {
	client *Client
}

// Definition returns the immutable connector operation definition.
func (SetFlagsOperation) Definition() sdkgo.MutationDefinition { return SetFlagsDefinition }

// IdempotencyKey uses the stable connector Call ID; IMAP has no key, and absolute flag values repeat safely.
func (SetFlagsOperation) IdempotencyKey(callID sdkgo.CallID, _ SetFlagsInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads the flags, stores only the differences with UID STORE, and reads the flags back.
func (operation SetFlagsOperation) Invoke(call sdkgo.Call, input SetFlagsInput) sdkgo.MutationAttempt[MessageFlags] {
	if err := validateSetFlagsInput(input); err != nil {
		return sdkgo.NewMutationBranch(SetFlagsBranchDefect, MessageFlags{},
			emailFailurePointer(sdkgo.FailureValidation, setFlagsOperation, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, setFlagsOperation)
	if failure != nil {
		return sdkgo.NewMutationBranch(SetFlagsBranchDefect, MessageFlags{}, failure, sdkgo.Receipt{})
	}
	ctx, cancel := context.WithTimeout(call.Context, imapOperationTimeout)
	defer cancel()
	session, sessionFailure := operation.client.openIMAPSession(ctx, credentials, setFlagsOperation)
	if sessionFailure != nil {
		return setFlagsAttemptForFailure(sessionFailure)
	}
	defer session.close()
	if _, sessionFailure := session.selectMailbox(input.Message.Mailbox, false, input.Message.UIDValidity); sessionFailure != nil {
		return setFlagsAttemptForFailure(sessionFailure)
	}
	uid := imap.UID(input.Message.UID)
	current, sessionFailure := readMessageFlags(session, input.Message, uid)
	if sessionFailure != nil {
		return setFlagsAttemptForFailure(sessionFailure)
	}
	added, removed := planFlagChanges(current, input)
	if len(added) == 0 && len(removed) == 0 {
		current.WasAlreadyApplied = true
		return sdkgo.NewMutationBranch(SetFlagsBranchUpdated, current, nil, sdkgo.Receipt{})
	}
	for _, change := range []imap.StoreFlags{{Op: imap.StoreFlagsAdd, Silent: true, Flags: added}, {Op: imap.StoreFlagsDel, Silent: true, Flags: removed}} {
		if len(change.Flags) == 0 {
			continue
		}
		if err := session.client.Store(imap.UIDSetNum(uid), &change, nil).Close(); err != nil {
			return setFlagsAttemptForFailure(session.classify("STORE", err))
		}
	}
	updated, sessionFailure := readMessageFlags(session, input.Message, uid)
	if sessionFailure != nil {
		return setFlagsAttemptForFailure(sessionFailure)
	}
	if stillAdded, stillRemoved := planFlagChanges(updated, input); len(stillAdded) > 0 || len(stillRemoved) > 0 {
		return sdkgo.NewMutationBranch(SetFlagsBranchProviderRejected, updated, emailFailurePointer(sdkgo.FailureProviderRejection, setFlagsOperation,
			"the IMAP server accepted STORE but did not keep the requested flags; the mailbox may not allow permanent flags"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(SetFlagsBranchUpdated, updated, nil, sdkgo.Receipt{})
}

// readMessageFlags fetches the message's flags, or selects notFound when no message has the UID.
func readMessageFlags(session *imapSession, reference MessageReference, uid imap.UID) (MessageFlags, *serverFailure) {
	fetched, failure := session.fetchMessages([]imap.UID{uid}, &imap.FetchOptions{UID: true, Flags: true})
	if failure != nil {
		return MessageFlags{}, failure
	}
	message := findFetchedMessage(fetched, uid)
	if message == nil {
		return MessageFlags{}, &serverFailure{outcome: outcomeNotFound, failure: emailFailure(sdkgo.FailureNotFound, session.operation,
			"no message in the mailbox has the UID")}
	}
	flags := MessageFlags{Message: reference}
	for _, flag := range message.Flags {
		switch {
		case strings.EqualFold(string(flag), string(imap.FlagSeen)):
			flags.IsSeen = true
		case strings.EqualFold(string(flag), string(imap.FlagFlagged)):
			flags.IsFlagged = true
		case strings.EqualFold(string(flag), string(imap.FlagAnswered)):
			flags.IsAnswered = true
		}
	}
	return flags, nil
}

// planFlagChanges returns the flags to add and remove so the message holds the requested values.
func planFlagChanges(current MessageFlags, input SetFlagsInput) (added []imap.Flag, removed []imap.Flag) {
	for _, requested := range []struct {
		value     *bool
		isPresent bool
		flag      imap.Flag
	}{
		{input.IsSeen, current.IsSeen, imap.FlagSeen},
		{input.IsFlagged, current.IsFlagged, imap.FlagFlagged},
		{input.IsAnswered, current.IsAnswered, imap.FlagAnswered},
	} {
		switch {
		case requested.value == nil || *requested.value == requested.isPresent:
		case *requested.value:
			added = append(added, requested.flag)
		default:
			removed = append(removed, requested.flag)
		}
	}
	return added, removed
}

func validateSetFlagsInput(input SetFlagsInput) error {
	if err := validateMessageReference("message", input.Message); err != nil {
		return err
	}
	if input.IsSeen == nil && input.IsFlagged == nil && input.IsAnswered == nil {
		return errors.New("set at least one of isSeen, isFlagged, or isAnswered")
	}
	return nil
}

func setFlagsAttemptForFailure(failure *serverFailure) sdkgo.MutationAttempt[MessageFlags] {
	switch failure.outcome {
	case outcomeRetryable, outcomeInvalidResponse:
		return sdkgo.NewMutationRetry[MessageFlags](failure.failure, 0)
	case outcomeNotFound:
		return sdkgo.NewMutationBranch(SetFlagsBranchNotFound, MessageFlags{}, &failure.failure, sdkgo.Receipt{})
	default:
		return sdkgo.NewMutationBranch(SetFlagsBranchProviderRejected, MessageFlags{}, &failure.failure, sdkgo.Receipt{})
	}
}
