// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package fenceobservation

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

var workAttemptTwo = AttemptIdentity{CallID: "call-1", ConnectionProvider: "chat", ConnectionName: "work", Attempt: 2}

func TestObservationFollowsFenceEventsOfItsIdentity(t *testing.T) {
	observation, endObservation := Begin(workAttemptTwo)
	defer endObservation()
	require.False(t, observation.MayHaveDispatched())

	for _, other := range []AttemptIdentity{
		{CallID: "call-1", ConnectionProvider: "chat", ConnectionName: "work", Attempt: 1},
		{CallID: "call-2", ConnectionProvider: "chat", ConnectionName: "work", Attempt: 2},
		{CallID: "call-1", ConnectionProvider: "chat", ConnectionName: "personal", Attempt: 2},
		{CallID: "call-1", ConnectionProvider: "mail", ConnectionName: "work", Attempt: 2},
	} {
		ReportClaimed(other)
		ReportDecision(other, false)
	}
	require.False(t, observation.MayHaveDispatched(), "another attempt, call, or connection does not change it")

	ReportDecision(workAttemptTwo, true)
	require.False(t, observation.MayHaveDispatched())
	ReportClaimed(workAttemptTwo)
	require.True(t, observation.MayHaveDispatched())
	ReportDecision(workAttemptTwo, true)
	require.True(t, observation.MayHaveDispatched(), "nothing clears a possible dispatch")
}

func TestWithheldSendMarksPossibleDispatch(t *testing.T) {
	observation, endObservation := Begin(workAttemptTwo)
	defer endObservation()
	ReportDecision(workAttemptTwo, false)
	require.True(t, observation.MayHaveDispatched())
}

func TestConcurrentObserversShareAnObservationUntilAllEnd(t *testing.T) {
	first, endFirst := Begin(workAttemptTwo)
	second, endSecond := Begin(workAttemptTwo)
	require.Same(t, first, second)

	var group sync.WaitGroup
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			ReportClaimed(workAttemptTwo)
			_ = first.MayHaveDispatched()
		}()
	}
	group.Wait()
	endFirst()
	require.True(t, second.MayHaveDispatched())
	endSecond()

	fresh, endFresh := Begin(workAttemptTwo)
	defer endFresh()
	require.NotSame(t, first, fresh)
	require.False(t, fresh.MayHaveDispatched())
	require.NotPanics(t, func() { ReportClaimed(AttemptIdentity{CallID: "unobserved", Attempt: 1}) })
}
