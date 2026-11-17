//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package resourceread

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/bitbucket/go"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/bitbucket/go/bitbucketmock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestResourceReadUsesManifestMockWithRealDex(t *testing.T) {
	mock := bitbucketmock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	serverAddress := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	if serverAddress == "" {
		serverAddress = "127.0.0.1:8801"
	}
	workerOptions := dex.WorkerOptions{FlowServiceAddress: serverAddress}
	worker, err := dex.NewWorker(registry, cache, workerOptions)
	require.NoError(t, err)
	clientOptions := dex.ClientOptions{FlowServiceAddress: serverAddress}
	client, err := dex.NewClient(registry, cache, clientOptions)
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(ctx), <-workerResult, client.Close(), cache.Close()))
	})
	{
		registrationContext, cancelRegistration := context.WithTimeout(t.Context(), 30*time.Second)
		registrationError := worker.WaitPullRegistered(registrationContext)
		cancelRegistration()
		require.NoError(t, registrationError)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := "bitbucket-resource-read-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	input := Request{Workspace: "meridian"}
	_, err = client.StartFlow(ctx, flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var outcome Outcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	require.NotEmpty(t, outcome.Value.Values)
	calls := mock.ListRepositories().Calls()
	require.Len(t, calls, 1)
	require.Equal(t, flowID, calls[0].FlowID)
	require.Equal(t, bitbucket.ListRepositoriesInput{Workspace: "meridian"}, calls[0].Input)
	require.Equal(t, bitbucket.ListRepositoriesBranchListed, calls[0].Branch)
}

func TestRepositoryCreateUsesManifestMockWithRealDex(t *testing.T) {
	mock := bitbucketmock.New(t, ConnectionName)
	flow := NewRepositoryCreateFlow(mock.Connection())
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
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(ctx), <-workerResult, client.Close(), cache.Close()))
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	require.NoError(t, worker.WaitPullRegistered(ctx))
	flowID := "bitbucket-repository-create-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	isPrivate := true
	input := bitbucket.CreateRepositoryInput{Workspace: "meridian", Repository: "api", Name: "Meridian API", IsPrivate: &isPrivate}
	_, err = client.StartFlow(ctx, flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output bitbucket.Repository
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, input.Workspace+"/"+input.Repository, output.FullName)
	require.Equal(t, input.Name, output.Name)
	calls := mock.CreateRepository().Calls()
	require.Len(t, calls, 1)
	require.Equal(t, input, calls[0].Input)
	require.Equal(t, bitbucket.CreateRepositoryBranchCreated, calls[0].Branch)
}
func TestPullRequestTitleUsesManifestMockWithRealDex(t *testing.T) {
	mock := bitbucketmock.New(t, ConnectionName)
	flow := NewPullRequestTitleFlow(mock.Connection())
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
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(ctx), <-workerResult, client.Close(), cache.Close()))
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	require.NoError(t, worker.WaitPullRegistered(ctx))
	flowID := "bitbucket-title-update-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	input := bitbucket.UpdatePullRequestTitleInput{Workspace: "meridian", Repository: "api", PullRequestID: 7, Title: "Add client and contract tests"}
	_, err = client.StartFlow(ctx, flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output bitbucket.PullRequest
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, input.PullRequestID, output.ID)
	require.Equal(t, input.Title, output.Title)
	calls := mock.UpdatePullRequestTitle().Calls()
	require.Len(t, calls, 1)
	require.Equal(t, input, calls[0].Input)
	require.Equal(t, bitbucket.UpdatePullRequestTitleBranchUpdated, calls[0].Branch)
}
