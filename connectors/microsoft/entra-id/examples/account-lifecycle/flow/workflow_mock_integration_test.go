//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package accountlifecycle

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	entraid "github.com/superdurable/dex-connectors-library/connectors/microsoft/entra-id"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/entra-id/entraidmock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestAccountLifecycleExampleRunsOnEntraIDMockWithRealDex(t *testing.T) {
	const adaID = "11111111-2222-4333-8444-555555555555"
	mock := entraidmock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	serverAddress := environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")
	harness := &accountLifecycleHarness{cache: cache}
	harness.client, err = dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: serverAddress,
	})
	require.NoError(t, err)
	harness.worker, err = dex.NewWorker(registry, cache, dex.WorkerOptions{FlowServiceAddress: serverAddress})
	require.NoError(t, err)
	harness.workerResult = make(chan error, 1)
	go func() { harness.workerResult <- harness.worker.Start() }()
	require.NoError(t, harness.worker.WaitPullRegistered(t.Context()))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(harness.worker.Stop(ctx), <-harness.workerResult, harness.client.Close(), harness.cache.Close()))
	})
	ctx := integrationContext(t)

	onboardID := startAccountLifecycle(t, ctx, harness.client, flow, "mock-onboard", onboardingInput())
	onboarded := waitForCompletedHandOff(t, ctx, harness.client, onboardID)
	require.Equal(t, HandOffOnboarded, onboarded.Status)
	require.Equal(t, "ada.lovelace@contoso.com", onboarded.UserPrincipalName)
	require.Equal(t, adaID, onboarded.UserID)
	require.True(t, onboarded.IsAccountEnabled)
	require.Equal(t, testGroupID, onboarded.GroupID)
	require.False(t, onboarded.WasAlreadyCreated)
	creates := mock.CreateUser().Calls()
	require.Len(t, creates, 1)
	require.Equal(t, onboardID, creates[0].FlowID)
	require.Equal(t, entraid.CreateUserInput{
		UserPrincipalName: "ada.lovelace@contoso.com", GivenName: "Ada", Surname: "Lovelace",
		Department: "Engineering", UsageLocation: "GB",
	}, creates[0].Input)

	offboardID := startAccountLifecycle(t, ctx, harness.client, flow, "mock-offboard", Input{
		Action: AccountActionOffboard, UserPrincipalName: "ada.lovelace@contoso.com", GroupID: testGroupID,
	})
	offboarded := waitForCompletedHandOff(t, ctx, harness.client, offboardID)
	require.Equal(t, HandOffOffboarded, offboarded.Status)
	require.False(t, offboarded.IsAccountEnabled)
	require.True(t, offboarded.AreSessionsRevoked)
	reads := mock.GetUser().Calls()
	require.Len(t, reads, 1)
	require.Equal(t, offboardID, reads[0].FlowID)
	require.Equal(t, entraid.GetUserInput{UserKey: "ada.lovelace@contoso.com"}, reads[0].Input)
	disables := mock.DisableUser().Calls()
	require.Len(t, disables, 1)
	require.Equal(t, entraid.DisableUserInput{UserKey: adaID}, disables[0].Input)
	revocations := mock.RevokeSignInSessions().Calls()
	require.Len(t, revocations, 1)
	require.Equal(t, entraid.RevokeSignInSessionsInput{UserKey: adaID}, revocations[0].Input)
	removals := mock.RemoveUserFromGroup().Calls()
	require.Len(t, removals, 1)
	require.Equal(t, entraid.RemoveUserFromGroupInput{GroupID: testGroupID, UserID: adaID}, removals[0].Input)

	reinstateID := startAccountLifecycle(t, ctx, harness.client, flow, "mock-reinstate", Input{
		Action: AccountActionReinstate, UserPrincipalName: "ada.lovelace@contoso.com", GroupID: testGroupID,
	})
	reinstated := waitForCompletedHandOff(t, ctx, harness.client, reinstateID)
	require.Equal(t, HandOffReinstated, reinstated.Status)
	require.True(t, reinstated.IsAccountEnabled)
	enables := mock.EnableUser().Calls()
	require.Len(t, enables, 1)
	require.Equal(t, entraid.EnableUserInput{UserKey: "ada.lovelace@contoso.com"}, enables[0].Input)
	adds := mock.AddUserToGroup().Calls()
	require.Len(t, adds, 2)
	require.Equal(t, onboardID, adds[0].FlowID)
	require.Equal(t, reinstateID, adds[1].FlowID)
	for _, call := range adds {
		require.Equal(t, entraid.AddUserToGroupInput{GroupID: testGroupID, UserID: adaID}, call.Input)
	}
	require.Empty(t, mock.ListUsers().Calls(), "the lifecycle Flow never lists all tenant accounts")
}
