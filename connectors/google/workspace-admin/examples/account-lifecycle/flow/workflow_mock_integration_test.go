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
	workspaceadmin "github.com/superdurable/dex-connectors-library/connectors/google/workspace-admin"
	"github.com/superdurable/dex-connectors-library/connectors/google/workspace-admin/workspaceadminmock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestAccountLifecycleUsesWorkspaceAdminMockWithRealDex(t *testing.T) {
	mock := workspaceadminmock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	serverAddress := environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: serverAddress,
	})
	require.NoError(t, err)
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{FlowServiceAddress: serverAddress})
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
	ctx := integrationContext(t)
	onboardID := startAccountLifecycle(t, ctx, client, flow, "mock-onboard", Input{
		Action: AccountActionOnboard, PrimaryEmail: " ADA@example.com ", GivenName: "Ada", FamilyName: "Lovelace",
		OrgUnitPath: "/Engineering", GroupEmail: "engineering@example.com",
	})
	onboarded := waitForCompletedHandOff(t, ctx, client, onboardID)
	require.Equal(t, HandOffOnboarded, onboarded.Status)
	require.Equal(t, "ada@example.com", onboarded.PrimaryEmail)
	require.False(t, onboarded.IsSuspended)
	require.Equal(t, workspaceadmin.GroupRoleMember, onboarded.GroupRole)
	require.Equal(t, newAccountSignInHandOff, onboarded.NextStep)
	createCalls := mock.CreateUser().Calls()
	require.Len(t, createCalls, 1)
	require.Equal(t, workspaceadmin.CreateUserInput{
		PrimaryEmail: "ada@example.com", GivenName: "Ada", FamilyName: "Lovelace", OrgUnitPath: "/Engineering",
	}, createCalls[0].Input)
	addCalls := mock.AddUserToGroup().Calls()
	require.Len(t, addCalls, 1)
	require.Equal(t, workspaceadmin.AddUserToGroupInput{
		GroupKey: "engineering@example.com", MemberEmail: "ada@example.com", Role: workspaceadmin.GroupRoleMember,
	}, addCalls[0].Input)
	getCalls := mock.GetUser().Calls()
	require.Len(t, getCalls, 1)
	require.Equal(t, workspaceadmin.GetUserInput{UserKey: "1001"}, getCalls[0].Input)

	offboardID := startAccountLifecycle(t, ctx, client, flow, "mock-offboard", Input{
		Action: AccountActionOffboard, PrimaryEmail: "ada@example.com", GroupEmail: "engineering@example.com",
	})
	offboarded := waitForCompletedHandOff(t, ctx, client, offboardID)
	require.Equal(t, HandOffOffboarded, offboarded.Status)
	require.True(t, offboarded.IsSuspended)
	require.Equal(t, offboardedAccountHandOff, offboarded.NextStep)
	suspendCalls := mock.SuspendUser().Calls()
	require.Len(t, suspendCalls, 1)
	require.Equal(t, workspaceadmin.SuspendUserInput{UserKey: "ada@example.com"}, suspendCalls[0].Input)
	removeCalls := mock.RemoveUserFromGroup().Calls()
	require.Len(t, removeCalls, 1)
	require.Equal(t, workspaceadmin.RemoveUserFromGroupInput{
		GroupKey: "engineering@example.com", MemberKey: "1001",
	}, removeCalls[0].Input)
	require.Len(t, mock.CreateUser().Calls(), 1)
	require.Len(t, mock.AddUserToGroup().Calls(), 1)
	require.Len(t, mock.GetUser().Calls(), 1)
	require.Empty(t, mock.UnsuspendUser().Calls())
	require.Empty(t, mock.ListUsers().Calls())
}
