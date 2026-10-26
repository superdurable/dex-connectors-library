//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package integrationtest_test

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
	"github.com/superdurable/dex/sdk-go/dex"
)

type singleDispatchCheckpointFlow struct{ dex.FlowDefaults }

func (*singleDispatchCheckpointFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{dex.DefineStartStep(singleDispatchCheckpointStep{})}
}

func (*singleDispatchCheckpointFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{}
}

type singleDispatchCheckpointStep struct {
	dex.StepDefaultsNoWaitFor[dex.None]
}

func (singleDispatchCheckpointStep) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{
		ExecuteDurability:    dex.StepDurabilitySync,
		ExecuteMethodTimeout: 10 * time.Second,
		ExecuteRetry:         &dex.RetryPolicy{InitialInterval: 100 * time.Millisecond, BackoffCoefficient: 2, MaximumInterval: time.Second, MaximumAttempts: 2, TotalDuration: 30 * time.Second},
	}
}

func (singleDispatchCheckpointStep) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	call := sdkgo.Call{Context: ctx, ID: "single-dispatch-integration-call"}
	fence, err := singledispatch.NewFence(call, 10*time.Second)
	if err != nil {
		return nil, err
	}
	decision := fence.Decide()
	if decision.Kind != singledispatch.SendAllowed {
		return nil, errors.New("persisted notSent marker did not permit next attempt")
	}
	if ctx.Attempt() == 1 {
		if err := fence.RecordNotSent(); err != nil {
			return nil, err
		}
		return nil, errors.New("retry after checkpoint")
	}
	if decision.Marker.Attempt != 1 || decision.Marker.State != singledispatch.StateNotSent {
		return nil, errors.New("next attempt did not see first attempt's checkpoint")
	}
	return dex.GracefulComplete("checkpoint-visible"), nil
}

// TestSingleDispatchMarkerSurvivesRetryWithRealDex checks sync heartbeat transfer across Worker attempts.
func TestSingleDispatchMarkerSurvivesRetryWithRealDex(t *testing.T) {
	flow := &singleDispatchCheckpointFlow{}
	harness := newDexHarness(t, []dex.Flow{flow})
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := "single-dispatch-checkpoint-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err := harness.client.StartFlow(ctx, flow, flowID, nil, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status)
	var output string
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, "checkpoint-visible", output)
}
