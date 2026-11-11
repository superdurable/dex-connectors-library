// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package channellookup

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/slack/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestFlowUsesStaticConnectionAndStepIdentities(t *testing.T) {
	reference := sdkgo.ConnectionRef{Provider: "slack", Name: ConnectionName}
	client, err := slack.New(slack.Config{}, sdkgo.StaticCredentialProvider[slack.Credentials]{})
	require.NoError(t, err)
	connection, err := slack.NewConnection(client, reference)
	require.NoError(t, err)
	flow := NewFlow(connection)
	_, err = dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	require.Equal(t, "SlackChannelLookup", dex.GetFinalFlowType(flow))
	require.Equal(t, "StartSlackChannelLookup", dex.GetFinalStepType[LookupRequest](startLookup{}))
	require.Equal(t, "RecordSlackWorkspace", dex.GetFinalStepType[slack.GetWorkspaceIdentityResult](recordWorkspace{}))
	require.Equal(t, "CompleteSlackChannelLookup", dex.GetFinalStepType[slack.ListChannelsResult](completeLookup{}))
	wait, err := startLookup{}.WaitFor(nil, LookupRequest{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
}

func TestMapToOperationInputs(t *testing.T) {
	request := LookupRequest{NameFilter: "#general", MemberOnly: true, Limit: 50}
	require.Equal(t, slack.GetWorkspaceIdentityInput{}, MapToWorkspaceIdentityInput(request))
	require.Equal(t, request, MapToListChannelsInput(request))
}
