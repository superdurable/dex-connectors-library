// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package singledispatch_test

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
	"github.com/superdurable/dex/sdk-go/dex"
)

type SendNoteInput struct {
	Recipient string
	Text      string
}

type NotifySubscriberOutput struct {
	NoteID string
}

// sendNoteDefinition stands in for a generated connector definition, such as
// gmail.SendMessageDefinition.
var sendNoteDefinition = sdkgo.MutationDefinition{
	Operation: sdkgo.OperationRef{ConnectorID: "example-notes", OperationID: "sendNote"},
	Branches: []sdkgo.BranchDefinition{
		{ID: "sent", Description: "The note was sent."},
		{ID: sdkgo.UncertainBranchID, Description: "The note may have been sent.", Optional: true},
		{ID: sdkgo.DefectBranchID, Description: "Local input or configuration is invalid.", Optional: true},
	},
	StepDefaults:    sdkgo.StepDefaults{ExecuteMethodTimeout: 30 * time.Second, ExecuteDurability: dex.StepDurabilitySync},
	RepeatOnUnknown: sdkgo.RepeatOnUnknownLimited,
	MaxSends:        3,
}

// sendNoteMutation is a fenced connector write whose provider answer gets lost.
type sendNoteMutation struct{ provider *notesProvider }

type notesProvider struct{ sentNotes int }

func (sendNoteMutation) Definition() sdkgo.MutationDefinition { return sendNoteDefinition }

