//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package createcheckoutsession

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/stripe"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

// The retry after a lost response can reach Stripe while the original request still executes.
func TestACHCheckoutRetriesKeyStillInUseWithRealDex(t *testing.T) {
	provider := &stripeACHCheckoutProvider{objectsByKey: make(map[string]string)}
	server := httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(server.Close)
	baseTransport := http.DefaultTransport.(*http.Transport).Clone()
	t.Cleanup(baseTransport.CloseIdleConnections)
	transport := &lostStripeResponseTransport{base: baseTransport}
	reference := sdkgo.ConnectionRef{Provider: "stripe", Name: ConnectionName}
	providerClient, err := stripe.New(stripe.Config{Endpoint: server.URL}, sdkgo.StaticCredentialProvider[stripe.Credentials]{
		reference: {SecretKey: sdkgo.NewSecretString("sk_test_example")},
	}, stripe.WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	connection, err := stripe.NewConnection(providerClient, reference)
	require.NoError(t, err)
	flow := NewFlow(connection)
	client := startRealDexWorker(t, flow)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := "stripe-ach-key-in-use-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, Input{
		ClientReferenceID: "registration-123", CustomerEmail: "attendee@example.com",
		SuccessURL: "https://events.example.com/success", CancelURL: "https://events.example.com/cancel",
		Currency: "usd", UnitAmount: 7500, ProductName: "Community Event",
	}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var session stripe.CheckoutSession
	require.NoError(t, result.DecodeSingleOutput(&session))
	require.Equal(t, "cs_test_ach_replayed", session.ID)
	require.True(t, transport.dropped.Load())
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	require.Len(t, provider.keys, 3)
	require.NotEmpty(t, provider.keys[0])
	require.Equal(t, provider.keys[0], provider.keys[1])
	require.Equal(t, provider.keys[0], provider.keys[2])
	require.Len(t, provider.objectsByKey, 1)
}

func startRealDexWorker(t *testing.T, flow dex.Flow) *dex.Client {
	t.Helper()
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cache.Close()) })
	serverAddress := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	if serverAddress == "" {
		serverAddress = "127.0.0.1:8801"
	}
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: serverAddress,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{FlowServiceAddress: serverAddress})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	require.NoError(t, worker.WaitPullRegistered(t.Context()))
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(stopCtx), <-workerResult))
	})
	return client
}

// stripeACHCheckoutProvider answers the second keyed request as if the first were still executing.
type stripeACHCheckoutProvider struct {
	mutex        sync.Mutex
	keys         []string
	objectsByKey map[string]string
}

func (provider *stripeACHCheckoutProvider) serveHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost || request.URL.Path != "/checkout/sessions" {
		http.NotFound(response, request)
		return
	}
	key := request.Header.Get("Idempotency-Key")
	provider.mutex.Lock()
	provider.keys = append(provider.keys, key)
	if _, exists := provider.objectsByKey[key]; !exists {
		provider.objectsByKey[key] = "cs_test_ach_replayed"
	}
	isKeyInUse := len(provider.keys) == 2
	provider.mutex.Unlock()
	response.Header().Set("Content-Type", "application/json")
	body := `{"id":"cs_test_ach_replayed","object":"checkout.session","status":"open"}`
	if isKeyInUse {
		response.WriteHeader(http.StatusConflict)
		body = `{"error":{"code":"idempotency_key_in_use","message":"There is currently another in-progress request using this idempotency key."}}`
	}
	if _, err := io.WriteString(response, body); err != nil {
		panic(err)
	}
}

type lostStripeResponseTransport struct {
	base    http.RoundTripper
	dropped atomic.Bool
}

func (transport *lostStripeResponseTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := transport.base.RoundTrip(request)
	if err != nil {
		return nil, err
	}
	if transport.dropped.CompareAndSwap(false, true) {
		if err := response.Body.Close(); err != nil {
			return nil, err
		}
		return nil, io.ErrUnexpectedEOF
	}
	return response, nil
}
