//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contacts

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	contacts "github.com/superdurable/dex-connectors-library/connectors/google/contacts/go"
	"github.com/superdurable/dex-connectors-library/connectors/google/contacts/go/contactsmock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestContactsUsesDefaultMockWithRealDex(t *testing.T) {
	mock := contactsmock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	updateFlow := NewUpdateFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow, updateFlow})
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
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(stopCtx), <-workerResult, client.Close(), cache.Close()))
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	require.NoError(t, worker.WaitPullRegistered(ctx))
	flowID := "google-contacts-list-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, contacts.ListContactsInput{}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output contacts.ContactsPage
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, "people/c123", output.Contacts[0].ResourceName)
	require.Equal(t, "Example Contact", output.Contacts[0].DisplayName)
	require.Len(t, mock.ListContacts().Calls(), 1)
	updateFlowID := "google-contacts-update-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, updateFlow, updateFlowID, contacts.UpdateContactInput{ResourceName: "people/c123", Etag: "etag-1", GivenName: "Example"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	updateResult, err := client.WaitForFlow(ctx, updateFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, updateResult.Status, updateResult.ErrorMessage)
	var updated contacts.Contact
	require.NoError(t, updateResult.DecodeSingleOutput(&updated))
	require.Equal(t, "people/c123", updated.ResourceName)
	require.Len(t, mock.UpdateContact().Calls(), 1)
}
