// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pagechanged

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/confluence/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func sampleTriggerEvent(kind confluence.PageChangeKind) sdkgo.TriggerEvent[confluence.PageChangedEvent] {
	return sdkgo.TriggerEvent[confluence.PageChangedEvent]{
		ID: "confluence-page-0123456789abcdef0123456789abcdef",
		Payload: confluence.PageChangedEvent{
			CloudID: "11223344-a1b2-4b33-8c44-def123456789", ChangeKind: kind,
			Page: confluence.PageSummary{ID: "600002", Title: "Remote work policy", SpaceKey: "OPS", VersionNumber: 2},
		},
	}
}

func TestTriggerMappingNamesOneFlowPerPageVersion(t *testing.T) {
	event := sampleTriggerEvent(confluence.PageChangeUpdated)
	require.Equal(t, "confluence-page-change-confluence-page-0123456789abcdef0123456789abcdef", ResolveFlowID(event))
	input := MapToFlowInput(event)
	require.Equal(t, Input{TriggerEventID: event.ID, Change: event.Payload}, input)
	require.True(t, AcceptEveryPageChange(event))
	require.Equal(t, confluence.GetPageInput{PageID: "600002", MaxBodyCharacters: MaxRecordedBodyCharacters}, MapToGetPageInput(input))
}

func TestValidateInputRejectsIncompleteChanges(t *testing.T) {
	for _, kind := range []confluence.PageChangeKind{confluence.PageChangeCreated, confluence.PageChangeUpdated} {
		require.NoError(t, validateInput(MapToFlowInput(sampleTriggerEvent(kind))))
	}
	missingID := MapToFlowInput(sampleTriggerEvent(confluence.PageChangeCreated))
	missingID.TriggerEventID = " "
	require.ErrorContains(t, validateInput(missingID), "triggerEventId")
	missingPage := MapToFlowInput(sampleTriggerEvent(confluence.PageChangeCreated))
	missingPage.Change.Page.VersionNumber = 0
	require.ErrorContains(t, validateInput(missingPage), "change.page.versionNumber")
	unknownKind := MapToFlowInput(sampleTriggerEvent("deleted"))
	require.ErrorContains(t, validateInput(unknownKind), "changeKind")
}

func TestTriggerBindingShowsOneGuidedSpacePicker(t *testing.T) {
	bindings := (&Flow{}).GetConnectorTriggerBindings()
	require.Len(t, bindings, 1)
	binding := bindings[0]
	require.NoError(t, binding.Validate())
	require.Equal(t, confluence.PageChangedTriggerDefinition, binding.Definition)
	require.Equal(t, ConnectionName, binding.ConnectionName)
	require.Equal(t, PageChangedTriggerBinding, binding.BindingName)
	require.NotNil(t, binding.ConfigurationUI)
	require.Len(t, binding.ConfigurationUI.Units, 1, "the picker is the only visible Trigger field")
	unit := binding.ConfigurationUI.Units[0]
	require.Equal(t, confluence.UIUnitSpacePicker, unit.UnitID)
	require.Equal(t, "Watched space", unit.Label)
	require.False(t, unit.Required, "an unsaved picker watches every visible space")
	for _, guidance := range []string{"current spaces", "space ID, key, and name", "not secret", "https://<your-site>.atlassian.net/wiki/spaces", "unsaved", "every space", "baseline", "Restart the Worker"} {
		require.Contains(t, unit.Description, guidance)
	}
	require.Equal(t, []sdkgo.ConnectorUIBinding{
		{Port: confluence.UISpacePickerPortSpaceID, JSONPointer: "/spaceId"},
		{Port: confluence.UISpacePickerPortSpaceKey, JSONPointer: "/spaceKey"},
		{Port: confluence.UISpacePickerPortSpaceName, JSONPointer: "/spaceName"},
	}, unit.Bindings)
}

func TestFlowIdentitiesAndRegistration(t *testing.T) {
	flow := NewFlow(confluence.Connection{})
	require.Equal(t, FlowType, dex.GetFinalFlowType(flow))
	require.Equal(t, recordPageChangeStepType, dex.GetFinalStepType[Input](recordPageChange{}))
	require.Equal(t, completePageChangeStepType, dex.GetFinalStepType[confluence.GetPageResult](completePageChange{}))
	wait, err := recordPageChange{}.WaitFor(nil, Input{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)

	client, err := confluence.New(confluence.Config{}, sdkgo.StaticCredentialProvider[confluence.Credentials]{})
	require.NoError(t, err)
	connection, err := confluence.NewConnection(client, sdkgo.ConnectionRef{Provider: "atlassian", Name: ConnectionName})
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection)})
	require.NoError(t, err)
}
