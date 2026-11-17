//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package subscriberinspection

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/kit/go"
	"github.com/superdurable/dex-connectors-library/connectors/kit/go/kitmock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestSubscriberInspectionUsesDefaultMockWithRealDex(t *testing.T) {
	mock := kitmock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow, NewTagsFlow(mock.Connection()), NewCustomFieldsFlow(mock.Connection()), NewFormsFlow(mock.Connection())})
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
	flowID := "kit-subscriberinspection-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, kit.ListSubscribersInput{PerPage: 20}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output kit.SubscribersPage
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, 20, output.PerPage)
	require.Len(t, mock.ListSubscribers().Calls(), 1)
	tagsFlow := NewTagsFlow(mock.Connection())
	tagsID := "kit-tags-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, tagsFlow, tagsID, kit.ListTagsInput{PerPage: 20}, dex.StartFlowOptions{})
	require.NoError(t, err)
	tagsResult, err := client.WaitForFlow(ctx, tagsID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, tagsResult.Status, tagsResult.ErrorMessage)
	var tags kit.TagsPage
	require.NoError(t, tagsResult.DecodeSingleOutput(&tags))
	require.Len(t, tags.Tags, 1)
	require.Len(t, mock.ListTags().Calls(), 1)
	fieldsFlow := NewCustomFieldsFlow(mock.Connection())
	fieldsID := "kit-fields-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, fieldsFlow, fieldsID, kit.ListCustomFieldsInput{PerPage: 20}, dex.StartFlowOptions{})
	require.NoError(t, err)
	fieldsResult, err := client.WaitForFlow(ctx, fieldsID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, fieldsResult.Status, fieldsResult.ErrorMessage)
	var fields kit.CustomFieldsPage
	require.NoError(t, fieldsResult.DecodeSingleOutput(&fields))
	require.Len(t, fields.CustomFields, 1)
	require.Equal(t, int64(8), fields.CustomFields[0].ID)
	require.Len(t, mock.ListCustomFields().Calls(), 1)
	formsFlow := NewFormsFlow(mock.Connection())
	formsID := "kit-forms-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, formsFlow, formsID, kit.ListFormsInput{PerPage: 20}, dex.StartFlowOptions{})
	require.NoError(t, err)
	formsResult, err := client.WaitForFlow(ctx, formsID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, formsResult.Status, formsResult.ErrorMessage)
	var forms kit.FormsPage
	require.NoError(t, formsResult.DecodeSingleOutput(&forms))
	require.Len(t, forms.Forms, 1)
	require.Equal(t, int64(53), forms.Forms[0].ID)
	require.Len(t, mock.ListForms().Calls(), 1)
}
