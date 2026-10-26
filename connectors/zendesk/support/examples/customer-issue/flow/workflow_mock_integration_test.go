//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package customerissue

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zendesk/support"
	"github.com/superdurable/dex-connectors-library/connectors/zendesk/support/supportmock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestCustomerIssueExampleRunsOnSupportMockWithRealDex(t *testing.T) {
	mock := supportmock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	defer func() { require.NoError(t, cache.Close()) }()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	workerAddress := listener.Addr().String()
	require.NoError(t, listener.Close())
	serverAddress := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	if serverAddress == "" {
		serverAddress = "127.0.0.1:8801"
	}
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: serverAddress, WorkerTarget: &dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	defer func() { require.NoError(t, client.Close()) }()
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress: workerAddress, FlowServiceAddress: serverAddress,
		WorkerTarget: dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	defer func() {
		stopContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(stopContext), <-workerResult))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := "zendesk-customer-issue-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	issue := Input{
		RequesterEmail: " Jane@acme.example.com ", RequesterName: " Jane Smith ",
		Subject: " Double charge on order 88213 ", Message: " I was charged twice for order 88213. ",
		IssueTag: "billing-double-charge",
	}
	_, err = client.StartFlow(ctx, flow, flowID, issue, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var outcome CustomerIssueOutcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	require.Equal(t, IssueFollowUpAdded, outcome.Action)
	require.Equal(t, int64(35436), outcome.Ticket.ID)
	require.Zero(t, outcome.SkippedTicketID)
	require.Empty(t, mock.CreateTicket().Calls(), "the existing matching ticket is followed up")

	searchCalls := mock.SearchTickets().Calls()
	require.Len(t, searchCalls, 1)
	require.Equal(t, flowID, searchCalls[0].FlowID)
	require.Equal(t, support.SearchTicketsInput{
		Statuses:       []support.TicketStatus{support.TicketStatusNew, support.TicketStatusOpen, support.TicketStatusPending, support.TicketStatusHold},
		RequesterEmail: "Jane@acme.example.com", Tags: []string{"billing-double-charge"}, PageSize: 10,
	}, searchCalls[0].Input)
	readCalls := mock.GetTicket().Calls()
	require.Len(t, readCalls, 1)
	require.Equal(t, support.GetTicketInput{TicketID: 35436, LatestCommentLimit: 5}, readCalls[0].Input)
	updateCalls := mock.UpdateTicket().Calls()
	require.Len(t, updateCalls, 1)
	require.Equal(t, support.UpdateTicketInput{
		TicketID: 35436, Status: support.TicketStatusOpen, AddTags: []string{"dex-repeat-contact"},
		Comment: &support.TicketCommentInput{Body: "The customer contacted us again about this issue. Message recorded by Dex:\n\nI was charged twice for order 88213.", IsInternalNote: true},
	}, updateCalls[0].Input)
}
