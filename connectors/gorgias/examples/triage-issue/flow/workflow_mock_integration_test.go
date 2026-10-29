//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package triageissue

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/gorgias"
	"github.com/superdurable/dex-connectors-library/connectors/gorgias/gorgiasmock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestTriageIssueUsesGorgiasMockWithRealDex(t *testing.T) {
	mock := gorgiasmock.New(t, ConnectionName)
	mock.FindCustomerByEmail().When(func(input gorgias.FindCustomerByEmailInput) bool { return input.Email == "nobody@example.com" },
		gorgiasmock.FindCustomerByEmailCustomerAbsent())
	mock.AddNote().When(func(input gorgias.AddNoteInput) bool { return input.TicketID == 7002 },
		gorgiasmock.AddNoteAdded(gorgias.AddNoteOutput{Message: gorgias.TicketMessage{
			ID: 9002, TicketID: 7002, Channel: gorgias.MessageChannelInternalNote, Via: "api",
			IsFromAgent: true, Body: "Refund triage", CreatedAt: time.Date(2026, 10, 26, 9, 5, 0, 0, time.UTC),
		}}, nil))
	harness := &triageHarness{flow: NewFlow(mock.Connection()), serverAddress: environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")}
	var err error
	harness.registry, err = dex.NewRegistry([]dex.Flow{harness.flow})
	require.NoError(t, err)
	harness.cache, err = blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	harness.client, err = dex.NewClient(harness.registry, harness.cache, dex.ClientOptions{
		FlowServiceAddress: harness.serverAddress,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(harness.worker.Stop(ctx), <-harness.workerResult, harness.client.Close(), harness.cache.Close()))
	})
	harness.startWorker(t)

	followUp := harness.runTriage(t, "mock-follow-up", Input{
		RequesterEmail: "ada@example.com", RequesterName: "Ada Lovelace", Subject: "Duplicate charge",
		Message: "I was charged twice.", IssueTag: "billing-double-charge", Priority: gorgias.TicketPriorityHigh,
	})
	require.Equal(t, TriageTicketFollowedUp, followUp.Action)
	require.False(t, followUp.WasAlreadyApplied)
	require.Zero(t, followUp.AcknowledgementID)
	require.Len(t, mock.FindCustomerByEmail().Calls(), 1)
	require.Equal(t, gorgias.FindCustomerByEmailInput{Email: "ada@example.com"}, mock.FindCustomerByEmail().Calls()[0].Input)
	searchCalls := mock.SearchTickets().Calls()
	require.Len(t, searchCalls, 1)
	require.Equal(t, gorgias.SearchTicketsInput{
		RequesterID: 501, Statuses: []gorgias.TicketStatus{gorgias.TicketStatusOpen}, Tags: []string{"billing-double-charge"}, PageSize: 100,
	}, searchCalls[0].Input)
	getCalls := mock.GetTicket().Calls()
	require.Len(t, getCalls, 1)
	require.Equal(t, gorgias.GetTicketInput{TicketID: 7001, LatestMessageLimit: 5}, getCalls[0].Input)
	updateCalls := mock.UpdateTicket().Calls()
	require.Len(t, updateCalls, 1)
	require.Equal(t, gorgias.UpdateTicketInput{
		TicketID: 7001, Status: gorgias.TicketStatusOpen, Priority: gorgias.TicketPriorityHigh, AddTags: []string{"dex-repeat-contact"},
	}, updateCalls[0].Input)

	opened := harness.runTriage(t, "mock-new-customer", Input{
		RequesterEmail: "nobody@example.com", RequesterName: "New Customer", Subject: "Refund question",
		Message: "Please explain my refund.", IssueTag: "billing-refund", Priority: gorgias.TicketPriorityHigh,
	})
	require.Equal(t, TriageTicketOpened, opened.Action)
	require.False(t, opened.WasTicketCreatedByEarlierAttempt)
	require.Len(t, mock.SearchTickets().Calls(), 1, "new customers skip the search")
	createCalls := mock.CreateTicket().Calls()
	require.Len(t, createCalls, 1)
	require.Equal(t, gorgias.CreateTicketInput{
		Subject: "Refund question", Description: "Please explain my refund.",
		Requester: gorgias.TicketRequesterInput{Email: "nobody@example.com", Name: "New Customer"},
		Status:    gorgias.TicketStatusOpen, Priority: gorgias.TicketPriorityHigh, Tags: []string{"billing-refund"},
	}, createCalls[0].Input)
	noteCalls := mock.AddNote().Calls()
	require.Len(t, noteCalls, 2)
	require.Equal(t, gorgias.AddNoteInput{
		TicketID: 7001, Body: "Dex triage: the customer contacted us again about billing-double-charge. Priority set to high.\n\nCustomer message:\nI was charged twice.",
	}, noteCalls[0].Input)
	require.Equal(t, gorgias.AddNoteInput{
		TicketID: 7002, Body: "Dex triage: opened for billing-refund with priority high; the customer had no open ticket carrying the tag.",
	}, noteCalls[1].Input)
	require.Len(t, mock.FindCustomerByEmail().Calls(), 2)
	require.Len(t, mock.GetTicket().Calls(), 1)
	require.Len(t, mock.UpdateTicket().Calls(), 1)
}
