//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package welcomeemail

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/resend"
	"github.com/superdurable/dex-connectors-library/connectors/resend/resendmock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestMockBackedFlowWithRealDex(t *testing.T) {
	mock := resendmock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	require.NoError(t, listener.Close())
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{BindAddress: address, FlowServiceAddress: os.Getenv("DEX_FLOW_SERVICE_ADDRESS")})
	require.NoError(t, err)
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: os.Getenv("DEX_FLOW_SERVICE_ADDRESS"), WorkerTarget: &dex.WorkerTarget{Address: address}})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	t.Cleanup(func() {
		stopContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(stopContext), <-workerResult, client.Close(), cache.Close()))
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := "resend-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	input := Request(resend.SendEmailInput{From: "Acme <hello@example.test>", To: []string{"reader@example.test"}, Subject: "Welcome", HTML: "<p>Hello</p>"})
	_, err = client.StartFlow(ctx, flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	var result dex.FlowResult
	for {
		result, err = client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
		var timeout *dex.LongPollTimeoutError
		if errors.As(err, &timeout) {
			continue
		}
		require.NoError(t, err)
		break
	}
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output resend.SentEmail
	require.NoError(t, result.DecodeSingleOutput(&output))
	var display map[string]any
	require.NoError(t, client.InvokeRPC(ctx, flowID, flow.GetDexDisplay, nil, &display))
	encodedOutcome, err := json.Marshal(display["resend-welcome-email-outcome"])
	require.NoError(t, err)
	var displayedOutcome resend.SentEmail
	require.NoError(t, json.Unmarshal(encodedOutcome, &displayedOutcome))
	require.Equal(t, output, displayedOutcome)
	calls := mock.SendEmail().Calls()
	require.Len(t, calls, 1)
	require.Equal(t, flowID, calls[0].FlowID)
	require.Equal(t, resend.SendEmailInput(input), calls[0].Input)
	require.Equal(t, resend.SendEmailBranchSent, calls[0].Branch)
}