func (sendNoteMutation) IdempotencyKey(id sdkgo.CallID, _ SendNoteInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

func (operation sendNoteMutation) Invoke(call sdkgo.Call, _ SendNoteInput) sdkgo.MutationAttempt[string] {
	failure := sdkgo.Failure{Kind: sdkgo.FailureTransport, Provider: "example-notes", Operation: "sendNote", Message: "the note's outcome is unknown"}
	fence, err := singledispatch.NewFence(call, 30*time.Second, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return sdkgo.NewMutationRetry[string](failure, 0)
	}
	if kind := fence.Decide().Kind; kind != singledispatch.SendAllowed && kind != singledispatch.ReadBackThenResendAllowed {
		return sdkgo.NewMutationUncertain("", failure, sdkgo.Receipt{})
	}
	err = fence.Dispatch(func(context.Context) error {
		operation.provider.sentNotes++
		return errors.New("connection reset after the request was written")
	})
	if err != nil {
		return sdkgo.NewMutationRetry[string](failure, 0)
	}
	return sdkgo.NewMutationBranch("sent", "note-1", nil, sdkgo.Receipt{})
}

var notifySubscriberDefinition = sdkgo.MutationDefinition{
	Operation: sdkgo.OperationRef{ConnectorID: "example-app", OperationID: "notifySubscriber"},
	Branches: []sdkgo.BranchDefinition{
		{ID: "notified", Description: "The subscriber was sent the note."},
		{ID: "unsubscribed", Description: "The recipient is no longer subscribed; nothing was sent.", Optional: true},
		{ID: sdkgo.UncertainBranchID, Description: "The note may have been sent.", Optional: true},
		{ID: sdkgo.DefectBranchID, Description: "Local input or configuration is invalid.", Optional: true},
	},
	RepeatOnUnknown: sdkgo.RepeatOnUnknownLimited,
	MaxSends:        3,
}

// notifySubscriberMutation is an application Mutation that checks a subscription before it
// runs the wrapped connector write.
type notifySubscriberMutation struct {
	sendNote    sendNoteMutation
	subscribers map[string]bool
}

func (notifySubscriberMutation) Definition() sdkgo.MutationDefinition {
	return notifySubscriberDefinition
}

func (notifySubscriberMutation) IdempotencyKey(id sdkgo.CallID, _ SendNoteInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

func (mutation notifySubscriberMutation) Invoke(call sdkgo.Call, input SendNoteInput) sdkgo.MutationAttempt[NotifySubscriberOutput] {
	sendCallID, err := sdkgo.NextCallID(call.Context, sendNoteDefinition.Operation)
	if err != nil {
		failure := sdkgo.Failure{Kind: sdkgo.FailureLocalDefect, Provider: "example-app", Operation: "notifySubscriber", Message: "the note call cannot be named"}
		return sdkgo.NewMutationBranch(sdkgo.DefectBranchID, NotifySubscriberOutput{}, &failure, sdkgo.Receipt{})
	}
	if !mutation.subscribers[input.Recipient] {
		failure := sdkgo.Failure{Kind: sdkgo.FailureConflict, Provider: "example-app", Operation: "notifySubscriber", Message: "the recipient is not subscribed"}
		priorSends, err := singledispatch.MaximumPriorSends(call.Context, sendCallID)
		if err != nil {
			return sdkgo.NewMutationRetry[NotifySubscriberOutput](failure, 0)
		}
		if priorSends > 0 {
			// An earlier attempt may have sent the note; a definite refusal would invite a duplicate.
			return sdkgo.NewMutationUncertain(NotifySubscriberOutput{}, failure, sdkgo.Receipt{})
		}
		return sdkgo.NewMutationBranch("unsubscribed", NotifySubscriberOutput{}, &failure, sdkgo.Receipt{})
	}
	connection := sdkgo.ConnectionRef{Provider: "example-notes", Name: "work"}
	result, err := sdkgo.RunMutation(call.Context, mutation.sendNote, connection, input)
	var retryErr *sdkgo.RetryError
	if errors.As(err, &retryErr) {
		return sdkgo.NewMutationRetry[NotifySubscriberOutput](retryErr.Failure, 0)
	}
	if err != nil || result.Branch != "sent" {
		failure := sdkgo.Failure{Kind: sdkgo.FailureTransport, Provider: "example-app", Operation: "notifySubscriber", Message: "the note may have been sent"}
		return sdkgo.NewMutationUncertain(NotifySubscriberOutput{}, failure, sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch("notified", NotifySubscriberOutput{NoteID: result.Value}, nil, sdkgo.Receipt{})
}

func runNotifySubscriber(ctx *singledispatchtest.Context, mutation notifySubscriberMutation, input SendNoteInput) string {
	connection := sdkgo.ConnectionRef{Provider: "example-app", Name: "default"}
	result, err := sdkgo.RunMutation(ctx, mutation, connection, input)
	var retryErr *sdkgo.RetryError
	if errors.As(err, &retryErr) {
		return "retry"
	}
	if err != nil {
		return "error: " + err.Error()
	}
	return string(result.Branch)
}

// ExampleMaximumPriorSends shows an application Mutation that checks a subscription and
// then sends a note through a fenced connector write. Attempt 1 sends, but the answer is
// lost. Before attempt 2 runs, the recipient unsubscribes. The note may already have gone
// out, so the refusal selects uncertain instead of the definite unsubscribed branch, and the
// caller does not send it again under a new Call ID. A first attempt that refuses selects
// unsubscribed.
func ExampleMaximumPriorSends() {
	provider := &notesProvider{}
	mutation := notifySubscriberMutation{sendNote: sendNoteMutation{provider: provider}, subscribers: map[string]bool{"ada@example.com": true}}
	input := SendNoteInput{Recipient: "ada@example.com", Text: "Your order shipped."}

	ctx := singledispatchtest.NewContext("orders-flow", "notify-subscriber")
	fmt.Println("attempt 1:", runNotifySubscriber(ctx, mutation, input), "after", provider.sentNotes, "send")
	delete(mutation.subscribers, "ada@example.com")
	ctx.AdvanceAttempt()
	fmt.Println("attempt 2:", runNotifySubscriber(ctx, mutation, input))

	another := singledispatchtest.NewContext("orders-flow", "notify-subscriber-again")
	fmt.Println("another call, attempt 1:", runNotifySubscriber(another, mutation, input))
	// Output:
	// attempt 1: retry after 1 send
	// attempt 2: uncertain
	// another call, attempt 1: unsubscribed
}
