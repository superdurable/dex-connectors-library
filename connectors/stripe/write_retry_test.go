// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package stripe_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/stripe"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

type stripeWriteCase struct {
	name          string
	path          string
	objectID      string
	responseBody  string
	successBranch sdkgo.BranchID
	invoke        func(*stripe.Client, *stripeDexContext) (sdkgo.BranchID, string, error)
}

func stripeWriteCases() []stripeWriteCase {
	return []stripeWriteCase{
		{
			name: "card checkout", path: "/checkout/sessions", objectID: "cs_test_card_replayed",
			responseBody:  `{"id":"cs_test_card_replayed","object":"checkout.session","status":"open"}`,
			successBranch: stripe.CreateCardCheckoutSessionBranchCreated,
			invoke: func(client *stripe.Client, ctx *stripeDexContext) (sdkgo.BranchID, string, error) {
				result, err := sdkgo.RunMutation(ctx, client.CreateCardCheckoutSession(), stripeConnection, stripe.CreateCardCheckoutSessionInput(validCreateInput()))
				return result.Branch, result.Value.ID, err
			},
		},
		{
			name: "ACH checkout", path: "/checkout/sessions", objectID: "cs_test_ach_replayed",
			responseBody:  `{"id":"cs_test_ach_replayed","object":"checkout.session","status":"open"}`,
			successBranch: stripe.CreateACHCheckoutSessionBranchCreated,
			invoke: func(client *stripe.Client, ctx *stripeDexContext) (sdkgo.BranchID, string, error) {
				result, err := sdkgo.RunMutation(ctx, client.CreateACHCheckoutSession(), stripeConnection, validCreateInput())
				return result.Branch, result.Value.ID, err
			},
		},
		{
			name: "refund", path: "/refunds", objectID: "re_replayed",
			responseBody:  `{"id":"re_replayed","object":"refund","status":"pending","amount":2500,"currency":"usd"}`,
			successBranch: stripe.CreateRefundBranchRefunded,
			invoke: func(client *stripe.Client, ctx *stripeDexContext) (sdkgo.BranchID, string, error) {
				result, err := sdkgo.RunMutation(ctx, client.CreateRefund(), stripeConnection, validRefundInput())
				return result.Branch, result.Value.RefundID, err
			},
		},
	}
}

func TestStripeKeyedWritesRetryBeforeDispatch(t *testing.T) {
	for _, write := range stripeWriteCases() {
		t.Run(write.name, func(t *testing.T) {
			transport := &stripeBeforeDispatchFailureTransport{}
			client := newStripeClient(t, "http://127.0.0.1:1", stripe.Config{}, stripe.WithHTTPClient(&http.Client{Transport: transport}))
			_, _, err := write.invoke(client, newStripeDexContext("before-dispatch"))
			requireStripeRetry(t, err, sdkgo.FailureTransport)
			require.Equal(t, 1, transport.calls)
			require.NotEmpty(t, transport.key)
		})
	}
}

func TestStripeKeyedWritesRetryLostResponseWithOriginalObject(t *testing.T) {
	for _, write := range stripeWriteCases() {
		t.Run(write.name, func(t *testing.T) {
			provider := &stripeWriteProvider{
				path: write.path, objectID: write.objectID, responseBody: write.responseBody,
				objectsByKey: make(map[string]string),
			}
			server := httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
			defer server.Close()
			baseTransport := http.DefaultTransport.(*http.Transport).Clone()
			defer baseTransport.CloseIdleConnections()
			transport := &stripeDropFirstResponseTransport{base: baseTransport}
			client := newStripeClient(t, server.URL, stripe.Config{}, stripe.WithHTTPClient(&http.Client{Transport: transport}))
			ctx := newStripeDexContext("lost-response")
			_, _, err := write.invoke(client, ctx)
			requireStripeRetry(t, err, sdkgo.FailureTransport)
			branch, objectID, err := write.invoke(client, ctx)
			require.NoError(t, err)
			require.Equal(t, write.successBranch, branch)
			require.Equal(t, write.objectID, objectID)
			provider.mutex.Lock()
			defer provider.mutex.Unlock()
			require.Len(t, provider.keys, 2)
			require.NotEmpty(t, provider.keys[0])
			require.Equal(t, provider.keys[0], provider.keys[1])
			require.Len(t, provider.objectsByKey, 1)
		})
	}
}

