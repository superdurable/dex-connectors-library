// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	incidentacknowledgement "github.com/superdurable/dex-connectors-library/connectors/microsoft/teams/examples/incident-acknowledgement/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

const (
	testTeamID    = "fbe2bf47-16c8-47cf-b4a5-4b9b187c508b"
	testChannelID = "19:4a95f7d8db4c4e7fae857bcebe0623e6@thread.tacv2"
)

func TestEnvironmentOr(t *testing.T) {
	t.Setenv("TEAMS_EXAMPLE_VALUE", "configured")
	require.Equal(t, "configured", environmentOr("TEAMS_EXAMPLE_VALUE", "fallback"))
	require.Equal(t, "fallback", environmentOr("TEAMS_EXAMPLE_MISSING", "fallback"))
}

func TestTheChannelIsRequiredAndTheEscalationChatIsOptional(t *testing.T) {
	configuration := projectconfig.Configuration{}
	_, err := loadChannelSelection(configuration)
	require.ErrorContains(t, err, "Incident team and Incident channel pickers")
	escalation, err := loadEscalationChatSelection(configuration)
	require.NoError(t, err)
	require.Equal(t, incidentacknowledgement.EscalationChatSelection{}, escalation)
}

func TestSavedPickerValuesAreLoaded(t *testing.T) {
	configuration := projectconfig.Configuration{OperationConfigurations: []projectconfig.OperationConfiguration{
		{
			ConnectorID: "microsoft-teams", ConnectionName: "microsoft-teams", OperationID: "postChannelMessage",
			FlowType: "TeamsIncidentAcknowledgement", StepType: "PostIncidentUpdate",
			Configuration: json.RawMessage(`{"teamId": "` + testTeamID + `", "teamName": "Operations", "channelId": "` + testChannelID + `", "channelName": "Incidents"}`),
		},
		{
			ConnectorID: "microsoft-teams", ConnectionName: "microsoft-teams", OperationID: "postChatMessage",
			FlowType: "TeamsIncidentAcknowledgement", StepType: "EscalateToChat",
			Configuration: json.RawMessage(`{"chatId": "19:meeting_MjdhNjM4YzUtYzExZi00@thread.v2", "chatName": "On-call managers"}`),
		},
	}}
	channel, err := loadChannelSelection(configuration)
	require.NoError(t, err)
	require.Equal(t, incidentacknowledgement.ChannelSelection{TeamID: testTeamID, TeamName: "Operations", ChannelID: testChannelID, ChannelName: "Incidents"}, channel)
	escalation, err := loadEscalationChatSelection(configuration)
	require.NoError(t, err)
	require.Equal(t, "19:meeting_MjdhNjM4YzUtYzExZi00@thread.v2", escalation.ChatID)
}
