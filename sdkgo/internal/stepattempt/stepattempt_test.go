// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package stepattempt_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo/internal/stepattempt"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
	"github.com/superdurable/dex/sdk-go/dex"
)

type counter struct{ attempt int32 }

func newCounterRegistry() *stepattempt.Registry[counter] {
	return stepattempt.NewRegistry(func(ctx dex.Context) *counter { return &counter{attempt: ctx.Attempt()} })
}

func TestRegistrySharesOneValuePerAttemptAndReplacesItOnRetry(t *testing.T) {
	registry := newCounterRegistry()
	ctx := singledispatchtest.NewContext("flow-1", "step-1")
	first := registry.For(ctx)
	require.Same(t, first, registry.For(ctx))
	ctx.AdvanceAttempt()
	second := registry.For(ctx)
	require.NotSame(t, first, second)
	require.Equal(t, int32(2), second.attempt)
}

func TestRegistryReleasesValueWhenAttemptContextEnds(t *testing.T) {
	registry := newCounterRegistry()
	ctx := singledispatchtest.NewContext("flow-1", "step-1")
	cancel := ctx.SetDeadline(time.Now().Add(time.Minute))
	registry.For(ctx)
	_, isRegistered := registry.Lookup(ctx)
	require.True(t, isRegistered)
	cancel()
	require.Eventually(t, func() bool {
		_, isRegistered := registry.Lookup(ctx)
		return !isRegistered
	}, time.Second, time.Millisecond)
}
