//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sendmessage

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/telegram/go"
	"github.com/superdurable/dex-connectors-library/connectors/telegram/go/telegrammock"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestMessageSendUsesDefaultMockWithRealDex(t *testing.T) {
	mock := telegrammock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	editFlow := NewEditMessageFlow(mock.Connection())
	photoFlow := NewSendPhotoFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow, editFlow, photoFlow})
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
	flowID := "telegram-message-send-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, telegram.SendMessageInput{ChatID: "@meridian_team", Text: "Hello from Dex"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output telegram.SentMessage
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, int64(52), output.MessageID)
	require.Len(t, mock.SendMessage().Calls(), 1)
	editFlowID := "telegram-message-edit-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, editFlow, editFlowID, telegram.EditMessageTextInput{ChatID: "@meridian_team", MessageID: 52, Text: "Edited"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	editResult, err := client.WaitForFlow(ctx, editFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, editResult.Status, editResult.ErrorMessage)
	var edited telegram.MessageActionResult
	require.NoError(t, editResult.DecodeSingleOutput(&edited))
	require.Equal(t, int64(52), edited.MessageID)
	require.Len(t, mock.EditMessageText().Calls(), 1)
	photoFlowID := "telegram-photo-send-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, photoFlow, photoFlowID, telegram.SendPhotoInput{ChatID: "@meridian_team", Media: "https://example.com/photo.jpg", Caption: "Photo"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	photoResult, err := client.WaitForFlow(ctx, photoFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, photoResult.Status, photoResult.ErrorMessage)
	var sentPhoto telegram.SentMessage
	require.NoError(t, photoResult.DecodeSingleOutput(&sentPhoto))
	require.EqualValues(t, 53, sentPhoto.MessageID)
	require.Len(t, mock.SendPhoto().Calls(), 1)
}

func TestMessageSendRetriesRateLimitWithRealDex(t *testing.T) {
	requests := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, err := fmt.Fprint(w, `{"ok":true,"result":{"message_id":53,"date":1794110001,"chat":{"id":-1001234567890}}}`)
		require.NoError(t, err)
	}))
	defer provider.Close()
	reference := sdkgo.ConnectionRef{Provider: "telegram", Name: ConnectionName}
	providerClient, err := telegram.New(telegram.Config{BaseURL: provider.URL}, sdkgo.StaticCredentialProvider[telegram.Credentials]{
		reference: {BotToken: sdkgo.NewSecretString("123456:fake_test_token")},
	})
	require.NoError(t, err)
	connection, err := telegram.NewConnection(providerClient, reference)
	require.NoError(t, err)
	flow := NewFlow(connection)
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
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(stopCtx), <-workerResult, client.Close(), cache.Close()))
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	require.NoError(t, worker.WaitPullRegistered(ctx))
	flowID := "telegram-message-send-retry-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, telegram.SendMessageInput{ChatID: "@meridian_team", Text: "Hello from Dex"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output telegram.SentMessage
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.EqualValues(t, 53, output.MessageID)
	require.Equal(t, 2, requests)
}
