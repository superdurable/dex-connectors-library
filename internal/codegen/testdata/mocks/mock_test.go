// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mockfixture_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"example.com/mockfixture"
	"example.com/mockfixture/mockfixturemock"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestGeneratedStepFactoryInvokesTheMockedOperation(t *testing.T) {
	mock := mockfixturemock.New(t, "primary")
	step := mockfixture.NewGetWidgetStep(mockfixture.GetWidgetStepConfig[mockfixture.GetWidgetInput]{
		StepType:            "GetWidget",
		Annotations:         sdkgo.StepAnnotations{GroupID: "widgets", GroupLabel: "Widgets", Explanation: "Read the widget."},
		Connection:          mock.Connection(),
		ConnectionName:      "primary",
		MapToOperationInput: func(input mockfixture.GetWidgetInput) mockfixture.GetWidgetInput { return input },
		Found:               sdkgo.GoTo(sdkgo.StepRef[mockfixture.GetWidgetResult]("WidgetFound")),
	})

	decision, err := step.Execute(newStepContext("flow-1", "step-1"), mockfixture.GetWidgetInput{ID: "w-1"})

	require.NoError(t, err)
	require.NotNil(t, decision)
	calls := mock.GetWidget().Calls()
	require.Len(t, calls, 1)
	require.Equal(t, mockfixture.GetWidgetBranchFound, calls[0].Branch)
	require.Equal(t, sdkgo.ConnectionRef{Provider: "example", Name: "primary"}, calls[0].Connection)
}

func TestGeneratedDefaultsAndManifestMocksAnswerCalls(t *testing.T) {
	mock := mockfixturemock.New(t, "primary")
	connection := sdkgo.ConnectionRef{Provider: "example", Name: "primary"}

	found, err := sdkgo.RunQuery(newStepContext("flow-1", "step-1"), mock.GetWidget(), connection, mockfixture.GetWidgetInput{ID: "w-1"})
	require.NoError(t, err)
	require.Equal(t, mockfixture.Widget{
		ID: "w-1", Name: "Gear", CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), Tags: []string{"metal", "007"},
	}, found.Value)

	mock.GetWidget().Respond(mockfixturemock.GetWidgetMissing(), mockfixturemock.GetWidgetThrottled())
	missing, err := sdkgo.RunQuery(newStepContext("flow-1", "step-2"), mock.GetWidget(), connection, mockfixture.GetWidgetInput{ID: "w-2"})
	require.NoError(t, err)
	require.Equal(t, mockfixture.GetWidgetBranchNotFound, missing.Branch)
	require.Equal(t, sdkgo.Failure{
		Kind: sdkgo.FailureNotFound, Provider: "mock-fixture", Operation: "getWidget", Message: "The widget does not exist.",
	}, *missing.Failure)
	_, err = sdkgo.RunQuery(newStepContext("flow-1", "step-3"), mock.GetWidget(), connection, mockfixture.GetWidgetInput{ID: "w-3"})
	var retryError *sdkgo.RetryError
	require.ErrorAs(t, err, &retryError)
	require.Equal(t, sdkgo.FailureRateLimit, retryError.Failure.Kind)

	mock.CreateWidget().Respond(mockfixturemock.CreateWidgetConnectionLost())
	uncertain, err := sdkgo.RunMutation(newStepContext("flow-1", "step-4"), mock.CreateWidget(), connection, mockfixture.CreateWidgetInput{Name: "Gear"})
	require.NoError(t, err)
	require.Equal(t, sdkgo.UncertainBranchID, uncertain.Branch)
}

func TestGeneratedPagedDefaultFollowsNextPage(t *testing.T) {
	mock := mockfixturemock.New(t, "primary")
	connection := sdkgo.ConnectionRef{Provider: "example", Name: "primary"}

	var identifiers []string
	input := mockfixture.ListWidgetsInput{}
	for step := 1; ; step++ {
		page, err := sdkgo.RunQuery(newStepContext("flow-1", fmt.Sprintf("step-%d", step)), mock.ListWidgets(), connection, input)
		require.NoError(t, err)
		require.Equal(t, mockfixture.ListWidgetsBranchListed, page.Branch)
		for _, widget := range page.Value.Widgets {
			identifiers = append(identifiers, widget.ID)
		}
		if page.Value.NextPage == 0 {
			break
		}
		input.Page = page.Value.NextPage
	}

	require.Equal(t, []string{"w-3", "w-1", "w-1", "w-9"}, identifiers)
	require.Len(t, mock.ListWidgets().Calls(), 3)
}

func TestMockedConnectionServesOnlyOperationSteps(t *testing.T) {
	_, err := mockfixture.NewConnectionWithOperations(nil, sdkgo.ConnectionRef{Provider: "example", Name: "primary"})
	require.ErrorContains(t, err, "operations are required")
}

type stepContext struct {
	context.Context
	flowID          string
	stepExecutionID string
}

func newStepContext(flowID, stepExecutionID string) *stepContext {
	return &stepContext{Context: context.Background(), flowID: flowID, stepExecutionID: stepExecutionID}
}

func (ctx *stepContext) FlowID() string                              { return ctx.flowID }
func (*stepContext) RunID() string                                   { return "run-1" }
func (*stepContext) FlowStartedAt() time.Time                        { return time.Time{} }
func (ctx *stepContext) StepExecutionID() string                     { return ctx.stepExecutionID }
func (*stepContext) FromStepExecutionID() string                     { return "" }
func (*stepContext) RecoveryError() *dex.RecoveryErrorInfo           { return nil }
func (*stepContext) FirstAttemptAt() time.Time                       { return time.Time{} }
func (*stepContext) Attempt() int32                                  { return 1 }
func (*stepContext) HasTimerFired() bool                             { return false }
func (*stepContext) HasTimerFiredByIndex(int) bool                   { return false }
func (*stepContext) WaitForMethodFailed() bool                       { return false }
func (*stepContext) RecordHeartbeat(any) error                       { return nil }
func (*stepContext) GetLastHeartbeatValue(any) (bool, error)         { return false, nil }
func (*stepContext) SetStepExecutionLocal(string, any) error         { return nil }
func (*stepContext) GetStepExecutionLocal(string, any) (bool, error) { return false, nil }
func (*stepContext) RecordEvent(string, any) error                   { return nil }

var _ dex.Context = (*stepContext)(nil)
