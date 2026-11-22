// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sdkgo_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/internal/conformancefixture"
	"github.com/superdurable/dex-connectors-library/sdkgo/internal/testsupport"
)

// blockingMutation holds Invoke open until release closes, so a second call overlaps it.
type blockingMutation struct {
	ref     sdkgo.OperationRef
	started chan struct{}
	release chan struct{}
	invokes int
}

func newBlockingMutation(ref sdkgo.OperationRef) *blockingMutation {
	return &blockingMutation{ref: ref, started: make(chan struct{}), release: make(chan struct{})}
}

func (operation *blockingMutation) Definition() sdkgo.MutationDefinition {
	return mutationDefinition(operation.ref)
}

func (*blockingMutation) IdempotencyKey(sdkgo.CallID, string) sdkgo.IdempotencyKey { return "" }

func (operation *blockingMutation) Invoke(sdkgo.Call, string) sdkgo.MutationAttempt[string] {
	operation.invokes++
	close(operation.started)
	<-operation.release
	return sdkgo.NewMutationBranch(testMutationSucceeded, "ok", nil, sdkgo.Receipt{})
}

func TestConcurrentCallOfTheSameMutationIsRefusedBeforeSending(t *testing.T) {
	ctx := testsupport.NewDexContext("flow-concurrent", "step-concurrent")
	running := newBlockingMutation(testMutationRef)
	firstResult := make(chan sdkgo.MutationResult[string], 1)
	go func() {
		result, err := sdkgo.RunMutation(ctx, running, testConnection, "first")
		assert.NoError(t, err)
		firstResult <- result
	}()
	<-running.started

	concurrent := &mutationOperation{}
	refused, err := sdkgo.RunMutation(ctx, concurrent, testConnection, "second")
	require.NoError(t, err)
	require.Equal(t, sdkgo.DefectBranchID, refused.Branch)
	require.Contains(t, refused.Failure.Message, "concurrent calls")
	require.Empty(t, concurrent.call.ID, "the refused call never reaches Invoke")

	other := &otherMutation{}
	otherResult, err := sdkgo.RunMutation(ctx, other, testConnection, "other")
	require.NoError(t, err)
	require.Equal(t, testMutationSucceeded, otherResult.Branch, "another operation may run concurrently")

	close(running.release)
	require.Equal(t, testMutationSucceeded, (<-firstResult).Branch)
	sequential := &mutationOperation{}
	sequentialResult, err := sdkgo.RunMutation(ctx, sequential, testConnection, "third")
	require.NoError(t, err)
	require.Equal(t, testMutationSucceeded, sequentialResult.Branch, "a call after the first ends runs")
	require.Equal(t, 1, running.invokes)

	reference := testsupport.NewDexContext("flow-concurrent", "step-concurrent")
	referenceCalls := []*mutationOperation{{}, {}, {}}
	for _, operation := range referenceCalls {
		_, err := sdkgo.RunMutation(reference, operation, testConnection, "reference")
		require.NoError(t, err)
	}
	require.Equal(t, referenceCalls[2].call.ID, sequential.call.ID, "the refused call kept ordinal 2, so this call is occurrence 3")
}

func TestQueriesOfTheSameOperationMayOverlap(t *testing.T) {
	ctx := testsupport.NewDexContext("flow-queries", "step-queries")
	running := newBlockingMutation(testMutationRef)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, err := sdkgo.RunMutation(ctx, running, testConnection, "first")
		assert.NoError(t, err)
	}()
	<-running.started
	for range 2 {
		result, err := sdkgo.RunQuery(ctx, &queryOperation{}, testConnection, "profile")
		require.NoError(t, err)
		require.Equal(t, testQuerySucceeded, result.Branch)
	}
	close(running.release)
	<-done
}

