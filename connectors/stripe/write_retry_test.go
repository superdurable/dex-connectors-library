// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package stripe_test

import (
	"fmt"
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
	name                   string
	path                   string
	objectID               string
	responseBody           string
	successBranch          sdkgo.BranchID
	providerRejectedBranch sdkgo.BranchID
	invalidResponseBranch  sdkgo.BranchID
	invoke                 func(*stripe.Client, *stripeDexContext) (stripeWriteOutcome, error)
}

type stripeWriteOutcome struct {
	branch   sdkgo.BranchID
	objectID string
	failure  *sdkgo.Failure
}

func stripeWriteCases() []stripeWriteCase {
	return []stripeWriteCase{
		{
			name: "card checkout", path: "/checkout/sessions", objectID: "cs_test_card_replayed",
			responseBody:           `{"id":"cs_test_card_replayed","object":"checkout.session","status":"open"}`,
			successBranch:          stripe.CreateCardCheckoutSessionBranchCreated,
			providerRejectedBranch: stripe.CreateCardCheckoutSessionBranchProviderRejected,
			invalidResponseBranch:  stripe.CreateCardCheckoutSessionBranchInvalidResponse,
			invoke: func(client *stripe.Client, ctx *stripeDexContext) (stripeWriteOutcome, error) {
				result, err := sdkgo.RunMutation(ctx, client.CreateCardCheckoutSession(), stripeConnection, stripe.CreateCardCheckoutSessionInput(validCreateInput()))
				return stripeWriteOutcome{result.Branch, result.Value.ID, result.Failure}, err
			},
		},
		{
			name: "ACH checkout", path: "/checkout/sessions", objectID: "cs_test_ach_replayed",
			responseBody:           `{"id":"cs_test_ach_replayed","object":"checkout.session","status":"open"}`,
			successBranch:          stripe.CreateACHCheckoutSessionBranchCreated,
			providerRejectedBranch: stripe.CreateACHCheckoutSessionBranchProviderRejected,
			invalidResponseBranch:  stripe.CreateACHCheckoutSessionBranchInvalidResponse,
			invoke: func(client *stripe.Client, ctx *stripeDexContext) (stripeWriteOutcome, error) {
				result, err := sdkgo.RunMutation(ctx, client.CreateACHCheckoutSession(), stripeConnection, validCreateInput())
				return stripeWriteOutcome{result.Branch, result.Value.ID, result.Failure}, err
			},
		},
		{
			name: "refund", path: "/refunds", objectID: "re_replayed",
			responseBody:           `{"id":"re_replayed","object":"refund","status":"pending","amount":2500,"currency":"usd"}`,
			successBranch:          stripe.CreateRefundBranchRefunded,
			providerRejectedBranch: stripe.CreateRefundBranchProviderRejected,
			invalidResponseBranch:  stripe.CreateRefundBranchInvalidResponse,
			invoke: func(client *stripe.Client, ctx *stripeDexContext) (stripeWriteOutcome, error) {
				result, err := sdkgo.RunMutation(ctx, client.CreateRefund(), stripeConnection, validRefundInput())
				return stripeWriteOutcome{result.Branch, result.Value.RefundID, result.Failure}, err
			},
		},
	}
}

func TestStripeKeyedWritesRetryBeforeDispatch(t *testing.T) {
	for _, write := range stripeWriteCases() {
		t.Run(write.name, func(t *testing.T) {
			transport := &stripeBeforeDispatchFailureTransport{}
			client := newStripeClient(t, "http://127.0.0.1:1", stripe.Config{}, stripe.WithHTTPClient(&http.Client{Transport: transport}))
			_, err := write.invoke(client, newStripeDexContext("before-dispatch"))
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
			_, err := write.invoke(client, ctx)
			requireStripeRetry(t, err, sdkgo.FailureTransport)
			outcome, err := write.invoke(client, ctx)
			require.NoError(t, err)
			require.Equal(t, write.successBranch, outcome.branch)
			require.Equal(t, write.objectID, outcome.objectID)
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
			_, err := write.invoke(client, newStripeDexContext("unreadable-response"))
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
			_, err := write.invoke(client, newStripeDexContext("server-failure"))
			requireStripeRetry(t, err, sdkgo.FailureAvailability)
		})
	}
}

