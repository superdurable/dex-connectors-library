// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package singledispatch_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

// sameCallReads is what one attempt's wrapper read and what the wrapped Fence decided.
type sameCallReads struct {
	wrapperCallID        sdkgo.CallID
	priorSends           int32
	priorSendsErr        error
	nextCallID           sdkgo.CallID
	nextCallIDErr        error
	nextCallIDPriorSends int32
	nextCallIDReadErr    error
	fenceCallID          sdkgo.CallID
	fencePriorSends      int32
	recordErr            error
}

// sameCallWrapper calls the wrapped Invoke with the Call it received.
type sameCallWrapper struct {
	definition sdkgo.MutationDefinition
	wrapped    rateLimitedNoteMutation
}

func (wrapper sameCallWrapper) Definition() sdkgo.MutationDefinition { return wrapper.definition }

func (sameCallWrapper) IdempotencyKey(id sdkgo.CallID, _ SendNoteInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

func (wrapper sameCallWrapper) Invoke(call sdkgo.Call, input SendNoteInput) sdkgo.MutationAttempt[string] {
	reads := wrapper.wrapped.reads
	reads.wrapperCallID = call.ID
	reads.priorSends, reads.priorSendsErr = singledispatch.MaximumPriorSends(call.Context, call.ID)
	reads.nextCallID, reads.nextCallIDErr = sdkgo.NextCallID(call.Context, sendNoteDefinition.Operation)
	reads.nextCallIDPriorSends, reads.nextCallIDReadErr = singledispatch.MaximumPriorSends(call.Context, reads.nextCallID)
	return wrapper.wrapped.Invoke(call, input)
}

// rateLimitedNoteMutation is a fenced write that the provider refuses with 429 after the send.
type rateLimitedNoteMutation struct{ reads *sameCallReads }

func (rateLimitedNoteMutation) Definition() sdkgo.MutationDefinition { return sendNoteDefinition }

func (rateLimitedNoteMutation) IdempotencyKey(id sdkgo.CallID, _ SendNoteInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

func (operation rateLimitedNoteMutation) Invoke(call sdkgo.Call, _ SendNoteInput) sdkgo.MutationAttempt[string] {
	failure := sdkgo.Failure{Kind: sdkgo.FailureRateLimit, Provider: "example-notes", Operation: "sendNote", Message: "rate limited"}
	fence, err := singledispatch.NewFence(call, 30*time.Second, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return sdkgo.NewMutationRetry[string](failure, 0)
	}
	decision := fence.Decide()
	operation.reads.fenceCallID, operation.reads.fencePriorSends = call.ID, decision.MaximumPriorSends
	if decision.Kind != singledispatch.SendAllowed && decision.Kind != singledispatch.ReadBackThenResendAllowed {
		return sdkgo.NewMutationRetry[string](failure, 0)
	}
	err = fence.Dispatch(func(context.Context) error { return nil })
	if err == nil {
		err = fence.RecordCanResend()
	}
	operation.reads.recordErr = err
	return sdkgo.NewMutationRetry[string](failure, 0)
}

// A refusal leaves no prior send, so only call.ID reads zero on attempt two.
func TestMaximumPriorSendsOfASameCallWrapperReadsCallID(t *testing.T) {
	ownDefinition := sendNoteDefinition
	ownDefinition.Operation = sdkgo.OperationRef{ConnectorID: "example-app", OperationID: "relayNote"}
	for _, scenario := range []struct {
		name       string
		definition sdkgo.MutationDefinition
	}{
		{name: "Definition delegates to the wrapped operation", definition: sendNoteDefinition},
		{name: "Definition is the wrapper's own", definition: ownDefinition},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			reads := &sameCallReads{}
			wrapper := sameCallWrapper{definition: scenario.definition, wrapped: rateLimitedNoteMutation{reads: reads}}
			ctx := singledispatchtest.NewContext("flow-prior-sends", "step-same-call")
			for attempt := int32(1); attempt <= 2; attempt++ {
				wrapperCallID, err := sdkgo.NextCallID(ctx, scenario.definition.Operation)
				require.NoError(t, err)
				_, err = sdkgo.RunMutation(ctx, wrapper, sdkgo.ConnectionRef{Provider: "example-notes", Name: "work"}, SendNoteInput{Recipient: "ada@example.com"})
				var retryErr *sdkgo.RetryError
				require.True(t, errors.As(err, &retryErr), "attempt %d: %v", attempt, err)
				require.NoError(t, reads.recordErr)

				require.Equal(t, wrapperCallID, reads.wrapperCallID, "the wrapper's caller names its call with NextCallID")
				require.Equal(t, reads.wrapperCallID, reads.fenceCallID, "the wrapped Fence uses the wrapper's Call ID")
				require.NoError(t, reads.priorSendsErr)
				require.Equal(t, reads.fencePriorSends, reads.priorSends)
				require.Zero(t, reads.priorSends)

				require.NoError(t, reads.nextCallIDErr)
				require.NoError(t, reads.nextCallIDReadErr)
				require.NotEqual(t, reads.wrapperCallID, reads.nextCallID, "inside the wrapper, NextCallID names another call")
				require.Equal(t, attempt-1, reads.nextCallIDPriorSends)
				ctx.AdvanceAttempt()
			}
		})
	}
}
