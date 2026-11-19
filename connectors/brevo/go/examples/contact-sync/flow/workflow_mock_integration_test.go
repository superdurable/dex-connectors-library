//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactsync

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/brevo/go/brevomock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestContactSyncUsesMockWithRealDex(t *testing.T) {
	mock := brevomock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	serverAddress := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	if serverAddress == "" {
		serverAddress = "127.0.0.1:8801"
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
	{
		registrationContext, cancelRegistration := context.WithTimeout(t.Context(), 30*time.Second)
		registrationError := worker.WaitPullRegistered(registrationContext)
		cancelRegistration()
		require.NoError(t, registrationError)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := "brevo-contact-sync-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, Request{Email: "member@meridian.example", FirstName: "Meridian", SenderID: 1, SenderName: "Support", AttributeName: "CUSTOMER_TIER", AttributeOptions: []string{"Gold", "Silver"}, EmailSenderAddress: "sender@example.com", EmailSenderName: "Support", EmailSubject: "Welcome", EmailText: "Hello"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	calls := mock.CreateOrUpdateContact().Calls()
	require.Len(t, calls, 1)
	require.Equal(t, flowID, calls[0].FlowID)
	require.Equal(t, "member@meridian.example", calls[0].Input.Email)
	require.Len(t, mock.ListAttributes().Calls(), 1)
	require.Len(t, mock.UpdateSender().Calls(), 1)
	require.Equal(t, int64(1), mock.UpdateSender().Calls()[0].Input.ID)
	require.Len(t, mock.UpdateAttribute().Calls(), 1)
	require.Equal(t, "CUSTOMER_TIER", mock.UpdateAttribute().Calls()[0].Input.Name)
	require.Len(t, mock.SendEmail().Calls(), 1)
	require.Equal(t, "member@meridian.example", mock.SendEmail().Calls()[0].Input.ToEmail)

	templateFlowID := flowID + "-template"
	_, err = client.StartFlow(ctx, flow, templateFlowID, Request{Email: "member@meridian.example", FirstName: "Meridian", EmailTemplateID: 42, EmailTemplateParams: map[string]string{"FIRSTNAME": "Meridian"}}, dex.StartFlowOptions{})
	require.NoError(t, err)
	templateResult, err := client.WaitForFlow(ctx, templateFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, templateResult.Status, templateResult.ErrorMessage)
	require.Len(t, mock.SendTemplateEmail().Calls(), 1)
	require.Equal(t, int64(42), mock.SendTemplateEmail().Calls()[0].Input.TemplateID)

	strictFlowID := flowID + "-strict"
	_, err = client.StartFlow(ctx, flow, strictFlowID, Request{Email: "member@meridian.example", FirstName: "Meridian", StrictCreate: true}, dex.StartFlowOptions{})
	require.NoError(t, err)
	strictResult, err := client.WaitForFlow(ctx, strictFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, strictResult.Status, strictResult.ErrorMessage)
	require.Len(t, mock.CreateContact().Calls(), 1)
	require.Equal(t, "member@meridian.example", mock.CreateContact().Calls()[0].Input.Email)
}
