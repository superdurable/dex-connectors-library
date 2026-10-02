// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	incidentacknowledgement "github.com/superdurable/dex-connectors-library/connectors/microsoft/teams/examples/incident-acknowledgement/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
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
	store := writeStore(t, "")
	_, err := loadChannelSelection(store)
	require.ErrorContains(t, err, "Incident team and Incident channel pickers")
	escalation, err := loadEscalationChatSelection(store)
	require.NoError(t, err)
	require.Equal(t, incidentacknowledgement.EscalationChatSelection{}, escalation)
}

func TestSavedPickerValuesAreLoaded(t *testing.T) {
	store := writeStore(t, `{
  "schemaVersion": "connectors.dex.dev/local-use-configurations/v1alpha1",
  "operationConfigurations": [
    {"connectorId": "microsoft-teams", "connectionName": "microsoft-teams", "operationId": "postChannelMessage",
     "flowType": "TeamsIncidentAcknowledgement", "stepType": "PostIncidentUpdate",
     "configuration": {"teamId": "`+testTeamID+`", "teamName": "Operations", "channelId": "`+testChannelID+`", "channelName": "Incidents"}},
    {"connectorId": "microsoft-teams", "connectionName": "microsoft-teams", "operationId": "postChatMessage",
     "flowType": "TeamsIncidentAcknowledgement", "stepType": "EscalateToChat",
     "configuration": {"chatId": "19:meeting_MjdhNjM4YzUtYzExZi00@thread.v2", "chatName": "On-call managers"}}
  ]
}`)
	channel, err := loadChannelSelection(store)
	require.NoError(t, err)
	require.Equal(t, incidentacknowledgement.ChannelSelection{TeamID: testTeamID, TeamName: "Operations", ChannelID: testChannelID, ChannelName: "Incidents"}, channel)
	escalation, err := loadEscalationChatSelection(store)
	require.NoError(t, err)
	require.Equal(t, "19:meeting_MjdhNjM4YzUtYzExZi00@thread.v2", escalation.ChatID)
}

func writeStore(t *testing.T, useConfigurations string) *localconfig.Store {
	t.Helper()
	directory := t.TempDir()
	connectionsPath := filepath.Join(directory, "connections.json")
	require.NoError(t, os.WriteFile(connectionsPath, []byte(`{"schemaVersion":"connectors.dex.dev/local-connections/v1alpha1","connections":[{
  "connectorId": "microsoft-teams", "modulePath": "github.com/superdurable/dex-connectors-library/connectors/microsoft/teams",
  "moduleVersion": "v0.1.0", "provider": "microsoft", "connectionName": "microsoft-teams", "configuration": {},
  "credentials": {"oauth_client_id": "client-id", "oauth_client_secret": "client-secret", "access_token": "access", "refresh_token": "refresh"}
}]}`), 0o600))
	if useConfigurations != "" {
		require.NoError(t, os.WriteFile(filepath.Join(directory, "use-configurations.json"), []byte(useConfigurations), 0o600))
	}
	store, err := localconfig.LoadFile(connectionsPath)
	require.NoError(t, err)
	return store
}