func TestStripeCheckoutServerFailureHeaders(t *testing.T) {
	testCases := []struct {
		name    string
		status  int
		headers http.Header
		isRetry bool
	}{
		{name: "500", status: 500},
		{name: "500 with retry hint", status: 500, headers: http.Header{"Stripe-Should-Retry": {"true"}}, isRetry: true},
		{name: "500 replayed with retry hint", status: 500, headers: http.Header{"Stripe-Should-Retry": {"true"}, "Idempotent-Replayed": {"true"}}},
		{name: "503", status: 503, isRetry: true},
		{name: "503 with retry hint", status: 503, headers: http.Header{"Stripe-Should-Retry": {"true"}}, isRetry: true},
		{name: "503 with no retry hint", status: 503, headers: http.Header{"Stripe-Should-Retry": {"false"}}},
		{name: "503 replayed", status: 503, headers: http.Header{"Idempotent-Replayed": {"true"}}},
	}
	for _, write := range stripeWriteCases()[:2] {
		for _, testCase := range testCases {
			t.Run(write.name+" "+testCase.name, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
					require.Equal(t, write.path, request.URL.Path)
					for name, values := range testCase.headers {
						response.Header().Set(name, values[0])
					}
					response.WriteHeader(testCase.status)
				}))
				defer server.Close()
				client := newStripeClient(t, server.URL, stripe.Config{})
				outcome, err := write.invoke(client, newStripeDexContext("server-headers"))
				if testCase.isRetry {
					requireStripeRetry(t, err, sdkgo.FailureAvailability)
					return
				}
				require.NoError(t, err)
				require.Equal(t, sdkgo.UncertainBranchID, outcome.branch)
				require.Equal(t, sdkgo.FailureAvailability, outcome.failure.Kind)
			})
		}
	}
}

func TestCreateRefundReadsBackIndeterminateServerFailure(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		status      int
		headerName  string
		headerValue string
		replayed    bool
		found       bool
		isRetry     bool
	}{
		{name: "500 found", status: 500, found: true},
		{name: "500 absent", status: 500},
		{name: "500 retry hint", status: 500, headerName: "Stripe-Should-Retry", headerValue: "true", isRetry: true},
		{name: "500 replayed with retry hint", status: 500, headerName: "Stripe-Should-Retry", headerValue: "true", replayed: true, found: true},
		{name: "503 retry", status: 503, isRetry: true},
		{name: "503 explicit retry", status: 503, headerName: "Stripe-Should-Retry", headerValue: "true", isRetry: true},
		{name: "503 no retry", status: 503, headerName: "Stripe-Should-Retry", headerValue: "false", found: true},
		{name: "503 replayed", status: 503, headerName: "Idempotent-Replayed", headerValue: "true", found: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var callID string
			readCount := 0
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				require.Equal(t, "/refunds", request.URL.Path)
				if request.Method == http.MethodPost {
					require.NoError(t, request.ParseForm())
					callID = request.Form.Get("metadata[dex_call_id]")
					require.Equal(t, request.Header.Get("Idempotency-Key"), callID)
					require.NotEmpty(t, callID)
					if testCase.headerName != "" {
						response.Header().Set(testCase.headerName, testCase.headerValue)
					}
					if testCase.replayed {
						response.Header().Set("Idempotent-Replayed", "true")
					}
					response.WriteHeader(testCase.status)
					return
				}
				readCount++
				require.Equal(t, http.MethodGet, request.Method)
				require.Equal(t, "pi_123", request.URL.Query().Get("payment_intent"))
				require.Equal(t, "100", request.URL.Query().Get("limit"))
				require.Empty(t, request.Header.Get("Idempotency-Key"))
				body := `{"object":"list","data":[],"has_more":false}`
				if testCase.found {
					body = fmt.Sprintf(`{"object":"list","data":[{"id":"re_found","object":"refund","status":"pending","amount":2500,"currency":"usd","metadata":{"dex_call_id":%q}}],"has_more":false}`, callID)
				}
				_, writeErr := io.WriteString(response, body)
				require.NoError(t, writeErr)
			}))
			defer server.Close()
			client := newStripeClient(t, server.URL, stripe.Config{})
			outcome, err := stripeWriteCases()[2].invoke(client, newStripeDexContext("refund-readback"))
			if testCase.isRetry {
				requireStripeRetry(t, err, sdkgo.FailureAvailability)
				require.Zero(t, readCount)
				return
			}
			require.NoError(t, err)
			require.Equal(t, 1, readCount)
			if testCase.found {
				require.Equal(t, stripe.CreateRefundBranchRefunded, outcome.branch)
				require.Equal(t, "re_found", outcome.objectID)
			} else {
				require.Equal(t, sdkgo.UncertainBranchID, outcome.branch)
				require.Equal(t, sdkgo.FailureAvailability, outcome.failure.Kind)
			}
		})
	}
}

