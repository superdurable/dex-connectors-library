//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/front"
	conversationtriage "github.com/superdurable/dex-connectors-library/connectors/front/examples/conversation-triage/flow"
	"github.com/superdurable/dex-connectors-library/connectors/front/frontmock"
)

func TestConversationTriageUsesFrontMocksWithRealDex(t *testing.T) {
	mock := frontmock.New(t, conversationtriage.ConnectionName)
	harness := newTriageHarnessForConnection(t, mock.Connection(), conversationtriage.RoutingConfiguration{
		AssigneeID: "tea_leela", TagID: "tag_triaged",
	})
	triage := harness.runTriage(t, "mock-default", "cnv_100")
	require.Equal(t, "completed", triage.Stage)
	require.Equal(t, "jane@acme.example.com", triage.RequesterEmail)
	require.Equal(t, "crd_100", triage.ContactID)
	require.Equal(t, []string{"cnv_200"}, triage.OtherOpenConversationIDs)
	require.Equal(t, "com_100", triage.CommentID)
	require.Equal(t, "tea_leela", triage.AssigneeID)
	require.Equal(t, []string{"tag_triaged"}, triage.TagIDs)

	getCalls := mock.GetConversation().Calls()
	require.Len(t, getCalls, 1)
	require.Equal(t, front.GetConversationInput{ConversationID: "cnv_100", MessageLimit: conversationtriage.NewestMessageLimit, CommentLimit: front.MaxCommentLimit}, getCalls[0].Input)
	contactCalls := mock.FindContactByEmail().Calls()
	require.Len(t, contactCalls, 1)
	require.Equal(t, front.FindContactByEmailInput{Email: "jane@acme.example.com"}, contactCalls[0].Input)
	searchCalls := mock.SearchConversations().Calls()
	require.Len(t, searchCalls, 1)
	require.Equal(t, front.SearchConversationsInput{
		InboxID: "inb_support", Statuses: []front.SearchStatusFilter{front.SearchStatusOpen},
		RecipientEmail: "jane@acme.example.com", PageSize: 11,
	}, searchCalls[0].Input)
	replyCalls := mock.ReplyToConversation().Calls()
	require.Len(t, replyCalls, 1)
	require.Equal(t, front.ReplyToConversationInput{
		ConversationID: "cnv_100", IsInternalNote: true,
		Text: conversationtriage.BuildTriageComment("jane@acme.example.com", "crd_100", []string{"cnv_200"}, false),
	}, replyCalls[0].Input)
	updateCalls := mock.UpdateConversation().Calls()
	require.Len(t, updateCalls, 1)
	require.Equal(t, front.UpdateConversationInput{
		ConversationID: "cnv_100", AssigneeID: "tea_leela", AddTagIDs: []string{"tag_triaged"},
	}, updateCalls[0].Input)
}
