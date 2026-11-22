//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package integrationtest_test

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
	"github.com/superdurable/dex/sdk-go/dex"
)

var (
	priorSendsNoteOperation = sdkgo.OperationRef{ConnectorID: "fixture-notes", OperationID: "sendNote"}
	priorSendsTextOperation = sdkgo.OperationRef{ConnectorID: "fixture-texts", OperationID: "sendText"}
)

type priorSendsFlow struct{ dex.FlowDefaults }

func (*priorSendsFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{dex.DefineStartStep(priorSendsStep{})}
}

func (*priorSendsFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{}
}

// priorSendsStep makes two fenced writes on attempt 1 and reads their prior sends on attempt 2.
type priorSendsStep struct {
	dex.StepDefaultsNoWaitFor[dex.None]
}

func (priorSendsStep) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{
		ExecuteDurability:    dex.StepDurabilitySync,
		ExecuteMethodTimeout: 10 * time.Second,
		ExecuteRetry:         &dex.RetryPolicy{InitialInterval: 100 * time.Millisecond, BackoffCoefficient: 2, MaximumInterval: time.Second, MaximumAttempts: 2, TotalDuration: 30 * time.Second},
	}
}

func (priorSendsStep) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	notePriorSends, err := readPriorSendsOfNextCall(ctx, priorSendsNoteOperation)
	if err != nil {
		return nil, err
	}
	textPriorSends, err := readPriorSendsOfNextCall(ctx, priorSendsTextOperation)
	if err != nil {
		return nil, err
	}
	read := fmt.Sprintf("attempt %d: note %d, text %d", ctx.Attempt(), notePriorSends, textPriorSends)
	if ctx.Attempt() > 1 || notePriorSends != 0 || textPriorSends != 0 {
		return dex.GracefulComplete(read), nil
	}
	connection := sdkgo.ConnectionRef{Provider: "fixture", Name: "default"}
	var retryErr *sdkgo.RetryError
	if _, err := sdkgo.RunMutation(ctx, priorSendsFencedMutation{operation: priorSendsNoteOperation}, connection, "note"); !errors.As(err, &retryErr) {
		return dex.GracefulComplete("the note's lost answer did not return Retry"), nil
	}
	_, err = sdkgo.RunMutation(ctx, priorSendsFencedMutation{operation: priorSendsTextOperation, isRefused: true}, connection, "text")
	if !errors.As(err, &retryErr) {
		return dex.GracefulComplete("the text's 429 did not return Retry"), nil
	}
	return nil, err
}

func readPriorSendsOfNextCall(ctx dex.Context, operation sdkgo.OperationRef) (int32, error) {
	callID, err := sdkgo.NextCallID(ctx, operation)
	if err != nil {
		return 0, err
	}
	return singledispatch.MaximumPriorSends(ctx, callID)
}

// priorSendsFencedMutation sends once; the answer is lost, or a 429 proves the send was refused.
type priorSendsFencedMutation struct {
	operation sdkgo.OperationRef
	isRefused bool
}

func (mutation priorSendsFencedMutation) Definition() sdkgo.MutationDefinition {
	return sdkgo.MutationDefinition{
		Operation: mutation.operation,
		Branches: []sdkgo.BranchDefinition{
			{ID: "sent", Description: "The message was sent."},
			{ID: sdkgo.UncertainBranchID, Description: "The message may have been sent.", Optional: true},
			{ID: sdkgo.DefectBranchID, Description: "Local input or configuration is invalid.", Optional: true},
		},
		StepDefaults:    sdkgo.StepDefaults{ExecuteMethodTimeout: 10 * time.Second, ExecuteDurability: dex.StepDurabilitySync},
		RepeatOnUnknown: sdkgo.RepeatOnUnknownLimited,
		MaxSends:        3,
	}
}

func (priorSendsFencedMutation) IdempotencyKey(id sdkgo.CallID, _ string) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

func (mutation priorSendsFencedMutation) Invoke(call sdkgo.Call, _ string) sdkgo.MutationAttempt[string] {
	failure := sdkgo.Failure{Kind: sdkgo.FailureTransport, Provider: mutation.operation.ConnectorID, Operation: mutation.operation.OperationID, Message: "the message's outcome is unknown"}
	fence, err := singledispatch.NewFence(call, 10*time.Second, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return sdkgo.NewMutationRetry[string](failure, 0)
	}
	if fence.Decide().Kind != singledispatch.SendAllowed {
		return sdkgo.NewMutationUncertain("", failure, sdkgo.Receipt{})
	}
	if err := fence.Dispatch(func(context.Context) error { return nil }); err != nil {
		return sdkgo.NewMutationRetry[string](failure, 0)
	}
	if mutation.isRefused {
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[string](failure, 0)
		}
		failure.Kind, failure.Message = sdkgo.FailureRateLimit, "the provider answered 429"
	}
	return sdkgo.NewMutationRetry[string](failure, 0)
}

// TestMaximumPriorSendsReadsEarlierAttemptsWithRealDex reads a claim and a refusal across a real Dex retry.
func TestMaximumPriorSendsReadsEarlierAttemptsWithRealDex(t *testing.T) {
	flow := &priorSendsFlow{}
	harness := newDexHarness(t, []dex.Flow{flow})
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := "prior-sends-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err := harness.client.StartFlow(ctx, flow, flowID, nil, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status)
	var output string
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, "attempt 2: note 1, text 0", output)
}
