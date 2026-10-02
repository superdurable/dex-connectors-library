// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package incidentacknowledgement

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/teams"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	unitTeamID    = "fbe2bf47-16c8-47cf-b4a5-4b9b187c508b"
	unitChannelID = "19:4a95f7d8db4c4e7fae857bcebe0623e6@thread.tacv2"
	posterUserID  = "8ea0e38b-efb3-4757-924a-5f94061cf8c2"
	humanUserID   = "5f1c2d3e-4b5a-4c6d-8e7f-901a2b3c4d5e"
)

func TestBuildIncidentUpdateValidatesStartFlowInput(t *testing.T) {
	update, err := BuildIncidentUpdate(Input{IncidentID: " INC-1042 ", Title: " Checkout latency ", Severity: "SEV2", Summary: " p95 is 4.2 s "})
	require.NoError(t, err)
	require.Equal(t, IncidentUpdate{IncidentID: "INC-1042", Title: "Checkout latency", Severity: "SEV2", Summary: "p95 is 4.2 s", AcknowledgementPhrase: "ack"}, update)
	update, err = BuildIncidentUpdate(Input{IncidentID: "INC-1", Title: "t", Severity: "SEV1", Summary: "s", AcknowledgementPhrase: " On It "})
	require.NoError(t, err)
	require.Equal(t, "on it", update.AcknowledgementPhrase)
	for _, input := range []Input{
		{Title: "t", Severity: "SEV1", Summary: "s"},
		{IncidentID: "INC 1", Title: "t", Severity: "SEV1", Summary: "s"},
		{IncidentID: "INC-1", Title: "one\ntwo", Severity: "SEV1", Summary: "s"},
		{IncidentID: "INC-1", Title: "t", Severity: "[SEV1]", Summary: "s"},
		{IncidentID: "INC-1", Title: "t", Severity: "SEV1", Summary: " "},
	} {
		_, err := BuildIncidentUpdate(input)
		require.Error(t, err, "%+v", input)
	}
}

func TestMappersBuildTheConnectorInputs(t *testing.T) {
	flow := NewFlow(newUnitConnection(t), ChannelSelection{TeamID: unitTeamID, ChannelID: unitChannelID}, EscalationChatSelection{}, policyForUnitTests())
	update := IncidentUpdate{IncidentID: "INC-1042", Title: "Checkout latency", Severity: "SEV2", Summary: "p95 <4.2 s>\nOwner: payments", AcknowledgementPhrase: "ack"}
	require.Equal(t, teams.PostChannelMessageInput{
		TeamID: unitTeamID, ChannelID: unitChannelID, Subject: "INC-1042 [SEV2]: Checkout latency",
		Content: "<p>p95 &lt;4.2 s&gt;<br>Owner: payments</p>", ContentType: teams.ContentTypeHTML, Importance: teams.MessageImportanceHigh,
	}, flow.MapToPostChannelMessageInput(update))
	require.Equal(t, "Status: SEV2 incident INC-1042 is being investigated. Reply ack in this thread to acknowledge.", BuildStatusReplyText(update))
	require.Equal(t, teams.ListThreadRepliesInput{TeamID: unitTeamID, ChannelID: unitChannelID, MessageID: "1616989510408", PageSize: 50, MaxTextCharacters: 2000},
		MapToListThreadRepliesInput(ThreadCheck{TeamID: unitTeamID, ChannelID: unitChannelID, RootMessageID: "1616989510408"}))
	require.Equal(t, teams.PostThreadReplyInput{TeamID: unitTeamID, ChannelID: unitChannelID, MessageID: "1616989510408", Content: "status"},
		MapToPostThreadReplyInput(StatusReplyRequest{TeamID: unitTeamID, ChannelID: unitChannelID, RootMessageID: "1616989510408", Content: "status"}))
	require.Equal(t, teams.MessageImportanceUrgent, MapToPostChatMessageInput(EscalationRequest{ChatID: "19:a@thread.v2", Content: "x"}).Importance)
}

