// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package connectormock

// FlowScript scripts the calls of the Flows whose ID starts with one Flow ID
// or prefix. Its cases take precedence over the mock's own script, so tests
// that run several Flows against one connection can give each Flow its own
// answers. A call that the FlowScript cannot answer falls through to the next
// matching script.
type FlowScript[IN, OUT any] struct {
	operation      *scriptedOperation[IN, OUT]
	flowIDOrPrefix string
	script         script[IN, OUT]
}

// Respond queues cases that answer the matching Flows' next calls in order.
func (flowScript *FlowScript[IN, OUT]) Respond(cases ...Case[OUT]) *FlowScript[IN, OUT] {
	flowScript.operation.t.Helper()
	flowScript.operation.respond(&flowScript.script, cases)
	return flowScript
}

// When answers every matching Flow's call whose input matches with response.
func (flowScript *FlowScript[IN, OUT]) When(matches func(IN) bool, response Case[OUT]) *FlowScript[IN, OUT] {
	flowScript.operation.t.Helper()
	flowScript.operation.when(&flowScript.script, matches, response)
	return flowScript
}

// Default replaces the matching Flows' default cases. They take precedence
// over the mock's own defaults and are ignored when the mock is Strict.
func (flowScript *FlowScript[IN, OUT]) Default(cases ...Case[OUT]) *FlowScript[IN, OUT] {
	flowScript.operation.t.Helper()
	flowScript.operation.setDefault(&flowScript.script, cases)
	return flowScript
}