func TestCreateRefundReadbackPaginationIsBounded(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		findPage   int
		wantReads  int
		wantBranch sdkgo.BranchID
	}{
		{name: "found on second page", findPage: 2, wantReads: 2, wantBranch: stripe.CreateRefundBranchRefunded},
		{name: "still more after bound", findPage: 0, wantReads: 3, wantBranch: sdkgo.UncertainBranchID},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			readCount := 0
			callID := ""
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				if request.Method == http.MethodPost {
					require.NoError(t, request.ParseForm())
					callID = request.Form.Get("metadata[dex_call_id]")
					response.WriteHeader(http.StatusInternalServerError)
					return
				}
				readCount++
				require.LessOrEqual(t, readCount, 3)
				if readCount > 1 {
					require.Equal(t, fmt.Sprintf("re_page_%d", readCount-1), request.URL.Query().Get("starting_after"))
				}
				marker := "other-call"
				if readCount == testCase.findPage {
					marker = callID
				}
				_, writeErr := fmt.Fprintf(response, `{"object":"list","data":[{"id":"re_page_%d","object":"refund","status":"pending","amount":2500,"currency":"usd","metadata":{"dex_call_id":%q}}],"has_more":true}`, readCount, marker)
				require.NoError(t, writeErr)
			}))
			defer server.Close()
			client := newStripeClient(t, server.URL, stripe.Config{})
			outcome, err := stripeWriteCases()[2].invoke(client, newStripeDexContext("refund-pages"))
			require.NoError(t, err)
			require.Equal(t, testCase.wantReads, readCount)
			require.Equal(t, testCase.wantBranch, outcome.branch)
		})
	}
}

func TestCreateRefundReadbackFailure(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		readStatus  int
		readBody    string
		maxBytes    int64
		shouldRetry bool
		wantKind    sdkgo.FailureKind
		wantMessage string
	}{
		{name: "temporary unavailability", readStatus: http.StatusServiceUnavailable, shouldRetry: true},
		{name: "oversized list", readStatus: http.StatusOK, readBody: strings.Repeat("x", 65), maxBytes: 64, wantKind: sdkgo.FailureAvailability, wantMessage: "Stripe refund outcome cannot be confirmed after a server error"},
		{name: "read-back forbidden", readStatus: http.StatusForbidden, readBody: `{"error":{"type":"invalid_request_error"}}`, wantKind: sdkgo.FailureAuthorization, wantMessage: stripeDeniedReadbackMessage},
		{name: "read-back unauthenticated", readStatus: http.StatusUnauthorized, readBody: `{"error":{"type":"invalid_request_error"}}`, wantKind: sdkgo.FailureAuthentication, wantMessage: stripeDeniedReadbackMessage},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				if request.Method == http.MethodPost {
					response.WriteHeader(http.StatusInternalServerError)
					return
				}
				response.WriteHeader(testCase.readStatus)
				_, writeErr := io.WriteString(response, testCase.readBody)
				require.NoError(t, writeErr)
			}))
			defer server.Close()
			client := newStripeClient(t, server.URL, stripe.Config{MaxResponseBytes: testCase.maxBytes})
			outcome, err := stripeWriteCases()[2].invoke(client, newStripeDexContext("refund-readback-error"))
			if testCase.shouldRetry {
				requireStripeRetry(t, err, sdkgo.FailureAvailability)
				return
			}
			require.NoError(t, err)
			require.Equal(t, sdkgo.UncertainBranchID, outcome.branch)
			require.Equal(t, testCase.wantKind, outcome.failure.Kind)
			require.Equal(t, testCase.wantMessage, outcome.failure.Message)
		})
	}
}

const stripeDeniedReadbackMessage = "Stripe refund outcome cannot be confirmed after a server error because Stripe denied the refund read-back; the API key needs Refund read permission"

