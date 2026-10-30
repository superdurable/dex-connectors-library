//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package refundpayment

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
	"github.com/superdurable/dex-connectors-library/connectors/stripe/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

// The refund retry after a lost response can reach Stripe while the original request still executes.
func TestRefundRetriesKeyStillInUseWithRealDex(t *testing.T) {
	provider := &stripeRefundProvider{refundsByKey: make(map[string]string)}
	server := httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(server.Close)
	baseTransport := http.DefaultTransport.(*http.Transport).Clone()
	t.Cleanup(baseTransport.CloseIdleConnections)
	transport := &lostStripeRefundResponseTransport{base: baseTransport}
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
	flowID := "stripe-refund-key-in-use-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, Input{PaymentIntentID: "pi_123", Reason: "requested_by_customer"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var outcome Outcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	require.True(t, outcome.IsRefundCreated)
	require.Equal(t, "re_replayed", outcome.Refund.RefundID)
	require.Equal(t, int64(5000), outcome.Refund.Amount)
	require.True(t, transport.dropped.Load())
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	require.Len(t, provider.keys, 3)
	require.NotEmpty(t, provider.keys[0])
	require.Equal(t, provider.keys[0], provider.keys[1])
	require.Equal(t, provider.keys[0], provider.keys[2])
	require.Len(t, provider.refundsByKey, 1)
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
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(stopCtx), <-workerResult))
	})
	{
		registrationContext, cancelRegistration := context.WithTimeout(t.Context(), 30*time.Second)
		registrationError := worker.WaitPullRegistered(registrationContext)
		cancelRegistration()
		require.NoError(t, registrationError)
	}
	return client
}

// stripeRefundProvider answers the second keyed refund as if the first were still executing.
type stripeRefundProvider struct {
	mutex        sync.Mutex
	keys         []string
	refundsByKey map[string]string
}

func (provider *stripeRefundProvider) serveHTTP(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Content-Type", "application/json")
	if request.Method == http.MethodGet && request.URL.Path == "/payment_intents/pi_123" {
		writeStripeBody(response, `{"id":"pi_123","object":"payment_intent","amount":7500,"currency":"usd","status":"succeeded","latest_charge":{"id":"ch_123","amount_refunded":2500}}`)
		return
	}
	if request.Method != http.MethodPost || request.URL.Path != "/refunds" {
		http.NotFound(response, request)
		return
	}
	if err := request.ParseForm(); err != nil || request.PostForm.Get("amount") != "5000" {
		http.Error(response, `{"error":{"type":"invalid_request_error"}}`, http.StatusBadRequest)
		return
	}
	key := request.Header.Get("Idempotency-Key")
	provider.mutex.Lock()
	provider.keys = append(provider.keys, key)
	if _, exists := provider.refundsByKey[key]; !exists {
		provider.refundsByKey[key] = "re_replayed"
	}
	isKeyInUse := len(provider.keys) == 2
	provider.mutex.Unlock()
	if isKeyInUse {
		response.WriteHeader(http.StatusConflict)
		writeStripeBody(response, `{"error":{"code":"idempotency_key_in_use","message":"There is currently another in-progress request using this idempotency key."}}`)
		return
	}
	writeStripeBody(response, `{"id":"re_replayed","object":"refund","status":"succeeded","amount":5000,"currency":"usd"}`)
}

func writeStripeBody(response http.ResponseWriter, body string) {
	if _, err := io.WriteString(response, body); err != nil {
		panic(err)
	}
}

// lostStripeRefundResponseTransport loses the first refund response after Stripe received the request.
type lostStripeRefundResponseTransport struct {
	base    http.RoundTripper
	dropped atomic.Bool
}

func (transport *lostStripeRefundResponseTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := transport.base.RoundTrip(request)
	if err != nil {
		return nil, err
	}
	if request.Method == http.MethodPost && transport.dropped.CompareAndSwap(false, true) {
		if err := response.Body.Close(); err != nil {
			return nil, err
		}
		return nil, io.ErrUnexpectedEOF
	}
	return response, nil
}
