//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package triageissue

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/freshworks/freshdesk/go"
	"github.com/superdurable/dex-connectors-library/connectors/freshworks/freshdesk/go/freshdeskmock"
)

func TestTriageUsesFreshdeskMockDefaultsWithRealDex(t *testing.T) {
	mock := freshdeskmock.New(t, ConnectionName)
	harness := newTriageHarnessForConnection(t, mock.Connection())
	input := integrationIssueInput()
	outcome := harness.runTriage(t, "mock-default", input)

	require.Equal(t, TriageTicketFollowedUp, outcome.Action)
	require.Empty(t, mock.CreateTicket().Calls())

	searches := mock.SearchTickets().Calls()
	require.Len(t, searches, 2)
	require.Equal(t, freshdesk.SearchTicketsInput{Statuses: []freshdesk.TicketStatus{2, 3, 6, 7}, Tags: []string{"billing-double-charge"}, RequesterEmail: "jane@acme.example.com", Page: 1}, searches[0].Input)
	require.Equal(t, freshdesk.SearchTicketsInput{Statuses: []freshdesk.TicketStatus{2, 3, 6, 7}, Tags: []string{"billing-double-charge"}, RequesterEmail: "jane@acme.example.com", Page: 2}, searches[1].Input)
	reads := mock.GetTicket().Calls()
	require.Len(t, reads, 1)
	require.Equal(t, freshdesk.GetTicketInput{TicketID: 1201, ConversationLimit: 5}, reads[0].Input)
	updates := mock.UpdateTicket().Calls()
	require.Len(t, updates, 1)
	require.Equal(t, freshdesk.UpdateTicketInput{TicketID: 1201, Status: 2, Priority: 3, AddTags: []string{"dex-repeat-contact"}}, updates[0].Input)
	notes := mock.AddNote().Calls()
	require.Len(t, notes, 1)
	require.Equal(t, freshdesk.AddNoteInput{TicketID: 1201, Body: "Dex triage: the customer contacted us again about billing-double-charge. Priority set to High (3).\n\nCustomer message:\nI was charged twice for order 88213."}, notes[0].Input)
}
