// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package fenceobservation

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestObservationFollowsFenceEventsOfItsAttempt(t *testing.T) {
	observation, endObservation := Begin("call-1", 2)
	defer endObservation()
	require.False(t, observation.MayHaveDispatched())

	ReportClaimed("call-1", 1)
	ReportClaimed("call-2", 2)
	require.False(t, observation.MayHaveDispatched(), "another attempt or call does not change it")

	ReportClaimed("call-1", 2)
	require.True(t, observation.MayHaveDispatched())
	ReportCanResend("call-1", 2)
	require.False(t, observation.MayHaveDispatched(), "canResend proves the claimed request was not processed")

	ReportDecision("call-1", 2, true)
	require.False(t, observation.MayHaveDispatched())
	ReportDecision("call-1", 2, false)
	ReportCanResend("call-1", 2)
	require.True(t, observation.MayHaveDispatched(), "a withheld send stays possible dispatch")
}

func TestConcurrentObserversShareAnObservationUntilBothEnd(t *testing.T) {
	first, endFirst := Begin("call-1", 1)
	second, endSecond := Begin("call-1", 1)
	require.Same(t, first, second)
	ReportClaimed("call-1", 1)
	endFirst()
	require.True(t, second.MayHaveDispatched())
	endSecond()

	fresh, endFresh := Begin("call-1", 1)
	defer endFresh()
	require.NotSame(t, first, fresh)
	require.False(t, fresh.MayHaveDispatched())
	require.NotPanics(t, func() { ReportClaimed("unobserved", 1) })
}
