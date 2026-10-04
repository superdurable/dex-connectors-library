// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package claimconversation

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/hiver"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestStartFlowIdentitiesMatchTheFlowDefinition(t *testing.T) {
	flow := NewFlow(newUnitTestConnection(t, ConnectionName))
	require.Equal(t, FlowType, dex.GetFinalFlowType(flow))
	require.Equal(t, recordClaimRequestStepType, dex.GetFinalStepType[Input](recordClaimRequest{}))
	require.Equal(t, chooseSharedInboxStepType, dex.GetFinalStepType[hiver.ListInboxesResult](chooseSharedInbox{}))
	require.Equal(t, chooseConversationStepType, dex.GetFinalStepType[hiver.ListConversationsResult](chooseConversation{}))
	require.Equal(t, confirmConversationStepType, dex.GetFinalStepType[hiver.GetConversationResult](confirmConversation{}))
	require.Equal(t, recordClaimedConversationStepType, dex.GetFinalStepType[hiver.UpdateConversationResult](recordClaimedConversation{}))
	require.Equal(t, recordClaimNoteStepType, dex.GetFinalStepType[hiver.AddNoteResult](recordClaimNote{}))
	require.Equal(t, recordUncertainNoteStepType, dex.GetFinalStepType[hiver.AddNoteResult](recordUncertainNote{}))
	require.Equal(t, completeClaimStepType, dex.GetFinalStepType[hiver.CreateSharedDraftResult](completeClaim{}))
	require.Equal(t, recordUncertainDraftStepType, dex.GetFinalStepType[hiver.CreateSharedDraftResult](recordUncertainDraft{}))
	wait, err := recordClaimRequest{}.WaitFor(nil, Input{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
}

func TestFlowRegistersOnlyWithItsStaticConnection(t *testing.T) {
	_, err := dex.NewRegistry([]dex.Flow{NewFlow(newUnitTestConnection(t, ConnectionName))})
	require.NoError(t, err)
	require.Panics(t, func() {
		_, _ = dex.NewRegistry([]dex.Flow{NewFlow(newUnitTestConnection(t, "another-connection"))})
	})
}

func TestBuildClaimRequestValidatesStartInput(t *testing.T) {
	request, err := BuildClaimRequest(Input{
		InboxEmail: " Support@Acme.example.com ", AssigneeEmail: "phoebe@acme.example.com", ClaimTagName: " Claimed ",
		Note: " Taking this one. ", ReplyDraft: " Hi Jane, ",
	})
	require.NoError(t, err)
	require.Equal(t, ClaimRequest{
		InboxEmail: "support@acme.example.com", AssigneeEmail: "phoebe@acme.example.com", ClaimTagName: "Claimed",
		Note: "Taking this one.", ReplyDraft: "Hi Jane,",
	}, request)
	valid := Input{InboxEmail: "support@acme.example.com", AssigneeEmail: "phoebe@acme.example.com", Note: "n"}
	for name, change := range map[string]func(*Input){
		"display inbox address": func(input *Input) { input.InboxEmail = "Support <support@acme.example.com>" },
		"missing assignee":      func(input *Input) { input.AssigneeEmail = "" },
		"blank note":            func(input *Input) { input.Note = " " },
		"tag with line break":   func(input *Input) { input.ClaimTagName = "a\nb" },
		"oversized draft":       func(input *Input) { input.ReplyDraft = string(make([]byte, maximumDraftBytes+1)) + "x" },
	} {
		input := valid
		change(&input)
		_, err := BuildClaimRequest(input)
		require.Error(t, err, name)
	}
}

func TestMappersPassOnlyTheRecordedClaim(t *testing.T) {
	require.Equal(t, hiver.ListInboxesInput{PageToken: "cGFnZTI=", PageSize: 100}, MapToListInboxesInput(InboxSearch{PageToken: "cGFnZTI="}))
	require.Equal(t, hiver.ListConversationsInput{InboxID: "105902", PageSize: 50}, MapToListConversationsInput(ConversationSearch{InboxID: "105902"}))
	require.Equal(t, hiver.GetConversationInput{InboxID: "105902", ConversationID: "1"},
		MapToGetConversationInput(ConversationReference{InboxID: "105902", ConversationID: "1"}))
	require.Equal(t, hiver.UpdateConversationInput{InboxID: "105902", ConversationID: "1", AssigneeEmail: "p@acme.example.com"},
		MapToUpdateConversationInput(ConversationClaim{InboxID: "105902", ConversationID: "1", AssigneeEmail: "p@acme.example.com"}))
	require.Equal(t, []string{"Claimed"}, MapToUpdateConversationInput(ConversationClaim{ClaimTagName: "Claimed"}).ApplyTagNames)
	require.Equal(t, hiver.AddNoteInput{InboxID: "105902", ConversationID: "1", Content: "n"},
		MapToAddNoteInput(ClaimNote{InboxID: "105902", ConversationID: "1", Content: "n"}))
	require.Equal(t, hiver.CreateSharedDraftInput{InboxID: "105902", HiverMessageID: "9", Body: "b"},
		MapToCreateSharedDraftInput(ReplyDraft{InboxID: "105902", HiverMessageID: "9", Body: "b"}))
	require.Empty(t, replyGmailMessageID(hiver.ConversationMessage{HiverMessageID: "9", GmailMessageID: "19cf"}), "the draft takes exactly one ID")
	require.Equal(t, "19cf", replyGmailMessageID(hiver.ConversationMessage{GmailMessageID: "19cf"}))
	require.Equal(t, "Dex claim: assigned to p@acme.example.com.\n\nTaking this one.",
		BuildClaimNote(ClaimRequest{AssigneeEmail: "p@acme.example.com", Note: "Taking this one."}))
}

func TestFindersPickTheRequestedInboxAndAClaimableConversation(t *testing.T) {
	inbox, isFound := FindInboxByEmail([]hiver.Inbox{{ID: "1", Email: "billing@acme.example.com"}, {ID: "2", Email: "Support@Acme.example.com"}}, "support@acme.example.com")
	require.True(t, isFound)
	require.Equal(t, "2", inbox.ID)
	_, isFound = FindInboxByEmail(nil, "support@acme.example.com")
	require.False(t, isFound)
	conversation, isFound := FindClaimableConversation([]hiver.Conversation{
		{ID: "1", Status: hiver.ConversationStatusClosed},
		{ID: "2", Status: hiver.ConversationStatusOpen, Assignee: &hiver.ConversationAssignee{Type: "user", ID: "7"}},
		{ID: "3", Status: hiver.ConversationStatusPending},
		{ID: "4", Status: hiver.ConversationStatusOpen},
	})
	require.True(t, isFound)
	require.Equal(t, "4", conversation.ID)
}

func newUnitTestConnection(t *testing.T, connectionName string) hiver.Connection {
	t.Helper()
	client, err := hiver.New(hiver.Config{}, sdkgo.StaticCredentialProvider[hiver.Credentials]{})
	require.NoError(t, err)
	connection, err := hiver.NewConnection(client, sdkgo.ConnectionRef{Provider: "hiver", Name: connectionName})
	require.NoError(t, err)
	return connection
}
