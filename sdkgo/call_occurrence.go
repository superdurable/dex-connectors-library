// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sdkgo

import (
	"fmt"
	"sync"

	"github.com/superdurable/dex-connectors-library/sdkgo/internal/stepattempt"
	"github.com/superdurable/dex/sdk-go/dex"
)

// callOccurrences counts the calls of each operation in one Step attempt and tracks the
// Mutations still running.
type callOccurrences struct {
	mu               sync.Mutex
	counts           map[OperationRef]int
	runningMutations map[OperationRef]bool
}

var stepCallOccurrences = stepattempt.NewRegistry(func(dex.Context) *callOccurrences {
	return &callOccurrences{counts: map[OperationRef]int{}, runningMutations: map[OperationRef]bool{}}
})

// NextCallID returns the Call ID of the next call of operation in ctx's Step attempt: the
// Call.ID that RunQuery, RunMutation, or an agent tool's RunAttempt gives that call when it
// starts with ctx. The first call of an operation in a Step attempt gets the four-field
// derivation of the connector contract, and the n-th call mixes in n; see Call.ID.
// NextCallID counts nothing, so it never changes the ID of a later call and returns the same
// ID until that call starts.
//
// An application Mutation that runs its own checks before a fenced connector write it starts
// as its own call in the same Step, through RunMutation or an agent tool, names that write
// with NextCallID and passes the ID to singledispatch.MaximumPriorSends. A write in a
// separate Connector Step is not such a call: that Step has its own Step execution ID, so
// its Call ID differs from the one NextCallID derives in another Step, and its checkpoint is
// in its own heartbeat, which another Step cannot read. A wrapper whose
// Invoke calls the wrapped operation's Invoke with the Call it received passes call.ID
// instead, because the wrapped Fence uses that ID; there NextCallID names a different call,
// as the wrapper's own call has already been counted or belongs to another operation. Call
// NextCallID before the write starts: once it has started, NextCallID names the call after it,
// and the result's Receipt.CallID names the call that ran. Because the count restarts each
// attempt, the ID names the same call in every attempt only when the Step makes its
// connector calls in a deterministic order. It is safe for concurrent callers, but a call
// of operation that starts concurrently can take the returned ID.
//
// It returns an error when ctx is nil, ctx has no Flow ID or no Step execution ID, as in an
// RPC handler, or operation is invalid.
func NextCallID(ctx dex.Context, operation OperationRef) (CallID, error) {
	if err := validateOperationScope(ctx, operation); err != nil {
		return "", err
	}
	return deriveOccurrenceCallID(ctx.FlowID(), ctx.StepExecutionID(), operation, peekCallOccurrence(ctx, operation)), nil
}

// peekCallOccurrence returns the ordinal the next call of operation in ctx's attempt gets.
func peekCallOccurrence(ctx dex.Context, operation OperationRef) int {
	occurrences := stepCallOccurrences.For(ctx)
	occurrences.mu.Lock()
	defer occurrences.mu.Unlock()
	return occurrences.counts[operation] + 1
}

// nextCallOccurrence returns the one-based ordinal of this call of operation in ctx's attempt.
func nextCallOccurrence(ctx dex.Context, operation OperationRef) int {
	occurrences := stepCallOccurrences.For(ctx)
	occurrences.mu.Lock()
	defer occurrences.mu.Unlock()
	occurrences.counts[operation]++
	return occurrences.counts[operation]
}

// beginMutationOccurrence refuses a call while an earlier one runs, since overlapping calls could swap ordinals.
// A refused call still consumes its ordinal, so later ordinals follow call order, not timing.
func beginMutationOccurrence(ctx dex.Context, operation OperationRef) (int, func(), error) {
	occurrences := stepCallOccurrences.For(ctx)
	occurrences.mu.Lock()
	defer occurrences.mu.Unlock()
	occurrences.counts[operation]++
	if occurrences.runningMutations[operation] {
		return 0, nil, fmt.Errorf("concurrent calls of connector operation %s %s in one Step are not supported; an earlier call of it is still running, so this call was not sent", operation.ConnectorID, operation.OperationID)
	}
	occurrences.runningMutations[operation] = true
	end := func() {
		occurrences.mu.Lock()
		defer occurrences.mu.Unlock()
		delete(occurrences.runningMutations, operation)
	}
	return occurrences.counts[operation], end, nil
}
