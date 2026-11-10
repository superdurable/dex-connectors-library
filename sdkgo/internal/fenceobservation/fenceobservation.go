// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package fenceobservation reports singledispatch Fence events to agent tool attempts; Dex shows heartbeats only to later attempts.
package fenceobservation

import "sync"

// AttemptIdentity names one call attempt on one connection. The Call ID covers the Flow, Step
// execution, and operation; the connection separates calls that share them.
type AttemptIdentity struct {
	// CallID is the stable connector call identity.
	CallID string
	// ConnectionProvider is the provider of the connection the call runs with.
	ConnectionProvider string
	// ConnectionName is the name of the connection the call runs with.
	ConnectionName string
	// Attempt is the one-based Dex Execute attempt.
	Attempt int32
}

// Observation accumulates the Fence events of one call attempt. It is safe for concurrent use.
// Its possible-dispatch flag never clears, because observers of one identity share it.
type Observation struct {
	mu                 sync.Mutex
	isDispatchPossible bool
	references         int
}

type observationRegistry struct {
	mu           sync.Mutex
	observations map[AttemptIdentity]*Observation
}

var registry = &observationRegistry{observations: map[AttemptIdentity]*Observation{}}

// Begin observes one call attempt and returns the function that stops; concurrent observers share it.
func Begin(identity AttemptIdentity) (*Observation, func()) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	observation := registry.observations[identity]
	if observation == nil {
		observation = &Observation{}
		registry.observations[identity] = observation
	}
	observation.references++
	return observation, func() { registry.end(identity) }
}

// ReportDecision records a first Decide result; withholding a send means an earlier attempt may have sent.
func ReportDecision(identity AttemptIdentity, isSendAllowed bool) {
	if !isSendAllowed {
		registry.markDispatchPossible(identity)
	}
}

// ReportClaimed records a successfully recorded claimed checkpoint: the request may reach the provider.
func ReportClaimed(identity AttemptIdentity) {
	registry.markDispatchPossible(identity)
}

// MayHaveDispatched reports Fence events that cannot prove no request of this call reached the provider.
func (observation *Observation) MayHaveDispatched() bool {
	observation.mu.Lock()
	defer observation.mu.Unlock()
	return observation.isDispatchPossible
}

func (registry *observationRegistry) end(identity AttemptIdentity) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	observation := registry.observations[identity]
	if observation == nil {
		return
	}
	observation.references--
	if observation.references == 0 {
		delete(registry.observations, identity)
	}
}

func (registry *observationRegistry) markDispatchPossible(identity AttemptIdentity) {
	registry.mu.Lock()
	observation := registry.observations[identity]
	registry.mu.Unlock()
	if observation == nil {
		return
	}
	observation.mu.Lock()
	defer observation.mu.Unlock()
	observation.isDispatchPossible = true
}
