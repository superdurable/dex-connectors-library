//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package claimconversation

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/hiver"
	"github.com/superdurable/dex-connectors-library/connectors/hiver/hivermock"
)

func TestClaimConversationRunsOnHiverMockWithRealDex(t *testing.T) {
	mock := hivermock.New(t, ConnectionName)
	harness := newClaimHarnessWithConnection(t, mock.Connection())
	outcome := harness.runClaim(t, "mock", integrationClaimInput())
	require.Equal(t, ClaimActionClaimed, outcome.Action)
	require.Equal(t, 3, outcome.InboxPagesRead)
	require.Equal(t, 3, outcome.ConversationPagesRead)

	inboxCalls := mock.ListInboxes().Calls()
	require.Len(t, inboxCalls, 3)
	for index, token := range []string{"", "inboxes-2", "inboxes-3"} {
		require.Equal(t, hiver.ListInboxesInput{PageToken: token, PageSize: 100}, inboxCalls[index].Input)
		require.Equal(t, hiver.ListInboxesBranchListed, inboxCalls[index].Branch)
	}
	conversationCalls := mock.ListConversations().Calls()
	require.Len(t, conversationCalls, 3)
	for index, token := range []string{"", "conversations-2", "conversations-3"} {
		require.Equal(t, hiver.ListConversationsInput{InboxID: "105902", PageToken: token, PageSize: 50}, conversationCalls[index].Input)
		require.Equal(t, hiver.ListConversationsBranchListed, conversationCalls[index].Branch)
	}
	readCalls := mock.GetConversation().Calls()
	require.Len(t, readCalls, 1)
	require.Equal(t, hiver.GetConversationInput{InboxID: "105902", ConversationID: "573741352"}, readCalls[0].Input)
	updateCalls := mock.UpdateConversation().Calls()
	require.Len(t, updateCalls, 1)
	require.Equal(t, hiver.UpdateConversationInput{InboxID: "105902", ConversationID: "573741352", AssigneeEmail: "phoebe@acme.example.com", ApplyTagNames: []string{"Claimed"}}, updateCalls[0].Input)
	require.Equal(t, hiver.UpdateConversationBranchUpdated, updateCalls[0].Branch)
	noteCalls := mock.AddNote().Calls()
	require.Len(t, noteCalls, 1)
	require.Equal(t, hiver.AddNoteInput{InboxID: "105902", ConversationID: "573741352", Content: "Dex claim: assigned to phoebe@acme.example.com.\n\nTaking this one; refund check first."}, noteCalls[0].Input)
	require.Equal(t, hiver.AddNoteBranchAdded, noteCalls[0].Branch)
	draftCalls := mock.CreateSharedDraft().Calls()
	require.Len(t, draftCalls, 1)
	require.Equal(t, hiver.CreateSharedDraftInput{InboxID: "105902", HiverMessageID: "834466048", Body: "Hi Jane, we are looking into the double charge now."}, draftCalls[0].Input)
	require.Equal(t, hiver.CreateSharedDraftBranchCreated, draftCalls[0].Branch)
}
