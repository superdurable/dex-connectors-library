// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sdkgo_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestConnectorConfigurationUIValidatesComposition(t *testing.T) {
	configuration := sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{
		{
			ID: "approvalChannel", UnitID: "channelPicker", Label: "Approval channel", Required: true,
			Bindings: []sdkgo.ConnectorUIBinding{{Port: "channelId", JSONPointer: "/approval/channelId"}},
		},
		{
			ID: "messageTemplate", UnitID: "textInput", Label: "Message",
			Bindings: []sdkgo.ConnectorUIBinding{{Port: "text", JSONPointer: "/message~1template"}},
		},
	}}
	require.NoError(t, configuration.Validate())

	duplicate := configuration
	duplicate.Units = append(duplicate.Units, configuration.Units[0])
	require.ErrorContains(t, duplicate.Validate(), "duplicated")

	invalidPointer := configuration
	invalidPointer.Units = append([]sdkgo.ConnectorUIUnit(nil), configuration.Units...)
	invalidPointer.Units[0].Bindings = []sdkgo.ConnectorUIBinding{{Port: "channelId", JSONPointer: "/bad~2pointer"}}
	require.ErrorContains(t, invalidPointer.Validate(), "invalid JSON Pointer")
}

func TestConnectorConfigurationRefRequiresStableStepIdentity(t *testing.T) {
	reference := sdkgo.ConnectorConfigurationRef{
		ConnectorID: "slack", ConnectionName: "approvals", OperationID: "postMessage",
		FlowType: "ExpenseApprovalFlow", StepType: "RequestApproval",
	}
	require.NoError(t, reference.Validate())

	reference.StepType = ""
	require.ErrorContains(t, reference.Validate(), "Flow and Step types")
}

func TestStepConfigurationUIIsIsolatedFromCallerMutation(t *testing.T) {
	configuration := sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
		ID: "channel", UnitID: "channelPicker", Label: "Channel", Required: true,
		Bindings: []sdkgo.ConnectorUIBinding{{Port: "channelId", JSONPointer: "/channelId"}},
	}}}
	step := sdkgo.MustNewQueryStep(sdkgo.QueryStepConfig[string, string, string]{
		StepType: "ConfiguredQuery", Annotations: sdkgo.StepAnnotations{GroupID: "test", GroupLabel: "Test", Explanation: "configured query"},
		Operation: &factoryQuery{definition: queryDefinition(testQueryRef)}, Connection: testConnection,
		MapToOperationInput: func(input string) string { return input }, Branches: queryFactoryTargets(), ConfigurationUI: configuration,
	})

	configuration.Units[0].Label = "mutated"
	first := step.ConfigurationUI()
	require.Equal(t, "Channel", first.Units[0].Label)
	first.Units[0].Bindings[0].JSONPointer = "/mutated"
	require.Equal(t, "/channelId", step.ConfigurationUI().Units[0].Bindings[0].JSONPointer)
}
