// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package customerissue

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zendesk/support"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestStartFlowIdentitiesMatchTheFlowDefinition(t *testing.T) {
	flow := NewFlow(newUnitTestConnection(t, ConnectionName))
	require.Equal(t, FlowType, dex.GetFinalFlowType(flow))
	require.Equal(t, recordCustomerIssueStepType, dex.GetFinalStepType[Input](recordCustomerIssue{}))
	require.Equal(t, chooseIssueTicketStepType, dex.GetFinalStepType[support.SearchTicketsResult](chooseIssueTicket{}))
	require.Equal(t, confirmCandidateTicketStepType, dex.GetFinalStepType[support.GetTicketResult](confirmCandidateTicket{}))
	require.Equal(t, completeFollowUpStepType, dex.GetFinalStepType[support.UpdateTicketResult](completeFollowUp{}))
	require.Equal(t, completeOpenedTicketStepType, dex.GetFinalStepType[support.CreateTicketResult](completeOpenedTicket{}))
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
		Message: "I was charged twice.", IssueTag: "billing-double-charge", GroupID: 98738,
	})
	require.NoError(t, err)
	require.Equal(t, CustomerIssue{
		RequesterEmail: "jane@acme.example.com", RequesterName: "Jane Smith", Subject: "Double charge",
		Message: "I was charged twice.", IssueTag: "billing-double-charge", GroupID: 98738,
	}, issue)
	for name, input := range map[string]Input{
		"display address": {RequesterEmail: "Jane <jane@acme.example.com>", Subject: "s", Message: "m", IssueTag: "billing"},
		"blank message":   {RequesterEmail: "jane@acme.example.com", Subject: "s", IssueTag: "billing"},
		"uppercase tag":   {RequesterEmail: "jane@acme.example.com", Subject: "s", Message: "m", IssueTag: "Billing"},
		"negative group":  {RequesterEmail: "jane@acme.example.com", Subject: "s", Message: "m", IssueTag: "billing", GroupID: -1},
	} {
		_, err := BuildCustomerIssue(input)
		require.Error(t, err, name)
	}
}

func TestMappersPassOnlyTheRecordedIssue(t *testing.T) {
	issue := CustomerIssue{
		RequesterEmail: "jane@acme.example.com", RequesterName: "Jane Smith", Subject: "Double charge",
		Message: "I was charged twice.", IssueTag: "billing-double-charge", GroupID: 98738,
	}
	require.Equal(t, support.SearchTicketsInput{
		Statuses:       []support.TicketStatus{support.TicketStatusNew, support.TicketStatusOpen, support.TicketStatusPending, support.TicketStatusHold},
		RequesterEmail: "jane@acme.example.com", Tags: []string{"billing-double-charge"}, PageSize: unsolvedTicketSearchPageSize,
	}, MapToSearchTicketsInput(issue))
	require.Equal(t, support.GetTicketInput{TicketID: 5512, LatestCommentLimit: candidateCommentLimit}, MapToGetTicketInput(IssueTicketReference{TicketID: 5512}))
	require.Equal(t, support.CreateTicketInput{
		Subject: "Double charge", Comment: support.TicketCommentInput{Body: "I was charged twice."},
		Requester: &support.TicketRequesterInput{Email: "jane@acme.example.com", Name: "Jane Smith"},
		Priority:  support.TicketPriorityNormal, Tags: []string{"billing-double-charge"}, GroupID: 98738,
	}, MapToCreateTicketInput(issue))
	require.Equal(t, support.UpdateTicketInput{
		TicketID: 5512, Status: support.TicketStatusOpen, AddTags: []string{RepeatContactTag},
		Comment: &support.TicketCommentInput{Body: FollowUpNote + "\n\nI was charged twice.", IsInternalNote: true},
	}, MapToUpdateTicketInput(FollowUp{TicketID: 5512, Message: "I was charged twice."}))
}

func TestChooseNewestTicketPrefersTheLatestUpdate(t *testing.T) {
	_, isFound := ChooseNewestTicket(nil)
	require.False(t, isFound)
	chosen, isFound := ChooseNewestTicket([]support.Ticket{
		{ID: 1, UpdatedAt: time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC)},
		{ID: 2, UpdatedAt: time.Date(2026, 1, 28, 0, 0, 0, 0, time.UTC)},
		{ID: 3, UpdatedAt: time.Date(2026, 1, 25, 0, 0, 0, 0, time.UTC)},
	})
	require.True(t, isFound)
	require.Equal(t, int64(2), chosen.ID)
}

func TestIsFollowUpCandidateRequiresTheCustomersOwnUnsolvedTaggedTicket(t *testing.T) {
	issue := CustomerIssue{RequesterEmail: "jane@acme.example.com", IssueTag: "billing-double-charge"}
	candidate := support.TicketDetails{
		Ticket:    support.Ticket{ID: 5512, Status: support.TicketStatusPending, Tags: []string{"vip", "billing-double-charge"}},
		Requester: &support.TicketUser{ID: 1, Email: "Jane@Acme.example.com"},
	}
	require.True(t, IsFollowUpCandidate(candidate, issue))

	otherRequester := candidate
	otherRequester.Requester = &support.TicketUser{ID: 2, Email: "jane@acme.example.com.au"}
	require.False(t, IsFollowUpCandidate(otherRequester, issue))
	solved := candidate
	solved.Ticket.Status = support.TicketStatusSolved
	require.False(t, IsFollowUpCandidate(solved, issue))
	untagged := candidate
	untagged.Ticket.Tags = []string{"vip"}
	require.False(t, IsFollowUpCandidate(untagged, issue))
	missingRequester := candidate
	missingRequester.Requester = nil
	require.False(t, IsFollowUpCandidate(missingRequester, issue))
}

func newUnitTestConnection(t *testing.T, connectionName string) support.Connection {
	t.Helper()
	client, err := support.New(support.Config{Subdomain: "acme"}, sdkgo.StaticCredentialProvider[support.Credentials]{})
	require.NoError(t, err)
	connection, err := support.NewConnection(client, sdkgo.ConnectionRef{Provider: "zendesk", Name: connectionName})
	require.NoError(t, err)
	return connection
}
