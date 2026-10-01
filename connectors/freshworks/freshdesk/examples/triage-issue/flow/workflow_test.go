// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package triageissue

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/freshworks/freshdesk"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestStartFlowIdentitiesMatchTheFlowDefinition(t *testing.T) {
	flow := NewFlow(newUnitTestConnection(t, ConnectionName))
	require.Equal(t, FlowType, dex.GetFinalFlowType(flow))
	require.Equal(t, recordCustomerIssueStepType, dex.GetFinalStepType[Input](recordCustomerIssue{}))
	require.Equal(t, chooseIssueTicketStepType, dex.GetFinalStepType[freshdesk.SearchTicketsResult](chooseIssueTicket{}))
	require.Equal(t, confirmCandidateTicketStepType, dex.GetFinalStepType[freshdesk.GetTicketResult](confirmCandidateTicket{}))
	require.Equal(t, recordPrioritizedTicketStepType, dex.GetFinalStepType[freshdesk.UpdateTicketResult](recordPrioritizedTicket{}))
	require.Equal(t, recordOpenedTicketStepType, dex.GetFinalStepType[freshdesk.CreateTicketResult](recordOpenedTicket{}))
	require.Equal(t, recordUncertainTicketStepType, dex.GetFinalStepType[freshdesk.CreateTicketResult](recordUncertainTicket{}))
	require.Equal(t, completeTriageStepType, dex.GetFinalStepType[freshdesk.AddNoteResult](completeTriage{}))
	require.Equal(t, recordUncertainNoteStepType, dex.GetFinalStepType[freshdesk.AddNoteResult](recordUncertainNote{}))
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
		Message: "I was charged twice.", IssueTag: "billing-double-charge", Priority: freshdesk.TicketPriorityHigh, GroupID: 156,
	})
	require.NoError(t, err)
	require.Equal(t, CustomerIssue{
		RequesterEmail: "jane@acme.example.com", RequesterName: "Jane Smith", Subject: "Double charge",
		Message: "I was charged twice.", IssueTag: "billing-double-charge", Priority: freshdesk.TicketPriorityHigh, GroupID: 156,
	}, issue)
	valid := Input{RequesterEmail: "jane@acme.example.com", Subject: "s", Message: "m", IssueTag: "billing", Priority: 2}
	for name, change := range map[string]func(*Input){
		"display address": func(input *Input) { input.RequesterEmail = "Jane <jane@acme.example.com>" },
		"blank message":   func(input *Input) { input.Message = " " },
		"uppercase tag":   func(input *Input) { input.IssueTag = "Billing" },
		"no priority":     func(input *Input) { input.Priority = 0 },
		"priority 5":      func(input *Input) { input.Priority = 5 },
		"negative group":  func(input *Input) { input.GroupID = -1 },
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
		Message: "I was charged twice.", IssueTag: "billing-double-charge", Priority: freshdesk.TicketPriorityHigh, GroupID: 156,
	}
	require.Equal(t, freshdesk.SearchTicketsInput{
		Statuses: []freshdesk.TicketStatus{2, 3, 6, 7}, Tags: []string{"billing-double-charge"}, RequesterEmail: "jane@acme.example.com", Page: 2,
	}, MapToSearchTicketsInput(IssueTicketSearch{RequesterEmail: "jane@acme.example.com", IssueTag: "billing-double-charge", Page: 2}))
	require.Equal(t, freshdesk.GetTicketInput{TicketID: 5512, ConversationLimit: candidateConversationLimit}, MapToGetTicketInput(IssueTicketReference{TicketID: 5512}))
	require.Equal(t, freshdesk.UpdateTicketInput{
		TicketID: 5512, Status: freshdesk.TicketStatusOpen, Priority: freshdesk.TicketPriorityHigh, AddTags: []string{RepeatContactTag},
	}, MapToUpdateTicketInput(TicketPriorityChange{TicketID: 5512, Priority: freshdesk.TicketPriorityHigh}))
	require.Equal(t, freshdesk.CreateTicketInput{
		Subject: "Double charge", Description: "I was charged twice.",
		Requester: freshdesk.TicketRequesterInput{Email: "jane@acme.example.com", Name: "Jane Smith"},
		Status:    freshdesk.TicketStatusOpen, Priority: freshdesk.TicketPriorityHigh, Tags: []string{"billing-double-charge"}, GroupID: 156,
	}, MapToCreateTicketInput(issue))
	require.Equal(t, freshdesk.AddNoteInput{TicketID: 5512, Body: "note"}, MapToAddNoteInput(TriageNote{TicketID: 5512, Body: "note"}))
	require.Equal(t, "Dex triage: the customer contacted us again about billing-double-charge. Priority set to High (3).\n\nCustomer message:\nI was charged twice.",
		BuildFollowUpNote(issue))
	require.Equal(t, "Dex triage: opened for billing-double-charge with priority High (3); no unresolved ticket from this customer carried the tag.",
		BuildOpenedTicketNote(issue))
}

func TestChooseNewestTicketPrefersTheLatestUpdate(t *testing.T) {
	_, isFound := ChooseNewestTicket(nil)
	require.False(t, isFound)
	chosen, isFound := ChooseNewestTicket([]freshdesk.Ticket{
		{ID: 1, UpdatedAt: time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC)},
		{ID: 2, UpdatedAt: time.Date(2026, 1, 28, 0, 0, 0, 0, time.UTC)},
		{ID: 3, UpdatedAt: time.Date(2026, 1, 25, 0, 0, 0, 0, time.UTC)},
	})
	require.True(t, isFound)
	require.Equal(t, int64(2), chosen.ID)
}

func TestIsFollowUpCandidateRequiresTheCustomersOwnUnresolvedTaggedTicket(t *testing.T) {
	issue := CustomerIssue{RequesterEmail: "jane@acme.example.com", IssueTag: "billing-double-charge"}
	candidate := freshdesk.TicketDetails{
		Ticket:    freshdesk.Ticket{ID: 5512, Status: freshdesk.TicketStatusWaitingOnCustomer, Tags: []string{"vip", "Billing-Double-Charge"}},
		Requester: &freshdesk.TicketRequester{ID: 1, Email: "Jane@Acme.example.com"},
	}
	require.True(t, IsFollowUpCandidate(candidate, issue))

	otherRequester := candidate
	otherRequester.Requester = &freshdesk.TicketRequester{ID: 2, Email: "jane@acme.example.com.au"}
	require.False(t, IsFollowUpCandidate(otherRequester, issue))
	for _, status := range []freshdesk.TicketStatus{freshdesk.TicketStatusResolved, freshdesk.TicketStatusClosed, 8} {
		resolved := candidate
		resolved.Ticket.Status = status
		require.False(t, IsFollowUpCandidate(resolved, issue), "status %d", status)
	}
	untagged := candidate
	untagged.Ticket.Tags = []string{"vip"}
	require.False(t, IsFollowUpCandidate(untagged, issue))
	missingRequester := candidate
	missingRequester.Requester = nil
	require.False(t, IsFollowUpCandidate(missingRequester, issue))
}

func newUnitTestConnection(t *testing.T, connectionName string) freshdesk.Connection {
	t.Helper()
	client, err := freshdesk.New(freshdesk.Config{Domain: "acme"}, sdkgo.StaticCredentialProvider[freshdesk.Credentials]{})
	require.NoError(t, err)
	connection, err := freshdesk.NewConnection(client, sdkgo.ConnectionRef{Provider: "freshdesk", Name: connectionName})
	require.NoError(t, err)
	return connection
}