func TestStripeKeyedWritesRetryUnreadableResponsesAndServerFailures(t *testing.T) {
	for _, write := range stripeWriteCases() {
		t.Run(write.name+" unreadable response", func(t *testing.T) {
			transport := stripeUnreadableResponseTransport{}
			client := newStripeClient(t, "http://127.0.0.1:1", stripe.Config{}, stripe.WithHTTPClient(&http.Client{Transport: transport}))
			_, _, err := write.invoke(client, newStripeDexContext("unreadable-response"))
			requireStripeRetry(t, err, sdkgo.FailureTransport)
		})
		t.Run(write.name+" 503", func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				require.Equal(t, write.path, request.URL.Path)
				require.NotEmpty(t, request.Header.Get("Idempotency-Key"))
				response.WriteHeader(http.StatusServiceUnavailable)
			}))
			defer server.Close()
			client := newStripeClient(t, server.URL, stripe.Config{})
			_, _, err := write.invoke(client, newStripeDexContext("server-failure"))
			requireStripeRetry(t, err, sdkgo.FailureAvailability)
		})
	}
}

func TestStripeKeyedWritesRetryOversizedResponses(t *testing.T) {
	for _, write := range stripeWriteCases() {
		t.Run(write.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				if _, err := io.WriteString(response, strings.Repeat("x", 65)); err != nil {
					panic(err)
				}
			}))
			defer server.Close()
			client := newStripeClient(t, server.URL, stripe.Config{MaxResponseBytes: 64})
			_, _, err := write.invoke(client, newStripeDexContext("oversized-response"))
			requireStripeRetry(t, err, sdkgo.FailureResponseTooLarge)
		})
	}
}

func TestCreateRefundStillRetriesIdempotencyConflict(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/refunds", request.URL.Path)
		response.WriteHeader(http.StatusConflict)
	}))
	defer server.Close()
	client := newStripeClient(t, server.URL, stripe.Config{})
	_, _, err := stripeWriteCases()[2].invoke(client, newStripeDexContext("refund-conflict"))
	requireStripeRetry(t, err, sdkgo.FailureConflict)
}

type stripeBeforeDispatchFailureTransport struct {
	calls int
	key   string
}

func (transport *stripeBeforeDispatchFailureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.calls++
	transport.key = request.Header.Get("Idempotency-Key")
	return nil, io.ErrClosedPipe
}

type stripeWriteProvider struct {
	mutex        sync.Mutex
	path         string
	objectID     string
	responseBody string
	keys         []string
	objectsByKey map[string]string
}

func (provider *stripeWriteProvider) serveHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost || request.URL.Path != provider.path {
		http.NotFound(response, request)
		return
	}
	key := request.Header.Get("Idempotency-Key")
	provider.mutex.Lock()
	provider.keys = append(provider.keys, key)
	if _, exists := provider.objectsByKey[key]; !exists {
		provider.objectsByKey[key] = provider.objectID
	}
	provider.mutex.Unlock()
	if _, err := io.WriteString(response, provider.responseBody); err != nil {
		panic(err)
	}
}

type stripeDropFirstResponseTransport struct {
	base    http.RoundTripper
	dropped atomic.Bool
}

func (transport *stripeDropFirstResponseTransport) RoundTrip(request *http.Request) (*http.Response, error) {
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

type stripeUnreadableResponseTransport struct{}

func (stripeUnreadableResponseTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(stripeReadError{}), Request: request,
	}, nil
}

type stripeReadError struct{}

func (stripeReadError) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func requireStripeRetry(t *testing.T, err error, kind sdkgo.FailureKind) {
	t.Helper()
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, kind, retry.Failure.Kind)
}
