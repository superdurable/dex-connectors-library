//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactdiscovery

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/insightly/go"
	"github.com/superdurable/dex-connectors-library/connectors/insightly/go/insightlymock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestContactDiscoveryAndLeadLifecycleUseDefaultMocksWithRealDex(t *testing.T) {
	mock := insightlymock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	lifecycleFlow := NewLeadLifecycleFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow, lifecycleFlow})
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
	flowID := "insightly-contactdiscovery-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, insightly.ListContactsInput{Top: 20, Skip: 0}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output insightly.ContactsPage
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, int64(45678), output.Contacts[0].ID)
	require.Len(t, mock.ListContacts().Calls(), 1)
	lifecycleID := "insightly-leadlifecycle-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, lifecycleFlow, lifecycleID, LeadLifecycleInput{
		FirstName: "Ada", LastName: "Lovelace", Email: "ada@example.com", UpdatedLastName: "Byron",
	}, dex.StartFlowOptions{})
	require.NoError(t, err)
	lifecycleResult, err := client.WaitForFlow(ctx, lifecycleID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, lifecycleResult.Status, lifecycleResult.ErrorMessage)
	var searchPage insightly.RecordsPage
	require.NoError(t, lifecycleResult.DecodeSingleOutput(&searchPage))
	require.Len(t, searchPage.Records, 1)
	require.Len(t, mock.CreateRecord().Calls(), 1)
	require.Equal(t, insightly.CreateRecordInput{RecordType: insightly.RecordTypeLeads, Fields: map[string]json.RawMessage{
		"FIRST_NAME": json.RawMessage(`"Ada"`), "LAST_NAME": json.RawMessage(`"Lovelace"`), "EMAIL": json.RawMessage(`"ada@example.com"`),
	}}, mock.CreateRecord().Calls()[0].Input)
	require.Len(t, mock.GetRecord().Calls(), 1)
	require.Equal(t, insightly.GetRecordInput{RecordType: insightly.RecordTypeLeads, ID: 1201}, mock.GetRecord().Calls()[0].Input)
	require.Len(t, mock.UpdateRecord().Calls(), 1)
	require.Equal(t, insightly.UpdateRecordInput{RecordType: insightly.RecordTypeLeads, ID: 1201, Fields: map[string]json.RawMessage{"LAST_NAME": json.RawMessage(`"Byron"`)}}, mock.UpdateRecord().Calls()[0].Input)
	require.Len(t, mock.DeleteRecord().Calls(), 1)
	require.Equal(t, insightly.DeleteRecordInput{RecordType: insightly.RecordTypeLeads, ID: 1201}, mock.DeleteRecord().Calls()[0].Input)
	require.Len(t, mock.ListRecords().Calls(), 1)
	require.Equal(t, insightly.ListRecordsInput{RecordType: insightly.RecordTypeLeads, Top: 10}, mock.ListRecords().Calls()[0].Input)
	require.Len(t, mock.SearchRecords().Calls(), 1)
	require.Equal(t, insightly.SearchRecordsInput{RecordType: insightly.RecordTypeLeads, FieldName: "EMAIL", FieldValue: "ada@example.com", Top: 10}, mock.SearchRecords().Calls()[0].Input)
}
