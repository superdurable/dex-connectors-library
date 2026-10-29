//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package authenticatedprofile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/linkedin"
	"github.com/superdurable/dex-connectors-library/connectors/linkedin/linkedinmock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestAuthenticatedProfileExampleRunsOnLinkedInMockWithRealDex(t *testing.T) {
	mock := linkedinmock.New(t, ConnectionName)
	mock.GetAuthenticatedProfile().Respond(linkedinmock.GetAuthenticatedProfileVerifiedSignupProfile())
	flow := NewFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	defer func() { require.NoError(t, cache.Close()) }()

	serverAddress := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	if serverAddress == "" {
		serverAddress = "127.0.0.1:8801"
	}
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: serverAddress,
	})
	require.NoError(t, err)
	defer func() { require.NoError(t, client.Close()) }()
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{FlowServiceAddress: serverAddress})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	defer func() {
		stopContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(stopContext), <-workerResult))
	}()
	{
		registrationContext, cancelRegistration := context.WithTimeout(t.Context(), 30*time.Second)
		registrationError := worker.WaitPullRegistered(registrationContext)
		cancelRegistration()
		require.NoError(t, registrationError)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := "linkedin-authenticated-profile-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, Input{RequestLabel: " signup-profile "}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var profile linkedin.AuthenticatedProfile
	require.NoError(t, result.DecodeSingleOutput(&profile))
	require.Equal(t, "linkedin-member-ada", profile.Subject)
	require.Equal(t, "ada@example.com", profile.VerifiedEmail)
	require.Equal(t, "Ada Lovelace", profile.Name)
	var summary map[string]any
	require.NoError(t, client.InvokeRPC(ctx, flowID, flow.GetDexSummary, dex.None(nil), &summary))
	require.Equal(t, map[string]any{"requestLabel": "signup-profile"}, summary["linkedin-profile-request"])

	calls := mock.GetAuthenticatedProfile().Calls()
	require.Len(t, calls, 1)
	require.Equal(t, flowID, calls[0].FlowID)
	require.Equal(t, linkedin.GetAuthenticatedProfileInput{}, calls[0].Input)
}
