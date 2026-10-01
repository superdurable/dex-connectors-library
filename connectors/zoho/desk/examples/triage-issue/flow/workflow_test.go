// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package triageissue

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoho/desk"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const unitDepartmentID = "1892000000006907"

func TestStartFlowIdentitiesMatchTheFlowDefinition(t *testing.T) {
	flow := NewFlow(newUnitTestConnection(t, ConnectionName))
	require.Equal(t, FlowType, dex.GetFinalFlowType(flow))
	require.Equal(t, recordCustomerIssueStepType, dex.GetFinalStepType[Input](recordCustomerIssue{}))
	require.Equal(t, chooseIssueTicketStepType, dex.GetFinalStepType[desk.SearchTicketsResult](chooseIssueTicket{}))
	require.Equal(t, confirmCandidateTicketStepType, dex.GetFinalStepType[desk.GetTicketResult](confirmCandidateTicket{}))
	require.Equal(t, recordPrioritizedTicketStepType, dex.GetFinalStepType[desk.UpdateTicketResult](recordPrioritizedTicket{}))
	require.Equal(t, recordOpenedTicketStepType, dex.GetFinalStepType[desk.CreateTicketResult](recordOpenedTicket{}))
	require.Equal(t, recordUncertainTicketStepType, dex.GetFinalStepType[desk.CreateTicketResult](recordUncertainTicket{}))
	require.Equal(t, completeTriageStepType, dex.GetFinalStepType[desk.AddCommentResult](completeTriage{}))
	require.Equal(t, recordUncertainCommentStepType, dex.GetFinalStepType[desk.AddCommentResult](recordUncertainComment{}))
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
		ContactEmail: " jane@acme.example.com ", ContactFirstName: "Jane", ContactLastName: "Smith", Subject: " Double charge ",
		Message: "I was charged twice.", DepartmentID: unitDepartmentID, Priority: " High ",
	})
	require.NoError(t, err)
	require.Equal(t, CustomerIssue{
		ContactEmail: "jane@acme.example.com", ContactFirstName: "Jane", ContactLastName: "Smith", Subject: "Double charge",
		Message: "I was charged twice.", DepartmentID: unitDepartmentID, Priority: desk.TicketPriorityHigh,
	}, issue)
	valid := Input{ContactEmail: "jane@acme.example.com", Subject: "s", Message: "m", DepartmentID: unitDepartmentID, Priority: "Low"}
	for name, change := range map[string]func(*Input){
		"display address":  func(input *Input) { input.ContactEmail = "Jane <jane@acme.example.com>" },
		"wildcard address": func(input *Input) { input.ContactEmail = "jane*@acme.example.com" },
		"blank message":    func(input *Input) { input.Message = " " },
		"department name":  func(input *Input) { input.DepartmentID = "Billing" },
		"no priority":      func(input *Input) { input.Priority = "" },
		"priority list":    func(input *Input) { input.Priority = "High,Low" },
	} {
		input := valid
		change(&input)
		_, err := BuildCustomerIssue(input)
		require.Error(t, err, name)
	}
}

