//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package texttranslation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/youdao/go"
	"github.com/superdurable/dex-connectors-library/connectors/youdao/go/youdaomock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestTextTranslationUsesDefaultMockWithRealDex(t *testing.T) {
	mock := youdaomock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
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
	flowID := "youdao-texttranslation-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, youdao.TranslateTextInput{Text: "Hello", SourceLanguage: "en", TargetLanguage: "zh-CHS"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output youdao.TextTranslation
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, []string{"你好"}, output.Translation)
	require.Len(t, mock.TranslateText().Calls(), 1)
	require.Equal(t, youdao.TranslateTextInput{Text: "Hello", SourceLanguage: "en", TargetLanguage: "zh-CHS"}, mock.TranslateText().Calls()[0].Input)
	require.Equal(t, "en", output.SourceLanguage)
	require.Equal(t, "zh-CHS", output.TargetLanguage)
}
