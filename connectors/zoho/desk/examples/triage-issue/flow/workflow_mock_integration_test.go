//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package triageissue

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoho/desk"
	"github.com/superdurable/dex-connectors-library/connectors/zoho/desk/deskmock"
)

func TestNewIssueUsesZohoDeskMocksWithRealDex(t *testing.T) {
	mock := deskmock.New(t, ConnectionName)
	harness := newTriageHarnessForConnection(t, mock.Connection())
	input := Input{
		ContactEmail: " jane@example.test ", ContactFirstName: "Jane", ContactLastName: "Doe",
		Subject: "Billing question", Message: "Please check my invoice.",
		DepartmentID: "1892000000006907", Priority: desk.TicketPriorityHigh,
	}
	outcome := harness.runTriage(t, "mock-new-issue", input)
	require.Equal(t, TriageTicketOpened, outcome.Action)
	require.Equal(t, "1892000000099001", outcome.Ticket.ID)
	require.Equal(t, "Billing question", outcome.Ticket.Subject)
	require.Equal(t, "1892000000099002", outcome.CommentID)
	require.Empty(t, outcome.SkippedTicketID)
	require.False(t, outcome.WasAlreadyApplied)

	searchCalls := mock.SearchTickets().Calls()
	require.Len(t, searchCalls, 3)
	for index, from := range []int{0, 25, 50} {
		require.Equal(t, desk.SearchTicketsInput{
			StatusTypes:  []desk.TicketStatusType{desk.TicketStatusTypeOpen, desk.TicketStatusTypeOnHold},
			ContactEmail: "jane@example.test", DepartmentID: "1892000000006907", From: from, Limit: 25,
		}, searchCalls[index].Input)
	}
	require.Empty(t, mock.GetTicket().Calls())
	require.Empty(t, mock.UpdateTicket().Calls())
	createCalls := mock.CreateTicket().Calls()
	require.Len(t, createCalls, 1)
	require.Equal(t, desk.CreateTicketInput{
		Subject: "Billing question", Description: "Please check my invoice.",
		DepartmentID: "1892000000006907", Contact: desk.TicketContactInput{
			Email: "jane@example.test", FirstName: "Jane", LastName: "Doe",
		}, Status: desk.TicketStatusOpen, Priority: desk.TicketPriorityHigh,
	}, createCalls[0].Input)
	commentCalls := mock.AddComment().Calls()
	require.Len(t, commentCalls, 1)
	require.Equal(t, desk.AddCommentInput{
		TicketID: "1892000000099001",
		Content:  "Dex triage: opened with priority High; the contact had no open or on-hold ticket in this department.",
	}, commentCalls[0].Input)
}

func TestFollowUpUsesZohoDeskMockDefaultsWithRealDex(t *testing.T) {
	mock := deskmock.New(t, ConnectionName)
	mock.SearchTickets().Respond(deskmock.SearchTicketsSearched(desk.SearchTicketsOutput{
		Tickets: []desk.Ticket{{ID: "1892000000099001"}}, TotalMatched: 1,
	}, nil))
	mock.AddComment().Respond(deskmock.AddCommentAdded(desk.AddCommentOutput{
		Comment: desk.TicketComment{
			ID: "1892000000099003", Content: "Dex triage: the customer contacted us again. Priority set to High.\n\nCustomer message:\nPlease recheck my invoice.",
			ContentType: "plainText", CommenterID: "1892000000056007", CommentedAt: time.Date(2026, time.January, 28, 9, 5, 0, 0, time.UTC),
		},
	}, nil))
	harness := newTriageHarnessForConnection(t, mock.Connection())
	input := Input{
		ContactEmail: " jane@example.test ", Subject: "Billing question",
		Message: "Please recheck my invoice.", DepartmentID: "1892000000006907", Priority: desk.TicketPriorityHigh,
	}
	outcome := harness.runTriage(t, "mock-follow-up", input)
	require.Equal(t, TriageTicketFollowedUp, outcome.Action)
	require.Equal(t, "1892000000099001", outcome.Ticket.ID)
	require.Equal(t, desk.TicketStatusOpen, outcome.Ticket.Status)
	require.Equal(t, desk.TicketPriorityHigh, outcome.Ticket.Priority)
	require.False(t, outcome.WasAlreadyApplied)
	require.Equal(t, "1892000000099003", outcome.CommentID)
	require.Empty(t, mock.CreateTicket().Calls())

	searchCalls := mock.SearchTickets().Calls()
	require.Len(t, searchCalls, 1)
	require.Equal(t, desk.SearchTicketsInput{
		StatusTypes:  []desk.TicketStatusType{desk.TicketStatusTypeOpen, desk.TicketStatusTypeOnHold},
		ContactEmail: "jane@example.test", DepartmentID: "1892000000006907", Limit: 25,
	}, searchCalls[0].Input)
	getCalls := mock.GetTicket().Calls()
	require.Len(t, getCalls, 1)
	require.Equal(t, desk.GetTicketInput{TicketID: "1892000000099001", ThreadLimit: 3, CommentLimit: 5}, getCalls[0].Input)
	updateCalls := mock.UpdateTicket().Calls()
	require.Len(t, updateCalls, 1)
	require.Equal(t, desk.UpdateTicketInput{
		TicketID: "1892000000099001", Status: desk.TicketStatusOpen, Priority: desk.TicketPriorityHigh,
	}, updateCalls[0].Input)
	commentCalls := mock.AddComment().Calls()
	require.Len(t, commentCalls, 1)
	require.Equal(t, desk.AddCommentInput{
		TicketID: "1892000000099001",
		Content:  "Dex triage: the customer contacted us again. Priority set to High.\n\nCustomer message:\nPlease recheck my invoice.",
	}, commentCalls[0].Input)
}
