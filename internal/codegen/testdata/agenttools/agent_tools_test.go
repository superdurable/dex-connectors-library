// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mockfixture_test

import (
	"encoding/json"
	"testing"

	"example.com/mockfixture"
	"example.com/mockfixture/mockfixturemock"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestGeneratedAgentToolsPassSDKValidationAndRunThroughMocks(t *testing.T) {
	mock := mockfixturemock.New(t, "primary")
	connection := mock.Connection()

	tools, err := connection.AgentTools()

	require.NoError(t, err)
	exposed := mockfixture.ExposedAgentTools()
	require.Len(t, tools, len(exposed))
	require.Len(t, tools, 2, "listWidgets is not exposed")
	for index, tool := range tools {
		require.Equal(t, exposed[index], tool.Metadata())
		require.Equal(t, connection.Reference(), tool.Connection())
	}
	require.Equal(t, sdkgo.ConnectionRef{Provider: "example", Name: "primary"}, connection.Reference())
	require.NotNil(t, connection.Operations())
	getWidget, createWidget := tools[0].Metadata(), tools[1].Metadata()
	require.Equal(t, "mock-fixture_get_widget", getWidget.ToolName)
	require.Equal(t, sdkgo.AgentToolEffectRead, getWidget.Effect())
	require.Equal(t, mockfixture.GetWidgetDefinition.Operation, getWidget.Operation)
	require.Equal(t, "Read one widget by its ID.", getWidget.Description)
	require.Equal(t, "mock-fixture_create_widget", createWidget.ToolName)
	require.Equal(t, sdkgo.AgentToolEffectIdempotentWrite, createWidget.Effect())
	require.True(t, createWidget.IsApprovalRecommended)
	require.Equal(t, "Create one widget.", createWidget.Description, "the operation description is the fallback")
	require.Equal(t, map[sdkgo.BranchID]sdkgo.AgentBranchOutcome{
		mockfixture.CreateWidgetBranchUncertain: sdkgo.AgentBranchOutcomeUnknown,
		mockfixture.CreateWidgetBranchDefect:    sdkgo.AgentBranchOutcomeRejected,
	}, createWidget.BranchOutcomes)
	require.Equal(t, mockfixture.CreateWidgetDefinition.StepDefaults, createWidget.StepDefaults)

	found, err := tools[0].RunAttempt(newStepContext("flow-1", "step-1"), json.RawMessage(`{"id":"w-1"}`))
	require.NoError(t, err)
	require.Equal(t, sdkgo.AgentToolAttemptSucceeded, found.Outcome)
	require.Equal(t, mockfixture.GetWidgetBranchFound, found.Branch)
	require.Contains(t, string(found.Value), `"name":"Gear"`)

	mock.GetWidget().Respond(mockfixturemock.GetWidgetMissing())
	missing, err := tools[0].RunAttempt(newStepContext("flow-2", "step-1"), json.RawMessage(`{"id":"w-2"}`))
	require.NoError(t, err)
	require.Equal(t, sdkgo.AgentToolAttemptRejected, missing.Outcome)

	mock.CreateWidget().Respond(mockfixturemock.CreateWidgetConnectionLost())
	lost, err := tools[1].RunAttempt(newStepContext("flow-3", "step-1"), json.RawMessage(`{"name":"Gear"}`))
	require.NoError(t, err)
	require.Equal(t, sdkgo.AgentToolAttemptUnknown, lost.Outcome)

	_, err = tools[0].RunAttempt(newStepContext("flow-4", "step-1"), json.RawMessage(`{"colour":"red"}`))
	require.ErrorIs(t, err, sdkgo.ErrAgentToolArgumentsInvalid)
}

func TestGeneratedAgentToolsRejectAZeroConnection(t *testing.T) {
	_, err := mockfixture.Connection{}.AgentTools()

	require.ErrorContains(t, err, "mock-fixture connector connection is required")
}

func TestExposedAgentToolsReturnsIndependentMetadata(t *testing.T) {
	first := mockfixture.ExposedAgentTools()
	first[1].BranchOutcomes[mockfixture.CreateWidgetBranchDefect] = sdkgo.AgentBranchOutcomeUnknown
	first[0].InputSchema[0] = '['

	second := mockfixture.ExposedAgentTools()

	require.Equal(t, sdkgo.AgentBranchOutcomeRejected, second[1].BranchOutcomes[mockfixture.CreateWidgetBranchDefect])
	require.Equal(t, byte('{'), second[0].InputSchema[0])
}
