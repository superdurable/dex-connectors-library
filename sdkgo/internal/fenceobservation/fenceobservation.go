// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package fenceobservation reports singledispatch Fence events to agent tool attempts; Dex shows heartbeats only to later attempts.
package fenceobservation

import "sync"

// Observation accumulates the Fence events of one call attempt. It is safe for concurrent use.
type Observation struct {
	mu                      sync.Mutex
	isPriorDispatchPossible bool
	isClaimed               bool
	references              int
}

type attemptKey struct {
	callID  string
	attempt int32
}

type observationRegistry struct {
	mu           sync.Mutex
	observations map[attemptKey]*Observation
}

var registry = &observationRegistry{observations: map[attemptKey]*Observation{}}

// Begin observes one call attempt and returns the function that stops; concurrent observers share it.
func Begin(callID string, attempt int32) (*Observation, func()) {
	key := attemptKey{callID: callID, attempt: attempt}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	observation := registry.observations[key]
	if observation == nil {
		observation = &Observation{}
		registry.observations[key] = observation
	}
	observation.references++
	return observation, func() { registry.end(key) }
}

// ReportDecision records a first Decide result; withholding a send means an earlier attempt may have sent.
func ReportDecision(callID string, attempt int32, isSendAllowed bool) {
	if isSendAllowed {
		return
	}
	registry.update(callID, attempt, func(observation *Observation) { observation.isPriorDispatchPossible = true })
}

// ReportClaimed records a successfully recorded claimed checkpoint: the request may reach the provider.
func ReportClaimed(callID string, attempt int32) {
	registry.update(callID, attempt, func(observation *Observation) { observation.isClaimed = true })
}

// ReportCanResend records the Fence's proof that this attempt's dispatched request was not processed.
func ReportCanResend(callID string, attempt int32) {
	registry.update(callID, attempt, func(observation *Observation) { observation.isClaimed = false })
}

// MayHaveDispatched reports Fence events that cannot prove no request of this call reached the provider.
func (observation *Observation) MayHaveDispatched() bool {
	observation.mu.Lock()
	defer observation.mu.Unlock()
	return observation.isPriorDispatchPossible || observation.isClaimed
}

func (registry *observationRegistry) end(key attemptKey) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	observation := registry.observations[key]
	if observation == nil {
		return
	}
	observation.references--
	if observation.references == 0 {
		delete(registry.observations, key)
	}
}

func (registry *observationRegistry) update(callID string, attempt int32, change func(*Observation)) {
	registry.mu.Lock()
	observation := registry.observations[attemptKey{callID: callID, attempt: attempt}]
	registry.mu.Unlock()
	if observation == nil {
		return
	}
	observation.mu.Lock()
	defer observation.mu.Unlock()
	change(observation)
}
