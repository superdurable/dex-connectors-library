// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package triageconversation

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/reamaze"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestStartFlowIdentitiesMatchTheFlowDefinition(t *testing.T) {
	flow := NewFlow(newUnitTestConnection(t, ConnectionName))
	require.Equal(t, FlowType, dex.GetFinalFlowType(flow))
	require.Equal(t, recordCustomerIssueStepType, dex.GetFinalStepType[Input](recordCustomerIssue{}))
	require.Equal(t, routeCustomerContactStepType, dex.GetFinalStepType[reamaze.FindContactByEmailResult](routeCustomerContact{}))
	require.Equal(t, chooseIssueConversationStepType, dex.GetFinalStepType[reamaze.SearchConversationsResult](chooseIssueConversation{}))
	require.Equal(t, confirmCandidateConversationStepType, dex.GetFinalStepType[reamaze.GetConversationResult](confirmCandidateConversation{}))
	require.Equal(t, recordReopenedConversationStepType, dex.GetFinalStepType[reamaze.UpdateConversationResult](recordReopenedConversation{}))
	require.Equal(t, recordOpenedConversationStepType, dex.GetFinalStepType[reamaze.CreateConversationResult](recordOpenedConversation{}))
	require.Equal(t, recordUncertainConversationStepType, dex.GetFinalStepType[reamaze.CreateConversationResult](recordUncertainConversation{}))
	require.Equal(t, completeTriageStepType, dex.GetFinalStepType[reamaze.ReplyToConversationResult](completeTriage{}))
	require.Equal(t, recordUncertainNoteStepType, dex.GetFinalStepType[reamaze.ReplyToConversationResult](recordUncertainNote{}))
	wait, err := recordCustomerIssue{}.WaitFor(nil, Input{})
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

func TestBuildCustomerIssueValidatesStartInput(t *testing.T) {
	issue, err := BuildCustomerIssue(Input{
		RequesterEmail: " jane@acme.example.com ", RequesterName: "Jane Smith", Subject: " Double charge ",
		Message: "I was charged twice.", IssueTag: "billing-double-charge", Channel: " support ",
	})
	require.NoError(t, err)
	require.Equal(t, CustomerIssue{
		RequesterEmail: "jane@acme.example.com", RequesterName: "Jane Smith", Subject: "Double charge",
		Message: "I was charged twice.", IssueTag: "billing-double-charge", Channel: "support",
	}, issue)
	valid := Input{RequesterEmail: "jane@acme.example.com", Subject: "s", Message: "m", IssueTag: "billing", Channel: "support"}
	apostrophe := valid
	apostrophe.RequesterEmail = "sean.o'brien@example.ie"
	_, err = BuildCustomerIssue(apostrophe)
	require.NoError(t, err)
	for name, change := range map[string]func(*Input){
		"display address": func(input *Input) { input.RequesterEmail = "Jane <jane@acme.example.com>" },
		"blank message":   func(input *Input) { input.Message = " " },
		"uppercase tag":   func(input *Input) { input.IssueTag = "Billing" },
		"comma tag":       func(input *Input) { input.IssueTag = "billing,vip" },
		"missing channel": func(input *Input) { input.Channel = "" },
		"channel path":    func(input *Input) { input.Channel = "support/x" },
	} {
		input := valid
		change(&input)
		_, err := BuildCustomerIssue(input)
		require.Error(t, err, name)
	}
}

