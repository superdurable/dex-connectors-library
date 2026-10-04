// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package triageissue

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/gorgias"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestStartFlowIdentitiesMatchTheFlowDefinition(t *testing.T) {
	flow := NewFlow(newUnitTestConnection(t, ConnectionName))
	require.Equal(t, FlowType, dex.GetFinalFlowType(flow))
	require.Equal(t, recordCustomerIssueStepType, dex.GetFinalStepType[Input](recordCustomerIssue{}))
	require.Equal(t, chooseRequesterRouteStepType, dex.GetFinalStepType[gorgias.FindCustomerByEmailResult](chooseRequesterRoute{}))
	require.Equal(t, chooseIssueTicketStepType, dex.GetFinalStepType[gorgias.SearchTicketsResult](chooseIssueTicket{}))
	require.Equal(t, confirmCandidateTicketStepType, dex.GetFinalStepType[gorgias.GetTicketResult](confirmCandidateTicket{}))
	require.Equal(t, recordPrioritizedTicketStepType, dex.GetFinalStepType[gorgias.UpdateTicketResult](recordPrioritizedTicket{}))
	require.Equal(t, recordOpenedTicketStepType, dex.GetFinalStepType[gorgias.CreateTicketResult](recordOpenedTicket{}))
	require.Equal(t, recordUncertainTicketStepType, dex.GetFinalStepType[gorgias.CreateTicketResult](recordUncertainTicket{}))
	require.Equal(t, recordTriageNoteStepType, dex.GetFinalStepType[gorgias.AddNoteResult](recordTriageNote{}))
	require.Equal(t, recordUncertainNoteStepType, dex.GetFinalStepType[gorgias.AddNoteResult](recordUncertainNote{}))
	require.Equal(t, completeTriageStepType, dex.GetFinalStepType[gorgias.AddNoteResult](completeTriage{}))
	require.Equal(t, recordUncertainAcknowledgmentStepType, dex.GetFinalStepType[gorgias.AddNoteResult](recordUncertainAcknowledgment{}))
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
		Message: "I was charged twice.", IssueTag: "billing-double-charge", Priority: gorgias.TicketPriorityHigh, Acknowledgement: " Thanks ",
	})
	require.NoError(t, err)
	require.Equal(t, CustomerIssue{
		RequesterEmail: "jane@acme.example.com", RequesterName: "Jane Smith", Subject: "Double charge",
		Message: "I was charged twice.", IssueTag: "billing-double-charge", Priority: gorgias.TicketPriorityHigh, Acknowledgement: "Thanks",
	}, issue)
	valid := Input{RequesterEmail: "jane@acme.example.com", Subject: "s", Message: "m", IssueTag: "billing", Priority: gorgias.TicketPriorityNormal}
	for name, change := range map[string]func(*Input){
		"display address":   func(input *Input) { input.RequesterEmail = "Jane <jane@acme.example.com>" },
		"blank message":     func(input *Input) { input.Message = " " },
		"uppercase tag":     func(input *Input) { input.IssueTag = "Billing" },
		"no priority":       func(input *Input) { input.Priority = "" },
		"zendesk priority":  func(input *Input) { input.Priority = "urgent" },
		"freshdesk integer": func(input *Input) { input.Priority = "3" },
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
		Message: "I was charged twice.", IssueTag: "billing-double-charge", Priority: gorgias.TicketPriorityHigh,
	}
	require.Equal(t, gorgias.FindCustomerByEmailInput{Email: "jane@acme.example.com"}, MapToFindCustomerByEmailInput(issue))
	require.Equal(t, gorgias.SearchTicketsInput{
		RequesterID: 3924, Statuses: []gorgias.TicketStatus{gorgias.TicketStatusOpen}, Tags: []string{"billing-double-charge"},
		PageSize: gorgias.MaxSearchPageSize, Cursor: "WyJuZXh0Il0=",
	}, MapToSearchTicketsInput(IssueTicketSearch{RequesterID: 3924, IssueTag: "billing-double-charge", Cursor: "WyJuZXh0Il0=", PageNumber: 2}))
	require.Equal(t, gorgias.GetTicketInput{TicketID: 5512, LatestMessageLimit: candidateMessageLimit}, MapToGetTicketInput(IssueTicketReference{TicketID: 5512}))
	require.Equal(t, gorgias.UpdateTicketInput{
		TicketID: 5512, Status: gorgias.TicketStatusOpen, Priority: gorgias.TicketPriorityHigh, AddTags: []string{RepeatContactTag},
	}, MapToUpdateTicketInput(TicketPriorityChange{TicketID: 5512, Priority: gorgias.TicketPriorityHigh}))
	require.Equal(t, gorgias.CreateTicketInput{
		Subject: "Double charge", Description: "I was charged twice.",
		Requester: gorgias.TicketRequesterInput{Email: "jane@acme.example.com", Name: "Jane Smith"},
		Status:    gorgias.TicketStatusOpen, Priority: gorgias.TicketPriorityHigh, Tags: []string{"billing-double-charge"},
	}, MapToCreateTicketInput(issue))
	require.Equal(t, gorgias.AddNoteInput{TicketID: 5512, Body: "note"}, MapToInternalNoteInput(TicketNote{TicketID: 5512, Body: "note"}))
	require.Equal(t, gorgias.AddNoteInput{TicketID: 5512, Body: "Thanks", IsPublicReply: true}, MapToPublicReplyInput(TicketNote{TicketID: 5512, Body: "Thanks"}))
	require.Equal(t, "Dex triage: the customer contacted us again about billing-double-charge. Priority set to high.\n\nCustomer message:\nI was charged twice.",
		BuildFollowUpNote(issue))
	require.Equal(t, "Dex triage: opened for billing-double-charge with priority high; the customer had no open ticket carrying the tag.",
		BuildOpenedTicketNote(issue))
}

