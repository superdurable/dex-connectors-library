//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/helpscout"
	conversationtriage "github.com/superdurable/dex-connectors-library/connectors/helpscout/examples/conversation-triage/flow"
	"github.com/superdurable/dex-connectors-library/connectors/helpscout/helpscoutmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestConversationTriageUsesHelpScoutMocksWithRealDex(t *testing.T) {
	mock := helpscoutmock.New(t, conversationtriage.ConnectionName)
	flow := conversationtriage.NewFlow(mock.Connection())
	client := startWorkerAndClient(t, flow)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := fmt.Sprintf("helpscout-mock-%d", time.Now().UnixNano())
	input := conversationtriage.TriageRequest{EventID: "convo.created:501:mock", ConversationID: 501, MailboxID: 123,
		Subject: "Double charge on order 88213", ReceivedAt: time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)}
	_, err := client.StartFlow(ctx, flow, flowID, input, dex.StartFlowOptions{RequestID: &flowID})
	require.NoError(t, err)
	completed, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, completed.Status, completed.ErrorMessage)
	var triage conversationtriage.Triage
	require.NoError(t, completed.DecodeSingleOutput(&triage))
	require.Equal(t, "completed", triage.Stage)
	require.Equal(t, []string{"customer"}, triage.NewestThreadTypes)
	require.Equal(t, []int64{238604}, triage.CustomerProfileIDs)
	require.Equal(t, []int64{502}, triage.OtherActiveConversationIDs)

	reads := mock.GetConversation().Calls()
	require.Len(t, reads, 1)
	require.Equal(t, helpscout.GetConversationInput{ConversationID: 501, ThreadLimit: 5}, reads[0].Input)
	customers := mock.FindCustomerByEmail().Calls()
	require.Len(t, customers, 1)
	require.Equal(t, helpscout.FindCustomerByEmailInput{Email: "jane@acme.example.com"}, customers[0].Input)
	searches := mock.SearchConversations().Calls()
	require.Len(t, searches, 1)
	require.Equal(t, helpscout.SearchConversationsInput{MailboxID: 123, Status: helpscout.ConversationStatusActive,
		CustomerEmail: "jane@acme.example.com"}, searches[0].Input)
	notes := mock.ReplyToConversation().Calls()
	require.Len(t, notes, 1)
	require.Equal(t, helpscout.ReplyToConversationInput{ConversationID: 501, IsInternalNote: true,
		Text: conversationtriage.BuildTriageNote("jane@acme.example.com", []int64{238604}, []int64{502})}, notes[0].Input)
	updates := mock.UpdateConversation().Calls()
	require.Len(t, updates, 1)
	require.Equal(t, helpscout.UpdateConversationInput{ConversationID: 501,
		AddTags: []string{conversationtriage.TriagedTag, conversationtriage.RepeatContactTag}}, updates[0].Input)
}