func TestMappersPassOnlyTheRecordedIssue(t *testing.T) {
	issue := CustomerIssue{
		RequesterEmail: "jane@acme.example.com", RequesterName: "Jane Smith", Subject: "Double charge",
		Message: "I was charged twice.", IssueTag: "billing-double-charge", Channel: "support",
	}
	require.Equal(t, reamaze.FindContactByEmailInput{Email: "jane@acme.example.com"}, MapToFindContactByEmailInput(CustomerEmail{Email: "jane@acme.example.com"}))
	require.Equal(t, reamaze.SearchConversationsInput{
		RequesterEmail: "jane@acme.example.com", Tags: []string{"billing-double-charge"}, Sort: reamaze.ConversationSortChanged, Page: 2,
	}, MapToSearchConversationsInput(IssueConversationSearch{RequesterEmail: "jane@acme.example.com", IssueTag: "billing-double-charge", Page: 2}))
	require.Equal(t, reamaze.GetConversationInput{ConversationID: "double-charge", MessageLimit: candidateMessageLimit},
		MapToGetConversationInput(IssueConversationReference{ConversationID: "double-charge"}))
	update := MapToUpdateConversationInput(IssueConversationReference{ConversationID: "double-charge"})
	require.Equal(t, reamaze.ConversationStatusOpen, *update.Status)
	require.Equal(t, []string{RepeatContactTag}, update.AddTags)
	require.Equal(t, reamaze.CreateConversationInput{
		Subject: "Double charge", Message: "I was charged twice.", Channel: "support",
		Requester: reamaze.ConversationRequesterInput{Email: "jane@acme.example.com", Name: "Jane Smith"}, Tags: []string{"billing-double-charge"},
	}, MapToCreateConversationInput(issue))
	require.Equal(t, reamaze.ReplyToConversationInput{ConversationID: "double-charge", Text: "note", IsInternalNote: true, ShouldSuppressAutoResolve: true},
		MapToReplyToConversationInput(TriageNote{ConversationID: "double-charge", Text: "note"}))
	require.Equal(t, "Dex triage: the customer contacted us again about billing-double-charge. Status set to Open (0).\n\nCustomer message:\nI was charged twice.",
		BuildFollowUpNote(issue))
	require.Equal(t, "Dex triage: opened for billing-double-charge; no unresolved conversation from this customer carried the tag.",
		BuildOpenedConversationNote(issue))
	require.Equal(t, "Spam (identified by AI) (9)", DescribeConversationStatus(reamaze.ConversationStatusAISpam))
	require.Equal(t, "Unknown (12)", DescribeConversationStatus(12))
}

func TestFollowUpCandidatesAreTheCustomersOwnUnresolvedTaggedConversations(t *testing.T) {
	issue := CustomerIssue{RequesterEmail: "jane@acme.example.com", IssueTag: "billing-double-charge"}
	candidate := reamaze.Conversation{
		ID: "double-charge", Status: reamaze.ConversationStatusResponded, Tags: []string{"vip", "Billing-Double-Charge"},
		Requester: &reamaze.ConversationParticipant{Email: "Jane@Acme.example.com"},
	}
	require.True(t, IsFollowUpCandidate(reamaze.ConversationDetails{Conversation: candidate}, issue))

	copied := candidate
	copied.Requester = &reamaze.ConversationParticipant{Email: "ben@meridian.example.com"}
	lookalike := candidate
	lookalike.Requester = &reamaze.ConversationParticipant{Email: "jane@acme.example.com.au"}
	untagged := candidate
	untagged.Tags = []string{"vip"}
	missingRequester := candidate
	missingRequester.Requester = nil
	for _, conversation := range []reamaze.Conversation{copied, lookalike, untagged, missingRequester} {
		require.False(t, IsFollowUpCandidate(reamaze.ConversationDetails{Conversation: conversation}, issue))
	}
	for _, status := range []reamaze.ConversationStatus{2, 3, 4, 6, 8, 9} {
		resolved := candidate
		resolved.Status = status
		require.False(t, IsFollowUpCandidate(reamaze.ConversationDetails{Conversation: resolved}, issue), "status %d", status)
	}
	done := candidate
	done.ID, done.Status = "done", reamaze.ConversationStatusDone
	chosen, isFound := ChooseFollowUpCandidate([]reamaze.Conversation{done, copied, candidate}, issue)
	require.True(t, isFound)
	require.Equal(t, "double-charge", chosen.ID)
	_, isFound = ChooseFollowUpCandidate([]reamaze.Conversation{done, copied}, issue)
	require.False(t, isFound)
}

func newUnitTestConnection(t *testing.T, connectionName string) reamaze.Connection {
	t.Helper()
	client, err := reamaze.New(reamaze.Config{Brand: "acme"}, sdkgo.StaticCredentialProvider[reamaze.Credentials]{})
	require.NoError(t, err)
	connection, err := reamaze.NewConnection(client, sdkgo.ConnectionRef{Provider: "reamaze", Name: connectionName})
	require.NoError(t, err)
	return connection
}