func TestRunQueryWithNilEmbeddedContextCountsOccurrences(t *testing.T) {
	ctx := &testsupport.DexContext{Flow: "flow-nil", Step: "step-nil", AttemptNo: 1}
	first, second := &queryOperation{}, &queryOperation{}
	require.NotPanics(t, func() {
		_, err := sdkgo.RunQuery(ctx, first, testConnection, "profile")
		require.NoError(t, err)
		_, err = sdkgo.RunQuery(ctx, second, testConnection, "profile")
		require.NoError(t, err)
	})
	reference := testsupport.NewDexContext("flow-nil", "step-nil")
	referenceFirst, referenceSecond := &queryOperation{}, &queryOperation{}
	for _, operation := range []*queryOperation{referenceFirst, referenceSecond} {
		_, err := sdkgo.RunQuery(reference, operation, testConnection, "profile")
		require.NoError(t, err)
	}
	require.Equal(t, referenceFirst.call.ID, first.call.ID)
	require.Equal(t, referenceSecond.call.ID, second.call.ID, "the second call gets occurrence 2")
	require.NotEqual(t, first.call.ID, second.call.ID)
}

type otherMutation struct{ mutationOperation }

func (*otherMutation) Definition() sdkgo.MutationDefinition {
	return mutationDefinition(sdkgo.OperationRef{ConnectorID: "mock-provider", OperationID: "revokeCredit"})
}

func TestNextCallIDNamesTheNextCallWithoutCountingIt(t *testing.T) {
	ctx := testsupport.NewDexContext("flow-next", "step-next")
	named, err := sdkgo.NextCallID(ctx, testMutationRef)
	require.NoError(t, err)
	namedAgain, err := sdkgo.NextCallID(ctx, testMutationRef)
	require.NoError(t, err)
	require.Equal(t, named, namedAgain, "naming a call does not count it")

	first := &mutationOperation{}
	_, err = sdkgo.RunMutation(ctx, first, testConnection, "first")
	require.NoError(t, err)
	require.Equal(t, named, first.call.ID)

	namedSecond, err := sdkgo.NextCallID(ctx, testMutationRef)
	require.NoError(t, err)
	require.NotEqual(t, named, namedSecond, "after the first call starts, the next name is occurrence 2")
	second := &mutationOperation{}
	_, err = sdkgo.RunMutation(ctx, second, testConnection, "second")
	require.NoError(t, err)
	require.Equal(t, namedSecond, second.call.ID)

	namedQuery, err := sdkgo.NextCallID(ctx, testQueryRef)
	require.NoError(t, err)
	query := &queryOperation{}
	_, err = sdkgo.RunQuery(ctx, query, testConnection, "profile")
	require.NoError(t, err)
	require.Equal(t, namedQuery, query.call.ID, "each operation keeps its own count")

	namedSecondQuery, err := sdkgo.NextCallID(ctx, testQueryRef)
	require.NoError(t, err)
	require.NotEqual(t, namedQuery, namedSecondQuery, "after the first Query starts, the next name is occurrence 2")
	secondQuery := &queryOperation{}
	_, err = sdkgo.RunQuery(ctx, secondQuery, testConnection, "profile")
	require.NoError(t, err)
	require.Equal(t, namedSecondQuery, secondQuery.call.ID)

	ctx.AttemptNo = 2
	retried, err := sdkgo.NextCallID(ctx, testMutationRef)
	require.NoError(t, err)
	require.Equal(t, named, retried, "a retry restarts the count, so the first call keeps its name")
}

func TestNextCallIDUsesTheContractDerivation(t *testing.T) {
	ctx := testsupport.NewDexContext("flow-42", "step-execution-7")
	callID, err := sdkgo.NextCallID(ctx, sdkgo.OperationRef{ConnectorID: "sample", OperationID: "createItem"})
	require.NoError(t, err)
	require.Equal(t, sdkgo.CallID(conformancefixture.ExampleCallID), callID)
}

func TestNextCallIDRequiresAStepAndAValidOperation(t *testing.T) {
	_, err := sdkgo.NextCallID(nil, testMutationRef)
	require.ErrorContains(t, err, "Dex context is required")
	_, err = sdkgo.NextCallID(testsupport.NewDexContext("flow-rpc", ""), testMutationRef)
	require.ErrorContains(t, err, "RPC invocation is not allowed")
	_, err = sdkgo.NextCallID(testsupport.NewDexContext("flow-next", "step-next"), sdkgo.OperationRef{ConnectorID: "mock-provider", OperationID: "Invalid"})
	require.ErrorContains(t, err, "invalid connector operation")
}