func TestFindAcknowledgementCountsOnlyAnotherPersonsWholePhrase(t *testing.T) {
	record := IncidentAcknowledgement{PosterUserID: posterUserID, StatusReplyID: "2", Update: IncidentUpdate{AcknowledgementPhrase: "ack"}}
	base := time.Date(2026, time.October, 1, 9, 30, 0, 0, time.UTC)
	replies := []teams.Message{
		{ID: "9", MessageType: "message", CreatedAt: base.Add(9 * time.Minute), Sender: teams.MessageSender{UserID: humanUserID, DisplayName: "Ada"}, Text: "ACK, on it"},
		{ID: "2", MessageType: "message", CreatedAt: base, Sender: teams.MessageSender{UserID: posterUserID}, Text: "Reply ack in this thread"},
		{ID: "3", MessageType: "message", CreatedAt: base.Add(time.Minute), Sender: teams.MessageSender{UserID: humanUserID}, Text: "I'll be back soon"},
		{ID: "4", MessageType: "message", CreatedAt: base.Add(2 * time.Minute), Sender: teams.MessageSender{UserID: humanUserID}, Text: "acknowledged"},
		{ID: "5", MessageType: "message", CreatedAt: base.Add(3 * time.Minute), Sender: teams.MessageSender{ApplicationID: "bot"}, Text: "ack"},
		{ID: "6", MessageType: "message", CreatedAt: base.Add(4 * time.Minute), Sender: teams.MessageSender{UserID: humanUserID}, IsDeleted: true},
		{ID: "7", MessageType: "systemEventMessage", CreatedAt: base.Add(5 * time.Minute), Sender: teams.MessageSender{UserID: humanUserID}, Text: "ack"},
		{ID: "8", MessageType: "message", CreatedAt: base.Add(6 * time.Minute), Sender: teams.MessageSender{UserID: posterUserID}, Text: "ack"},
		{ID: "10", MessageType: "message", CreatedAt: base.Add(7 * time.Minute), Sender: teams.MessageSender{UserID: humanUserID, DisplayName: "Grace"}, Text: "Ack 👍"},
	}
	acknowledgement, isFound := FindAcknowledgement(replies, record)
	require.True(t, isFound)
	require.Equal(t, Acknowledgement{MessageID: "10", UserID: humanUserID, DisplayName: "Grace", RepliedAt: base.Add(7 * time.Minute)}, acknowledgement,
		"the earliest qualifying reply wins, whatever order Graph returns")
	_, isFound = FindAcknowledgement(replies[1:8], record)
	require.False(t, isFound)
}

func TestContainsWholePhrase(t *testing.T) {
	require.True(t, ContainsWholePhrase("ack", "ack"))
	require.True(t, ContainsWholePhrase("Ok, ACK.", "ack"))
	require.True(t, ContainsWholePhrase("hack then ack", "ack"))
	require.True(t, ContainsWholePhrase("I'm on it now", "on it"))
	require.True(t, ContainsWholePhrase("👍", "👍"))
	require.False(t, ContainsWholePhrase("back", "ack"))
	require.False(t, ContainsWholePhrase("acks", "ack"))
	require.False(t, ContainsWholePhrase("anything", ""))
}

func TestFlowRegistersEveryStepAndRPC(t *testing.T) {
	flow := NewFlow(newUnitConnection(t), ChannelSelection{TeamID: unitTeamID, ChannelID: unitChannelID}, EscalationChatSelection{ChatID: "19:a@thread.v2"}, policyForUnitTests())
	_, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	require.Len(t, flow.GetRPCs(), 3)
	require.Panics(t, func() {
		NewFlow(newUnitConnection(t), ChannelSelection{}, EscalationChatSelection{}, &AcknowledgementPolicy{CheckInterval: time.Millisecond, MaximumChecks: 1})
	})
	require.Equal(t, AcknowledgementPolicy{CheckInterval: 30 * time.Second, MaximumChecks: 20}, DefaultAcknowledgementPolicy())
}

func policyForUnitTests() *AcknowledgementPolicy {
	return &AcknowledgementPolicy{CheckInterval: time.Second, MaximumChecks: 2}
}

func newUnitConnection(t *testing.T) teams.Connection {
	t.Helper()
	client, err := teams.New(teams.Config{}, sdkgo.StaticCredentialProvider[teams.Credentials]{})
	require.NoError(t, err)
	connection, err := teams.NewConnection(client, sdkgo.ConnectionRef{Provider: "microsoft", Name: ConnectionName})
	require.NoError(t, err)
	return connection
}
