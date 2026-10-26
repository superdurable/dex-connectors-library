//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package triageissue

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/freshworks/freshdesk"
	"github.com/superdurable/dex-connectors-library/connectors/freshworks/freshdesk/freshdeskmock"
)

func TestTriageUsesFreshdeskMockDefaultsWithRealDex(t *testing.T) {
	mock := freshdeskmock.New(t, ConnectionName)
	harness := newTriageHarnessForConnection(t, mock.Connection())
	input := integrationIssueInput()
	outcome := harness.runTriage(t, "mock-default", input)

	require.Equal(t, TriageTicketFollowedUp, outcome.Action)
	require.Equal(t, freshdesk.TicketStatusOpen, outcome.Ticket.Status)
	require.Equal(t, input.Priority, outcome.Ticket.Priority)
	require.Contains(t, outcome.Ticket.Tags, RepeatContactTag)
	require.Empty(t, mock.CreateTicket().Calls())

	searches := mock.SearchTickets().Calls()
	require.Len(t, searches, 2)
	require.Equal(t, freshdesk.SearchTicketsInput{Statuses: UnresolvedTicketStatuses, Tags: []string{"billing-double-charge"}, RequesterEmail: "jane@acme.example.com", Page: 1}, searches[0].Input)
	require.Equal(t, freshdesk.SearchTicketsInput{Statuses: UnresolvedTicketStatuses, Tags: []string{"billing-double-charge"}, RequesterEmail: "jane@acme.example.com", Page: 2}, searches[1].Input)
	reads := mock.GetTicket().Calls()
	require.Len(t, reads, 1)
	require.Equal(t, freshdesk.GetTicketInput{TicketID: 1201, ConversationLimit: 5}, reads[0].Input)
	updates := mock.UpdateTicket().Calls()
	require.Len(t, updates, 1)
	require.Equal(t, freshdesk.UpdateTicketInput{TicketID: 1201, Status: freshdesk.TicketStatusOpen, Priority: input.Priority, AddTags: []string{RepeatContactTag}}, updates[0].Input)
	notes := mock.AddNote().Calls()
	require.Len(t, notes, 1)
	require.Equal(t, freshdesk.AddNoteInput{TicketID: 1201, Body: BuildFollowUpNote(mustCustomerIssue(t, input))}, notes[0].Input)
}
