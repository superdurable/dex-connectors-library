// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package teams_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/teams"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type postedMessageTarget struct {
	dex.StepDefaultsNoWaitFor[teams.PostChannelMessageResult]
}

func (postedMessageTarget) Execute(dex.Context, teams.PostChannelMessageResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

func TestPostChannelMessageFactoryRequiresOnlyTheHappyPath(t *testing.T) {
	connection, err := teams.NewConnection(newTeamsClient(t, "http://127.0.0.1:1/v1.0"), teamsConnection)
	require.NoError(t, err)
	annotations := sdkgo.StepAnnotations{GroupID: "teams", GroupLabel: "Microsoft Teams", Explanation: "Post a message."}
	mapToInput := func(string) teams.PostChannelMessageInput { return teams.PostChannelMessageInput{} }
	require.NotPanics(t, func() {
		teams.NewPostChannelMessageStep(teams.PostChannelMessageStepConfig[string]{
			StepType: "Post", Annotations: annotations, Connection: connection, ConnectionName: teamsConnection.Name,
			MapToOperationInput: mapToInput, Sent: sdkgo.GoTo(postedMessageTarget{}),
		})
	})
	require.Panics(t, func() {
		teams.NewPostChannelMessageStep(teams.PostChannelMessageStepConfig[string]{
			StepType: "Post", Annotations: annotations, Connection: connection, ConnectionName: teamsConnection.Name,
			MapToOperationInput: mapToInput, Uncertain: sdkgo.GoTo(postedMessageTarget{}),
		})
	})
	require.Panics(t, func() {
		teams.NewPostChannelMessageStep(teams.PostChannelMessageStepConfig[string]{
			StepType: "Post", Annotations: annotations, Connection: connection, ConnectionName: "another-connection",
			MapToOperationInput: mapToInput, Sent: sdkgo.GoTo(postedMessageTarget{}),
		})
	})
}

// TestDuplicateDispatchDecisionsPerOperation records how each operation stays safe under a repeated dispatch.
func TestDuplicateDispatchDecisionsPerOperation(t *testing.T) {
	for _, test := range []struct {
		operation    string
		branches     []sdkgo.BranchDefinition
		defaults     sdkgo.StepDefaults
		hasUncertain bool
		durability   dex.StepDurability
	}{
		// A read can repeat freely.
		{"listThreadReplies", teams.ListThreadRepliesDefinition.Branches, teams.ListThreadRepliesDefinition.StepDefaults, false, dex.StepDurabilityAsync},
		// Only the heartbeat checkpoint prevents a second message, and an async backup attempt cannot see it.
		{"postChannelMessage", teams.PostChannelMessageDefinition.Branches, teams.PostChannelMessageDefinition.StepDefaults, true, dex.StepDurabilitySync},
		{"postThreadReply", teams.PostThreadReplyDefinition.Branches, teams.PostThreadReplyDefinition.StepDefaults, true, dex.StepDurabilitySync},
		{"postChatMessage", teams.PostChatMessageDefinition.Branches, teams.PostChatMessageDefinition.StepDefaults, true, dex.StepDurabilitySync},
	} {
		required, hasUncertain := 0, false
		for _, branch := range test.branches {
			if branch.ID == sdkgo.UncertainBranchID {
				hasUncertain = true
			}
			if !branch.Optional {
				required++
			}
		}
		require.Equal(t, 1, required, test.operation)
		require.Equal(t, test.hasUncertain, hasUncertain, test.operation)
		require.Equal(t, test.durability, test.defaults.ExecuteDurability, test.operation)
	}
}

func TestTeamsConnectionCannotBeSerialized(t *testing.T) {
	connection, err := teams.NewConnection(newTeamsClient(t, "http://127.0.0.1:1/v1.0"), teamsConnection)
	require.NoError(t, err)
	_, err = json.Marshal(connection)
	require.Error(t, err)
	require.Equal(t, "teams.Connection{[REDACTED]}", fmt.Sprint(connection))
}

func TestNewRejectsUnsafeConfiguration(t *testing.T) {
	for name, config := range map[string]teams.Config{
		"plain HTTP host":    {Endpoint: "http://graph.microsoft.com/v1.0"},
		"query in endpoint":  {Endpoint: "https://graph.microsoft.com/v1.0?x=1"},
		"user in endpoint":   {Endpoint: "https://user@graph.microsoft.com/v1.0"},
		"negative limit":     {MaxResponseBytes: -1},
		"negative msg limit": {MaxMessageBytes: -5},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := teams.New(config, staticTeamsCredentials())
			require.Error(t, err)
		})
	}
	_, err := teams.New(teams.Config{}, nil)
	require.Error(t, err)
	client, err := teams.New(teams.Config{}, staticTeamsCredentials())
	require.NoError(t, err, "blank fields take the manifest defaults")
	require.NotNil(t, client)
}
