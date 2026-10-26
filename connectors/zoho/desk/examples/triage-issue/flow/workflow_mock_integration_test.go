//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package triageissue

import (
	"testing"

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

	searchCalls := mock.SearchTickets().Calls()
	require.Len(t, searchCalls, 3)
	for index, from := range []int{0, 25, 50} {
		require.Equal(t, desk.SearchTicketsInput{
			StatusTypes: UnresolvedTicketStatusTypes, ContactEmail: "jane@example.test",
			DepartmentID: "1892000000006907", From: from, Limit: SearchPageLimit,
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
	require.Equal(t, createCalls[0].Input.Subject, outcome.Ticket.Subject)
	require.Equal(t, createCalls[0].Input.Contact.Email, outcome.Ticket.Email)
}
