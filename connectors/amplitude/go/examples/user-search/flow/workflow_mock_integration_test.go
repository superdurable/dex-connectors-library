//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package usersearch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/amplitude/go"
	"github.com/superdurable/dex-connectors-library/connectors/amplitude/go/amplitudemock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestUserSearchUsesDefaultMockWithRealDex(t *testing.T) {
	mock := amplitudemock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	trackFlow := NewTrackFlow(mock.Connection())
	unsetFlow := NewUnsetPropertyFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow, trackFlow, unsetFlow})
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
	flowID := "amplitude-usersearch-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, amplitude.SearchUsersInput{User: "example-user"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output amplitude.UserSearchPage
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, "example-user", output.User)
	require.Equal(t, []string{"example-user"}, output.UserIDs)
	require.Equal(t, "match_user_or_device_id", output.MatchType)
	require.Len(t, mock.SearchUsers().Calls(), 1)
	trackID := "amplitude-track-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, trackFlow, trackID, amplitude.TrackEventInput{UserID: "example-user", EventType: "purchase", EventProperties: map[string]string{"plan": "pro"}}, dex.StartFlowOptions{})
	require.NoError(t, err)
	trackResult, err := client.WaitForFlow(ctx, trackID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, trackResult.Status, trackResult.ErrorMessage)
	var tracked amplitude.TrackedEvent
	require.NoError(t, trackResult.DecodeSingleOutput(&tracked))
	require.Equal(t, "example-insert-id", tracked.InsertID)
	require.Len(t, mock.TrackEvent().Calls(), 1)
	unsetID := "amplitude-unset-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, unsetFlow, unsetID, amplitude.UnsetUserPropertyInput{UserID: "example-user", PropertyName: "oldPlan"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	unsetResult, err := client.WaitForFlow(ctx, unsetID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, unsetResult.Status, unsetResult.ErrorMessage)
	var unset amplitude.IdentifiedUser
	require.NoError(t, unsetResult.DecodeSingleOutput(&unset))
	require.Equal(t, "oldPlan", unset.PropertyName)
	require.Len(t, mock.UnsetUserProperty().Calls(), 1)
}
