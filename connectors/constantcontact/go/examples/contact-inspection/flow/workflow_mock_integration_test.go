//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactinspection

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/constantcontact/go"
	"github.com/superdurable/dex-connectors-library/connectors/constantcontact/go/constantcontactmock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestContactInspectionUsesDefaultMockWithRealDex(t *testing.T) {
	mock := constantcontactmock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	contactListsFlow := NewContactListsFlow(mock.Connection())
	createContactFlow := NewCreateContactFlow(mock.Connection())
	updateContactFlow := NewUpdateContactFlow(mock.Connection())
	activitiesFlow := NewActivitiesFlow(mock.Connection())
	segmentsFlow := NewSegmentsFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow, contactListsFlow, createContactFlow, updateContactFlow, activitiesFlow, segmentsFlow})
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
	flowID := "constantcontact-contactinspection-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, constantcontact.GetContactInput{ContactID: "1898ae62-4752-11e9-9c8a-fa163e6b01c1"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output constantcontact.ContactMetadata
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, "1898ae62-4752-11e9-9c8a-fa163e6b01c1", output.ContactID)
	require.Len(t, mock.GetContact().Calls(), 1)
	listsFlowID := "constantcontact-lists-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, contactListsFlow, listsFlowID, constantcontact.ListContactListsInput{Limit: 2}, dex.StartFlowOptions{})
	require.NoError(t, err)
	listsResult, err := client.WaitForFlow(ctx, listsFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, listsResult.Status, listsResult.ErrorMessage)
	var listsOutput constantcontact.ContactListsPage
	require.NoError(t, listsResult.DecodeSingleOutput(&listsOutput))
	require.Len(t, listsOutput.Lists, 1)
	require.Len(t, mock.ListContactLists().Calls(), 1)
	createFlowID := "constantcontact-create-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, createContactFlow, createFlowID, constantcontact.CreateContactInput{EmailAddress: "reader@example.com", FirstName: "Avery", LastName: "Reader", EmailPermission: "implicit", CreateSource: "Account"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	createResult, err := client.WaitForFlow(ctx, createFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, createResult.Status, createResult.ErrorMessage)
	var createdContact constantcontact.ContactMetadata
	require.NoError(t, createResult.DecodeSingleOutput(&createdContact))
	require.Equal(t, "reader@example.com", createdContact.EmailAddress)
	require.Len(t, mock.CreateContact().Calls(), 1)
	updatedName := "Updated"
	updateFlowID := "constantcontact-update-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, updateContactFlow, updateFlowID, constantcontact.UpdateContactInput{ContactID: "1898ae62-4752-11e9-9c8a-fa163e6b01c1", UpdateSource: "Account", FirstName: &updatedName}, dex.StartFlowOptions{})
	require.NoError(t, err)
	updateResult, err := client.WaitForFlow(ctx, updateFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, updateResult.Status, updateResult.ErrorMessage)
	var updatedContact constantcontact.ContactMetadata
	require.NoError(t, updateResult.DecodeSingleOutput(&updatedContact))
	require.Equal(t, "1898ae62-4752-11e9-9c8a-fa163e6b01c1", updatedContact.ContactID)
	require.Len(t, mock.UpdateContact().Calls(), 1)
	activitiesFlowID := "constantcontact-activities-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, activitiesFlow, activitiesFlowID, constantcontact.ListActivitiesInput{MaxItems: 2}, dex.StartFlowOptions{})
	require.NoError(t, err)
	activitiesResult, err := client.WaitForFlow(ctx, activitiesFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, activitiesResult.Status, activitiesResult.ErrorMessage)
	var activitiesOutput constantcontact.ActivitiesPage
	require.NoError(t, activitiesResult.DecodeSingleOutput(&activitiesOutput))
	require.Len(t, activitiesOutput.Activities, 1)
	require.Len(t, mock.ListActivities().Calls(), 1)
	segmentsFlowID := "constantcontact-segments-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, segmentsFlow, segmentsFlowID, constantcontact.ListSegmentsInput{Limit: 2}, dex.StartFlowOptions{})
	require.NoError(t, err)
	segmentsResult, err := client.WaitForFlow(ctx, segmentsFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, segmentsResult.Status, segmentsResult.ErrorMessage)
	var segmentsOutput constantcontact.SegmentsPage
	require.NoError(t, segmentsResult.DecodeSingleOutput(&segmentsOutput))
	require.Len(t, segmentsOutput.Segments, 1)
	require.Len(t, mock.ListSegments().Calls(), 1)
}
