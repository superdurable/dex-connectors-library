//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactread

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/keap/go"
	"github.com/superdurable/dex-connectors-library/connectors/keap/go/keapmock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestContactReadUsesDefaultMockWithRealDex(t *testing.T) {
	mock := keapmock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow, NewDirectoryFlow(mock.Connection()), NewCompanyFlow(mock.Connection())})
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
	flowID := "keap-contactread-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, keap.GetContactInput{ContactID: "123"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output keap.ContactProfile
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, "123", output.ID)
	require.Equal(t, "Example", output.GivenName)
	require.Len(t, mock.GetContact().Calls(), 1)
	directoryFlow := NewDirectoryFlow(mock.Connection())
	directoryID := "keap-directory-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, directoryFlow, directoryID, keap.ListContactsInput{PageSize: 20}, dex.StartFlowOptions{})
	require.NoError(t, err)
	directoryResult, err := client.WaitForFlow(ctx, directoryID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, directoryResult.Status, directoryResult.ErrorMessage)
	var page keap.ContactsPage
	require.NoError(t, directoryResult.DecodeSingleOutput(&page))
	require.Len(t, page.Contacts, 1)
	require.Equal(t, "123", page.Contacts[0].ID)
	require.Len(t, mock.ListContacts().Calls(), 1)
	companyFlow := NewCompanyFlow(mock.Connection())
	companyFlowID := "keap-company-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, companyFlow, companyFlowID, keap.CompanyIDInput{CompanyID: "321"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	companyResult, err := client.WaitForFlow(ctx, companyFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, companyResult.Status, companyResult.ErrorMessage)
	var company keap.CompanyProfile
	require.NoError(t, companyResult.DecodeSingleOutput(&company))
	require.Equal(t, "321", company.ID)
	require.Len(t, mock.GetCompany().Calls(), 1)
}
