// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package connectormock

import (
	"testing"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// Query is a scripted sdkgo.Query for application tests. It returns the real
// operation definition, so the SDK validates its answers exactly as it
// validates a provider connector: an undeclared branch, an invalid failure, or
// an uncertain case still selects the defect branch.
//
// Each call is answered by, in order: the most specific matching ForFlow
// script, then the mock's own script; within a script, the first When rule
// whose predicate matches, then the next Respond case; and when no script
// answers, the Default cases of those scripts unless the mock is Strict. A
// call that nothing answers fails the test and selects the defect branch.
//
// A Query is safe for concurrent use by Workers and the test. Predicates run
// while the mock is locked and must not call the mock.
type Query[IN, OUT any] struct {
	definition sdkgo.QueryDefinition
	operation  *scriptedOperation[IN, OUT]
}

// NewQuery returns an unscripted Query mock with the operation's real
// definition, such as a connector's generated GenerateTextDefinition. Scripting
// mistakes and unscripted calls fail t; calls answered after t finished
// select the defect branch without reporting.
func NewQuery[IN, OUT any](t testing.TB, definition sdkgo.QueryDefinition) *Query[IN, OUT] {
	t.Helper()
	if err := definition.Validate(); err != nil {
		t.Fatalf("connector mock query definition: %v", err)
	}
	return &Query[IN, OUT]{
		definition: definition,
		operation:  newScriptedOperation[IN, OUT](t, definition.Operation, definition.Branches, false),
	}
}

// Definition returns the real operation definition.
func (query *Query[IN, OUT]) Definition() sdkgo.QueryDefinition {
	return query.definition
}

// Invoke answers one call from the script and records it in Calls and the
// process-wide RecordedCalls.
func (query *Query[IN, OUT]) Invoke(call sdkgo.Call, input IN) sdkgo.QueryAttempt[OUT] {
	resolved := query.operation.answer(call, input, "")
	if resolved.kind == caseKindRetry {
		return sdkgo.NewQueryRetry[OUT](*resolved.failure, resolved.retryAfter)
	}
	return sdkgo.NewQueryBranch(resolved.branch, resolved.value, resolved.failure, sdkgo.Receipt{})
}

// Respond queues cases that answer the next calls in order, one case per
// call.
func (query *Query[IN, OUT]) Respond(cases ...Case[OUT]) *Query[IN, OUT] {
	query.operation.t.Helper()
	query.operation.respond(&query.operation.unscopedScript, cases)
	return query
}

// When answers every call whose input matches with response. Rules are
// checked in the order they were added and are never consumed.
func (query *Query[IN, OUT]) When(matches func(IN) bool, response Case[OUT]) *Query[IN, OUT] {
	query.operation.t.Helper()
	query.operation.when(&query.operation.unscopedScript, matches, response)
	return query
}

// Default replaces the cases that answer calls no Respond or When case
// answers. They are used in order and the last one repeats. A generated mock
// package installs the manifest default here.
func (query *Query[IN, OUT]) Default(cases ...Case[OUT]) *Query[IN, OUT] {
	query.operation.t.Helper()
	query.operation.setDefault(&query.operation.unscopedScript, cases)
	return query
}

// Strict stops every Default from answering, including ForFlow defaults, so
// each call must be scripted with Respond or When.
func (query *Query[IN, OUT]) Strict() *Query[IN, OUT] {
	query.operation.makeStrict()
	return query
}

// ForFlow returns the script for calls whose Flow ID starts with
// flowIDOrPrefix, which includes an exact Flow ID. A longer match takes
// precedence. Calling it again with the same value returns the same script.
func (query *Query[IN, OUT]) ForFlow(flowIDOrPrefix string) *FlowScript[IN, OUT] {
	query.operation.t.Helper()
	return query.operation.flowScript(flowIDOrPrefix)
}

// Calls returns a copy of every call the mock answered, in answer order.
func (query *Query[IN, OUT]) Calls() []Call[IN] {
	return query.operation.recordedCalls()
}
