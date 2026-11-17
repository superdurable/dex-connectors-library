//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package templatelist

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/sendgrid/go"
	"github.com/superdurable/dex-connectors-library/connectors/sendgrid/go/sendgridmock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestContactListAndMailUseMockWithRealDex(t *testing.T) {
	mock := sendgridmock.New(t, ConnectionName)
	flow := NewContactFlow(mock.Connection())
	listFlow := NewMarketingListFlow(mock.Connection())
	mailFlow := NewMailFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow, listFlow, mailFlow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	serverAddress := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	if serverAddress == "" {
		serverAddress = "127.0.0.1:9600"
	}
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{FlowServiceAddress: serverAddress})
	require.NoError(t, err)
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: serverAddress})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	t.Cleanup(func() {
		stopContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(stopContext), <-workerResult, client.Close(), cache.Close()))
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	require.NoError(t, worker.WaitPullRegistered(ctx))
	flowID := "sendgrid-contact-lookup-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, sendgrid.ContactIDInput{ID: "c-123"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var contact sendgrid.Contact
	require.NoError(t, result.DecodeSingleOutput(&contact))
	require.Equal(t, "priya@example.com", contact.Email)
	calls := mock.GetContact().Calls()
	require.Len(t, calls, 1)
	require.Equal(t, sendgrid.ContactIDInput{ID: "c-123"}, calls[0].Input)
	listFlowID := "sendgrid-list-page-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, listFlow, listFlowID, sendgrid.ListListsInput{PageSize: 25}, dex.StartFlowOptions{})
	require.NoError(t, err)
	listResult, err := client.WaitForFlow(ctx, listFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, listResult.Status, listResult.ErrorMessage)
	var listPage sendgrid.ListListsOutput
	require.NoError(t, listResult.DecodeSingleOutput(&listPage))
	require.Len(t, listPage.Lists, 1)
	require.Equal(t, "Customers", listPage.Lists[0].Name)
	require.Len(t, mock.ListLists().Calls(), 1)
	mailInput := sendgrid.SendMailInput{From: "sender@example.com", To: []string{"priya@example.com"}, Subject: "Example notice", Text: "Hello from the mock-backed example"}
	mailFlowID := "sendgrid-mail-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, mailFlow, mailFlowID, mailInput, dex.StartFlowOptions{})
	require.NoError(t, err)
	mailResult, err := client.WaitForFlow(ctx, mailFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, mailResult.Status, mailResult.ErrorMessage)
	var mailOutput sendgrid.SendMailOutput
	require.NoError(t, mailResult.DecodeSingleOutput(&mailOutput))
	require.True(t, mailOutput.Accepted)
	mailCalls := mock.SendMail().Calls()
	require.Len(t, mailCalls, 1)
	require.Equal(t, mailInput, mailCalls[0].Input)
}