func TestStripeKeyedWritesRejectOversizedResponses(t *testing.T) {
	for _, write := range stripeWriteCases() {
		t.Run(write.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				if _, err := io.WriteString(response, strings.Repeat("x", 65)); err != nil {
					panic(err)
				}
			}))
			defer server.Close()
			client := newStripeClient(t, server.URL, stripe.Config{MaxResponseBytes: 64})
			outcome, err := write.invoke(client, newStripeDexContext("oversized-response"))
			require.NoError(t, err)
			require.Equal(t, write.invalidResponseBranch, outcome.branch)
			require.Equal(t, sdkgo.FailureResponseTooLarge, outcome.failure.Kind)
		})
	}
}

const stripeKeyInUseBody = `{"error":{"code":"idempotency_key_in_use","message":"There is currently another in-progress request using this idempotency key."}}`

func TestStripeKeyedWritesRetryKeyStillInUseWithSameKey(t *testing.T) {
	for _, write := range stripeWriteCases() {
		t.Run(write.name, func(t *testing.T) {
			provider := &stripeWriteProvider{
				path: write.path, objectID: write.objectID, responseBody: write.responseBody,
				objectsByKey: make(map[string]string), conflictsBeforeSuccess: 1,
			}
			server := httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
			defer server.Close()
			client := newStripeClient(t, server.URL, stripe.Config{})
			ctx := newStripeDexContext("key-in-use")
			_, err := write.invoke(client, ctx)
			requireStripeRetry(t, err, sdkgo.FailureConflict)
			outcome, err := write.invoke(client, ctx)
			require.NoError(t, err)
			require.Equal(t, write.successBranch, outcome.branch)
			require.Equal(t, write.objectID, outcome.objectID)
			provider.mutex.Lock()
			defer provider.mutex.Unlock()
			require.Len(t, provider.keys, 2)
			require.NotEmpty(t, provider.keys[0])
			require.Equal(t, provider.keys[0], provider.keys[1])
			require.Len(t, provider.objectsByKey, 1)
		})
	}
}

func TestStripeKeyedWritesClassifyConflicts(t *testing.T) {
	testCases := []struct {
		name    string
		status  int
		headers http.Header
		body    string
		isRetry bool
	}{
		{name: "409 key in use", status: http.StatusConflict, body: stripeKeyInUseBody, isRetry: true},
		{name: "409 with retry hint", status: http.StatusConflict, headers: http.Header{"Stripe-Should-Retry": {"true"}}, body: `{"error":{"type":"invalid_request_error"}}`, isRetry: true},
		{name: "409 without code", status: http.StatusConflict},
		{name: "409 idempotency mismatch", status: http.StatusConflict, body: `{"error":{"type":"idempotency_error","message":"Keys for idempotent requests can only be used with the same parameters they were first used with."}}`},
		{name: "400 idempotency mismatch", status: http.StatusBadRequest, body: `{"error":{"type":"idempotency_error","message":"Keys for idempotent requests can only be used with the same parameters they were first used with."}}`},
	}
	for _, write := range stripeWriteCases() {
		for _, testCase := range testCases {
			t.Run(write.name+" "+testCase.name, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
					require.Equal(t, http.MethodPost, request.Method)
					require.Equal(t, write.path, request.URL.Path)
					for name, values := range testCase.headers {
						response.Header().Set(name, values[0])
					}
					response.WriteHeader(testCase.status)
					_, writeErr := io.WriteString(response, testCase.body)
					require.NoError(t, writeErr)
				}))
				defer server.Close()
				client := newStripeClient(t, server.URL, stripe.Config{})
				outcome, err := write.invoke(client, newStripeDexContext("conflict"))
				if testCase.isRetry {
					requireStripeRetry(t, err, sdkgo.FailureConflict)
					return
				}
				require.NoError(t, err)
				require.Equal(t, write.providerRejectedBranch, outcome.branch)
				wantKind := sdkgo.FailureProviderRejection
				if testCase.status == http.StatusConflict {
					wantKind = sdkgo.FailureConflict
				}
				require.Equal(t, wantKind, outcome.failure.Kind)
			})
		}
	}
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
	mutex                  sync.Mutex
	path                   string
	objectID               string
	responseBody           string
	conflictsBeforeSuccess int
	keys                   []string
	objectsByKey           map[string]string
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
	body := provider.responseBody
	if len(provider.keys) <= provider.conflictsBeforeSuccess {
		response.WriteHeader(http.StatusConflict)
		body = stripeKeyInUseBody
	}
	provider.mutex.Unlock()
	if _, err := io.WriteString(response, body); err != nil {
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