func TestChooseNewestTicketPrefersTheLatestUpdate(t *testing.T) {
	_, isFound := ChooseNewestTicket(nil)
	require.False(t, isFound)
	chosen, isFound := ChooseNewestTicket([]gorgias.Ticket{
		{ID: 1, UpdatedAt: time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC)},
		{ID: 2, UpdatedAt: time.Date(2026, 1, 28, 0, 0, 0, 0, time.UTC)},
		{ID: 3, UpdatedAt: time.Date(2026, 1, 25, 0, 0, 0, 0, time.UTC)},
	})
	require.True(t, isFound)
	require.Equal(t, int64(2), chosen.ID)
}

func TestIsFollowUpCandidateRequiresTheCustomersOwnOpenTaggedTicket(t *testing.T) {
	issue := CustomerIssue{RequesterEmail: "jane@acme.example.com", IssueTag: "billing-double-charge"}
	candidate := gorgias.TicketDetails{
		Ticket:    gorgias.Ticket{ID: 5512, Status: gorgias.TicketStatusOpen, Tags: []string{"vip", "billing-double-charge"}},
		Requester: &gorgias.Customer{ID: 1, Email: "Jane@Acme.example.com"},
	}
	require.True(t, IsFollowUpCandidate(candidate, issue))

	otherRequester := candidate
	otherRequester.Requester = &gorgias.Customer{ID: 2, Email: "jane@acme.example.com.au"}
	require.False(t, IsFollowUpCandidate(otherRequester, issue))
	closed := candidate
	closed.Ticket.Status = gorgias.TicketStatusClosed
	require.False(t, IsFollowUpCandidate(closed, issue))
	differentCase := candidate
	differentCase.Ticket.Tags = []string{"Billing-Double-Charge"}
	require.False(t, IsFollowUpCandidate(differentCase, issue), "Gorgias tag names are case sensitive")
	missingRequester := candidate
	missingRequester.Requester = nil
	require.False(t, IsFollowUpCandidate(missingRequester, issue))
}

func newUnitTestConnection(t *testing.T, connectionName string) gorgias.Connection {
	t.Helper()
	client, err := gorgias.New(gorgias.Config{Domain: "acme"}, sdkgo.StaticCredentialProvider[gorgias.Credentials]{})
	require.NoError(t, err)
	connection, err := gorgias.NewConnection(client, sdkgo.ConnectionRef{Provider: "gorgias", Name: connectionName})
	require.NoError(t, err)
	return connection
}
