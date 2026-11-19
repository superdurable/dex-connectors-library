//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package websearch

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/bocha/go"
	"github.com/superdurable/dex-connectors-library/connectors/bocha/go/bochamock"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestSearchWebUsesDefaultMockWithRealDex(t *testing.T) {
	mock := bochamock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	serverAddress := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	if serverAddress == "" {
		serverAddress = "127.0.0.1:10061"
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
	flowID := "bocha-search-web-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, bocha.SearchWebInput{Query: "example"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output bocha.SearchWebOutput
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, bocha.SearchWebOutput{Results: []bocha.WebResult{{Title: "Example page", URL: "https://example.com/article", Snippet: "Example search excerpt."}}}, output)

	require.Len(t, mock.SearchWeb().Calls(), 1)
}

type providerRoundTrip func(*http.Request) (*http.Response, error)

func (send providerRoundTrip) RoundTrip(request *http.Request) (*http.Response, error) {
	return send(request)
}

func TestSearchWebUsesCurrentOfficialEndpointWithRealDex(t *testing.T) {
	requestCount := 0
	providerTransport := providerRoundTrip(func(request *http.Request) (*http.Response, error) {
		requestCount++
		require.Equal(t, "https://api.bochaai.com/v1/web-search", request.URL.String())
		return &http.Response{
			StatusCode: 200,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(
				`{"code":200,"data":{"webPages":{"value":[{"name":"Example","url":"https://example.com/a","snippet":"A snippet"}]}}}`,
			)),
		}, nil
	})
	reference := sdkgo.ConnectionRef{Provider: "bocha", Name: ConnectionName}
	credentials := sdkgo.StaticCredentialProvider[bocha.Credentials]{
		reference: {APIKey: sdkgo.NewSecretString("synthetic-bocha-key")},
	}
	provider, err := bocha.New(bocha.Config{}, credentials, bocha.WithHTTPClient(&http.Client{Transport: providerTransport}))
	require.NoError(t, err)
	connection, err := bocha.NewConnection(provider, reference)
	require.NoError(t, err)
	flow := NewFlow(connection)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	serverAddress := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	if serverAddress == "" {
		serverAddress = "127.0.0.1:10061"
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
	flowID := "bocha-search-web-provider-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, bocha.SearchWebInput{Query: "example"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output bocha.SearchWebOutput
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, bocha.SearchWebOutput{Results: []bocha.WebResult{{Title: "Example", URL: "https://example.com/a", Snippet: "A snippet"}}}, output)
	require.Equal(t, 1, requestCount)
}
