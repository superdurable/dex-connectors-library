// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package connectormock

import (
	"testing"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// Mutation is a scripted sdkgo.Mutation for application tests. It scripts
// calls exactly like Query and adds provider idempotency: a call whose
// idempotency key already received a terminal outcome, including uncertain,
// returns that first outcome again without consuming a case, as a provider
// does for a Step whose Execute is retried after the call. A Retry is not
// remembered, so the retried attempt takes the next case.
//
// The idempotency key is the SDK Call ID, which is stable across the retries
// of one Step execution.
type Mutation[IN, OUT any] struct {
	definition sdkgo.MutationDefinition
	operation  *scriptedOperation[IN, OUT]
}

// NewMutation returns an unscripted Mutation mock with the operation's real
// definition, such as a connector's generated SendMessageDefinition. Scripting
// mistakes and unscripted calls fail t; calls answered after t finished select
// the defect branch without reporting.
func NewMutation[IN, OUT any](t testing.TB, definition sdkgo.MutationDefinition) *Mutation[IN, OUT] {
	t.Helper()
	if err := definition.Validate(); err != nil {
		t.Fatalf("connector mock mutation definition: %v", err)
	}
	return &Mutation[IN, OUT]{
		definition: definition,
		operation:  newScriptedOperation[IN, OUT](t, definition.Operation, definition.Branches, true),
	}
}

// Definition returns the real operation definition.
func (mutation *Mutation[IN, OUT]) Definition() sdkgo.MutationDefinition {
	return mutation.definition
}

// IdempotencyKey returns callID, which stays the same across the retries of
// one Step execution.
func (mutation *Mutation[IN, OUT]) IdempotencyKey(callID sdkgo.CallID, _ IN) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke answers one call from the script, or replays the first terminal
// outcome for a repeated idempotency key, and records it in Calls and the
// process-wide RecordedCalls.
func (mutation *Mutation[IN, OUT]) Invoke(call sdkgo.Call, input IN) sdkgo.MutationAttempt[OUT] {
	resolved := mutation.operation.answer(call, input, call.IdempotencyKey)
	switch resolved.kind {
	case caseKindRetry:
		return sdkgo.NewMutationRetry[OUT](*resolved.failure, resolved.retryAfter)
	case caseKindUncertain:
		return sdkgo.NewMutationUncertain(resolved.value, *resolved.failure, sdkgo.Receipt{})
	default:
		return sdkgo.NewMutationBranch(resolved.branch, resolved.value, resolved.failure, sdkgo.Receipt{})
	}
}

// Respond queues cases that answer the next calls in order, one case per
// call.
func (mutation *Mutation[IN, OUT]) Respond(cases ...Case[OUT]) *Mutation[IN, OUT] {
	mutation.operation.t.Helper()
	mutation.operation.respond(&mutation.operation.unscopedScript, cases)
	return mutation
}

// When answers every call whose input matches with response. Rules are
// checked in the order they were added and are never consumed.
func (mutation *Mutation[IN, OUT]) When(matches func(IN) bool, response Case[OUT]) *Mutation[IN, OUT] {
	mutation.operation.t.Helper()
	mutation.operation.when(&mutation.operation.unscopedScript, matches, response)
	return mutation
}

// Default replaces the cases that answer calls no Respond or When case
// answers. They are used in order and the last one repeats.
func (mutation *Mutation[IN, OUT]) Default(cases ...Case[OUT]) *Mutation[IN, OUT] {
	mutation.operation.t.Helper()
	mutation.operation.setDefault(&mutation.operation.unscopedScript, cases)
	return mutation
}

// Strict stops every Default from answering, including ForFlow defaults, so
// each call must be scripted with Respond or When.
func (mutation *Mutation[IN, OUT]) Strict() *Mutation[IN, OUT] {
	mutation.operation.makeStrict()
	return mutation
}

// ForFlow returns the script for calls whose Flow ID starts with
// flowIDOrPrefix, which includes an exact Flow ID. A longer match takes
// precedence. Calling it again with the same value returns the same script.
func (mutation *Mutation[IN, OUT]) ForFlow(flowIDOrPrefix string) *FlowScript[IN, OUT] {
	mutation.operation.t.Helper()
	return mutation.operation.flowScript(flowIDOrPrefix)
}

// Calls returns a copy of every call the mock answered, in answer order,
// including replays of a repeated idempotency key.
func (mutation *Mutation[IN, OUT]) Calls() []Call[IN] {
	return mutation.operation.recordedCalls()
}
