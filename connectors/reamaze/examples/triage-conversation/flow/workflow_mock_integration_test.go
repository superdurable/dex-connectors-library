//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package triageconversation

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/reamaze"
	"github.com/superdurable/dex-connectors-library/connectors/reamaze/reamazemock"
)

func TestNewIssueUsesMockDefaultsWithRealDex(t *testing.T) {
	mock := reamazemock.New(t, ConnectionName)
	harness := newTriageHarnessForConnection(t, mock.Connection())
	outcome := harness.runTriage(t, "mock-new-issue", integrationIssueInput())
	require.Equal(t, TriageConversationOpened, outcome.Action)
	require.True(t, outcome.IsKnownContact)
	require.Equal(t, "double-charge", outcome.Conversation.ID)
	require.Equal(t, "dex-note-88213", outcome.NoteOriginID)

	contactCalls := mock.FindContactByEmail().Calls()
	require.Len(t, contactCalls, 1)
	require.Equal(t, reamaze.FindContactByEmailInput{Email: "jane@acme.example.com"}, contactCalls[0].Input)
	searchCalls := mock.SearchConversations().Calls()
	require.Len(t, searchCalls, 1)
	require.Equal(t, reamaze.SearchConversationsInput{
		RequesterEmail: "jane@acme.example.com", Tags: []string{"billing-double-charge"},
		Sort: reamaze.ConversationSortChanged, Page: 1,
	}, searchCalls[0].Input)
	createCalls := mock.CreateConversation().Calls()
	require.Len(t, createCalls, 1)
	require.Equal(t, reamaze.CreateConversationInput{
		Subject: "Double charge on order 88213", Message: "I was charged twice for order 88213.", Channel: "support",
		Requester: reamaze.ConversationRequesterInput{Email: "jane@acme.example.com", Name: "Jane Smith"},
		Tags:      []string{"billing-double-charge"},
	}, createCalls[0].Input)
	noteCalls := mock.ReplyToConversation().Calls()
	require.Len(t, noteCalls, 1)
	require.Equal(t, reamaze.ReplyToConversationInput{
		ConversationID: "double-charge", Text: "Dex triage: opened for billing-double-charge; no unresolved conversation from this customer carried the tag.",
		IsInternalNote: true, ShouldSuppressAutoResolve: true,
	}, noteCalls[0].Input)
	require.Empty(t, mock.GetConversation().Calls())
	require.Empty(t, mock.UpdateConversation().Calls())
}
