//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package incidentacknowledgement

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/teams/go"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/teams/go/teamsmock"
)

func TestIncidentAcknowledgementUsesTeamsMockDefaultsWithRealDex(t *testing.T) {
	mock := teamsmock.New(t, ConnectionName)
	policy := AcknowledgementPolicy{CheckInterval: time.Second, MaximumChecks: 3}
	flow := NewFlow(mock.Connection(), ChannelSelection{TeamID: integrationTeamID, ChannelID: integrationChannelID}, EscalationChatSelection{}, &policy)
	harness := newIncidentHarnessForFlow(t, flow, true)
	input := incidentInput()
	ctx := integrationContext(t)
	record := harness.waitForFlowOutput(t, ctx, harness.startIncident(t, ctx, "mock-default", input))

	require.Equal(t, PhaseAcknowledged, record.Phase)
	require.Equal(t, input.Title, record.Update.Title)
	require.Equal(t, input.IncidentID, record.Update.IncidentID)
	require.Equal(t, 1, record.ReplyChecks)
	require.NotNil(t, record.Acknowledgement)
	require.Equal(t, "1616990032040", record.Acknowledgement.MessageID)

	posts := mock.PostChannelMessage().Calls()
	require.Len(t, posts, 1)
	require.Equal(t, teams.PostChannelMessageInput{
		TeamID: integrationTeamID, ChannelID: integrationChannelID,
		Subject:     "INC-1042 [SEV2]: Checkout latency above SLO",
		Content:     "<p>p95 checkout latency is 4.2 s &amp; rising<br>Owner: payments on-call</p>",
		ContentType: teams.ContentTypeHTML, Importance: teams.MessageImportanceHigh,
	}, posts[0].Input)

	replies := mock.PostThreadReply().Calls()
	require.Len(t, replies, 1)
	require.Equal(t, teams.PostThreadReplyInput{
		TeamID: integrationTeamID, ChannelID: integrationChannelID, MessageID: "1616990032035",
		Content: "Status: SEV2 incident INC-1042 is being investigated. Reply ack in this thread to acknowledge.",
	}, replies[0].Input)

	reads := mock.ListThreadReplies().Calls()
	require.Len(t, reads, 1)
	require.Equal(t, teams.ListThreadRepliesInput{
		TeamID: integrationTeamID, ChannelID: integrationChannelID, MessageID: "1616990032035",
		PageSize: 50, MaxTextCharacters: 2000,
	}, reads[0].Input)
	require.Empty(t, mock.PostChatMessage().Calls())
}