func TestMappersPassOnlyTheRecordedIssue(t *testing.T) {
	issue := CustomerIssue{
		ContactEmail: "jane@acme.example.com", ContactFirstName: "Jane", ContactLastName: "Smith", Subject: "Double charge",
		Message: "I was charged twice.", DepartmentID: unitDepartmentID, Priority: desk.TicketPriorityHigh,
	}
	require.Equal(t, desk.SearchTicketsInput{
		StatusTypes: []desk.TicketStatusType{desk.TicketStatusTypeOpen, desk.TicketStatusTypeOnHold}, ContactEmail: "jane@acme.example.com",
		DepartmentID: unitDepartmentID, From: 25, Limit: SearchPageLimit,
	}, MapToSearchTicketsInput(IssueTicketSearch{ContactEmail: "jane@acme.example.com", DepartmentID: unitDepartmentID, From: 25}))
	require.Equal(t, desk.GetTicketInput{TicketID: "5512", ThreadLimit: candidateThreadLimit, CommentLimit: candidateCommentLimit},
		MapToGetTicketInput(IssueTicketReference{TicketID: "5512"}))
	require.Equal(t, desk.UpdateTicketInput{TicketID: "5512", Status: desk.TicketStatusOpen, Priority: desk.TicketPriorityHigh},
		MapToUpdateTicketInput(TicketPriorityChange{TicketID: "5512", Priority: desk.TicketPriorityHigh}))
	require.Equal(t, desk.CreateTicketInput{
		Subject: "Double charge", Description: "I was charged twice.", DepartmentID: unitDepartmentID,
		Contact: desk.TicketContactInput{Email: "jane@acme.example.com", FirstName: "Jane", LastName: "Smith"},
		Status:  desk.TicketStatusOpen, Priority: desk.TicketPriorityHigh,
	}, MapToCreateTicketInput(issue))
	require.Equal(t, desk.AddCommentInput{TicketID: "5512", Content: "comment"}, MapToAddCommentInput(TriageComment{TicketID: "5512", Content: "comment"}),
		"the triage comment is private")
	require.Equal(t, "Dex triage: the customer contacted us again. Priority set to High.\n\nCustomer message:\nI was charged twice.", BuildFollowUpComment(issue))
	require.Equal(t, "Dex triage: opened with priority High; the contact had no open or on-hold ticket in this department.", BuildOpenedTicketComment(issue))
}

func TestChooseNewestTicketPrefersTheLatestModification(t *testing.T) {
	_, isFound := ChooseNewestTicket(nil)
	require.False(t, isFound)
	chosen, isFound := ChooseNewestTicket([]desk.Ticket{
		{ID: "1", ModifiedAt: time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC)},
		{ID: "2", ModifiedAt: time.Date(2026, 1, 28, 0, 0, 0, 0, time.UTC)},
		{ID: "3", ModifiedAt: time.Date(2026, 1, 25, 0, 0, 0, 0, time.UTC)},
	})
	require.True(t, isFound)
	require.Equal(t, "2", chosen.ID)
}

func TestIsFollowUpCandidateRequiresTheContactsOwnUnresolvedTicketInTheDepartment(t *testing.T) {
	issue := CustomerIssue{ContactEmail: "jane@acme.example.com", DepartmentID: unitDepartmentID}
	candidate := desk.TicketDetails{
		Ticket:  desk.Ticket{ID: "5512", Status: "Waiting for Customer", StatusType: desk.TicketStatusTypeOnHold, DepartmentID: unitDepartmentID},
		Contact: &desk.TicketContact{ID: "1", Email: "Jane@Acme.example.com"},
	}
	require.True(t, IsFollowUpCandidate(candidate, issue), "the contact's email matches without regard to letter case")

	byTicketEmail := candidate
	byTicketEmail.Contact = nil
	byTicketEmail.Ticket.Email = "jane@acme.example.com"
	require.True(t, IsFollowUpCandidate(byTicketEmail, issue))

	otherContact := candidate
	otherContact.Contact = &desk.TicketContact{ID: "2", Email: "jane@acme.example.com.au"}
	require.False(t, IsFollowUpCandidate(otherContact, issue))
	closed := candidate
	closed.Ticket.StatusType = desk.TicketStatusTypeClosed
	require.False(t, IsFollowUpCandidate(closed, issue))
	otherDepartment := candidate
	otherDepartment.Ticket.DepartmentID = "1892000000082069"
	require.False(t, IsFollowUpCandidate(otherDepartment, issue))
	missingContact := candidate
	missingContact.Contact = nil
	require.False(t, IsFollowUpCandidate(missingContact, issue))
}

func newUnitTestConnection(t *testing.T, connectionName string) desk.Connection {
	t.Helper()
	client, err := desk.New(desk.Config{OrgID: "2389290"}, sdkgo.StaticCredentialProvider[desk.Credentials]{})
	require.NoError(t, err)
	connection, err := desk.NewConnection(client, sdkgo.ConnectionRef{Provider: "zoho", Name: connectionName})
	require.NoError(t, err)
	return connection
}
