// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package webhooktrigger_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/webhooktrigger"
)

type testCredentials struct {
	secret string
}

type orderEvent struct {
	Kind string
}

var testConnection = sdkgo.ConnectionRef{Provider: "orders", Name: "shop"}

// recordingTarget records prepared and handled events; a non-nil prepareErr fails every PrepareTrigger.
type recordingTarget struct {
	mu         sync.Mutex
	prepared   []string
	handled    chan sdkgo.TriggerEvent[orderEvent]
	prepareErr error
}

func newRecordingTarget() *recordingTarget {
	return &recordingTarget{handled: make(chan sdkgo.TriggerEvent[orderEvent], 16)}
}

func (target *recordingTarget) PrepareTrigger(_ context.Context, event sdkgo.TriggerEvent[orderEvent]) error {
	target.mu.Lock()
	defer target.mu.Unlock()
	if target.prepareErr != nil {
		return target.prepareErr
	}
	target.prepared = append(target.prepared, event.ID)
	return nil
}

func (target *recordingTarget) HandleTrigger(_ context.Context, event sdkgo.TriggerEvent[orderEvent]) error {
	target.handled <- event
	return nil
}

func (target *recordingTarget) preparedIDs() []string {
	target.mu.Lock()
	defer target.mu.Unlock()
	return append([]string(nil), target.prepared...)
}

// newTestEndpoint verifies an "X-Test-Secret" header and decodes "id:kind" bodies; kind "ignored" is unhandled.
func newTestEndpoint(t *testing.T, configure func(*webhooktrigger.EndpointConfig[testCredentials, orderEvent])) *webhooktrigger.Endpoint[testCredentials, orderEvent] {
	t.Helper()
	config := webhooktrigger.EndpointConfig[testCredentials, orderEvent]{
		ConnectorID: "orders", TriggerName: "orderUpdated", Connection: testConnection,
		Credentials:  sdkgo.StaticCredentialProvider[testCredentials]{testConnection: {secret: "signing-secret"}},
		MaxBodyBytes: 64,
		VerifyRequest: func(request webhooktrigger.Request, credentials testCredentials) error {
			if credentials.secret == "" {
				return fmt.Errorf("secret missing: %w", webhooktrigger.ErrVerificationUnavailable)
			}
			if request.Header.Get("X-Test-Secret") != credentials.secret {
				return errors.New("signature mismatch")
			}
			return nil
		},
		DecodeEvent: func(request webhooktrigger.Request) (sdkgo.TriggerEvent[orderEvent], bool, error) {
			id, kind, found := strings.Cut(string(request.Body), ":")
			if !found || id == "" {
				return sdkgo.TriggerEvent[orderEvent]{}, false, errors.New("malformed body")
			}
			if kind == "ignored" {
				return sdkgo.TriggerEvent[orderEvent]{}, false, nil
			}
			return sdkgo.TriggerEvent[orderEvent]{ID: id, OccurredAt: request.ReceivedAt, Payload: orderEvent{Kind: kind}}, true, nil
		},
		Now: func() time.Time { return time.Date(2026, time.September, 30, 12, 0, 0, 0, time.UTC) },
	}
	if configure != nil {
		configure(&config)
	}
	endpoint, err := webhooktrigger.NewEndpoint(config)
	require.NoError(t, err)
	return endpoint
}

func postWebhook(endpoint http.Handler, body string, secret string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/webhooks/orders", strings.NewReader(body))
	request.Header.Set("X-Test-Secret", secret)
	response := httptest.NewRecorder()
	endpoint.ServeHTTP(response, request)
	return response
}

// runSource runs source until the test ends and waits until the endpoint counts it as running.
func runSource(t *testing.T, endpoint *webhooktrigger.Endpoint[testCredentials, orderEvent], source sdkgo.TriggerSource[orderEvent], target sdkgo.TriggerTarget[orderEvent]) {
	t.Helper()
	expectedCount := endpoint.RunningSourceCount() + 1
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- source.Run(ctx, target) }()
	t.Cleanup(func() {
		cancel()
		require.ErrorIs(t, <-done, context.Canceled)
	})
	require.Eventually(t, func() bool { return endpoint.RunningSourceCount() == expectedCount }, 5*time.Second, time.Millisecond)
}

func receiveHandled(t *testing.T, target *recordingTarget) sdkgo.TriggerEvent[orderEvent] {
	t.Helper()
	select {
	case event := <-target.handled:
		return event
	case <-time.After(5 * time.Second):
		t.Fatal("event was not delivered")
		return sdkgo.TriggerEvent[orderEvent]{}
	}
}

func TestEndpointRecordsBeforeAcknowledgingAndDeliversToAcceptingSources(t *testing.T) {
	endpoint := newTestEndpoint(t, nil)
	paidTarget, everyTarget := newRecordingTarget(), newRecordingTarget()
	runSource(t, endpoint, endpoint.NewSource(func(event sdkgo.TriggerEvent[orderEvent]) bool { return event.Payload.Kind == "paid" }), paidTarget)
	runSource(t, endpoint, endpoint.NewSource(nil), everyTarget)

	response := postWebhook(endpoint, "evt_1:paid", "signing-secret")
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, []string{"evt_1"}, paidTarget.preparedIDs(), "recorded before the 200")
	require.Equal(t, []string{"evt_1"}, everyTarget.preparedIDs())
	delivered := receiveHandled(t, paidTarget)
	require.Equal(t, "evt_1", delivered.ID)
	require.Equal(t, time.Date(2026, time.September, 30, 12, 0, 0, 0, time.UTC), delivered.OccurredAt)
	require.Equal(t, "evt_1", receiveHandled(t, everyTarget).ID)

	require.Equal(t, http.StatusOK, postWebhook(endpoint, "evt_2:shipped", "signing-secret").Code)
	require.Equal(t, []string{"evt_1"}, paidTarget.preparedIDs(), "a rejecting source records nothing")
	require.Equal(t, "evt_2", receiveHandled(t, everyTarget).ID)
}

func TestEndpointDeliversEventsInArrivalOrder(t *testing.T) {
	endpoint := newTestEndpoint(t, nil)
	target := newRecordingTarget()
	runSource(t, endpoint, endpoint.NewSource(nil), target)
	for index := range 5 {
		require.Equal(t, http.StatusOK, postWebhook(endpoint, fmt.Sprintf("evt_%d:paid", index), "signing-secret").Code)
	}
	for index := range 5 {
		require.Equal(t, fmt.Sprintf("evt_%d", index), receiveHandled(t, target).ID)
	}
}

func TestEndpointAnswersEachFailureSoTheProviderRetriesOnlyRecoverableOnes(t *testing.T) {
	for _, test := range []struct {
		name         string
		configure    func(*webhooktrigger.EndpointConfig[testCredentials, orderEvent])
		isRunning    bool
		prepareErr   error
		method       string
		body         string
		secret       string
		expectedCode int
	}{
		{name: "another method", method: http.MethodGet, body: "evt:paid", secret: "signing-secret", expectedCode: http.StatusMethodNotAllowed},
		{name: "oversized body", body: strings.Repeat("x", 65), secret: "signing-secret", expectedCode: http.StatusRequestEntityTooLarge},
		{name: "failed verification", body: "evt:paid", secret: "wrong", expectedCode: http.StatusBadRequest},
		{name: "verification unavailable", body: "evt:paid", expectedCode: http.StatusServiceUnavailable,
			configure: func(config *webhooktrigger.EndpointConfig[testCredentials, orderEvent]) {
				config.Credentials = sdkgo.StaticCredentialProvider[testCredentials]{testConnection: {}}
			}},
		{name: "credentials unavailable", body: "evt:paid", secret: "signing-secret", expectedCode: http.StatusServiceUnavailable,
			configure: func(config *webhooktrigger.EndpointConfig[testCredentials, orderEvent]) {
				config.Credentials = sdkgo.StaticCredentialProvider[testCredentials]{}
			}},
		{name: "undecodable event", body: "no-separator", secret: "signing-secret", expectedCode: http.StatusBadRequest},
		{name: "unhandled event", body: "evt:ignored", secret: "signing-secret", expectedCode: http.StatusOK},
		{name: "no running source", body: "evt:paid", secret: "signing-secret", expectedCode: http.StatusServiceUnavailable},
		{name: "recording failed", isRunning: true, prepareErr: errors.New("inbox unavailable"), body: "evt:paid", secret: "signing-secret", expectedCode: http.StatusServiceUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			endpoint := newTestEndpoint(t, test.configure)
			target := newRecordingTarget()
			target.prepareErr = test.prepareErr
			if test.isRunning {
				runSource(t, endpoint, endpoint.NewSource(nil), target)
			}
			method := test.method
			if method == "" {
				method = http.MethodPost
			}
			request := httptest.NewRequest(method, "/webhooks/orders", strings.NewReader(test.body))
			request.Header.Set("X-Test-Secret", test.secret)
			response := httptest.NewRecorder()
			endpoint.ServeHTTP(response, request)
			require.Equal(t, test.expectedCode, response.Code)
			if test.expectedCode == http.StatusMethodNotAllowed {
				require.Equal(t, http.MethodPost, response.Header().Get("Allow"))
			}
			require.NotContains(t, response.Body.String(), "signature mismatch", "error text never reaches the provider")
			require.NotContains(t, response.Body.String(), "inbox unavailable")
		})
	}
}

func TestEndpointAnswersRetryableWhenASourceQueueIsFull(t *testing.T) {
	endpoint := newTestEndpoint(t, func(config *webhooktrigger.EndpointConfig[testCredentials, orderEvent]) {
		config.SourceQueueCapacity = 1
	})
	blocked := make(chan struct{})
	target := &blockingTarget{release: blocked, started: make(chan struct{}, 1)}
	source := endpoint.NewSource(nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- source.Run(ctx, target) }()
	defer func() { cancel(); close(blocked); <-done }()
	require.Eventually(t, func() bool { return endpoint.RunningSourceCount() == 1 }, 5*time.Second, time.Millisecond)
	require.Equal(t, http.StatusOK, postWebhook(endpoint, "evt_1:paid", "signing-secret").Code)
	<-target.started
	require.Equal(t, http.StatusOK, postWebhook(endpoint, "evt_2:paid", "signing-secret").Code, "fills the one-slot queue")
	require.Equal(t, http.StatusServiceUnavailable, postWebhook(endpoint, "evt_3:paid", "signing-secret").Code)
}

type blockingTarget struct {
	release <-chan struct{}
	started chan struct{}
}

func (target *blockingTarget) HandleTrigger(ctx context.Context, _ sdkgo.TriggerEvent[orderEvent]) error {
	select {
	case target.started <- struct{}{}:
	default:
	}
	select {
	case <-target.release:
	case <-ctx.Done():
	}
	return nil
}

func TestSourceRunsOnceAndStopsReceivingWhenCanceled(t *testing.T) {
	endpoint := newTestEndpoint(t, nil)
	source := endpoint.NewSource(nil)
	require.Error(t, source.Run(context.Background(), nil))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	target := newRecordingTarget()
	go func() { done <- source.Run(ctx, target) }()
	require.Eventually(t, func() bool { return endpoint.RunningSourceCount() == 1 }, 5*time.Second, time.Millisecond)
	require.Equal(t, http.StatusOK, postWebhook(endpoint, "evt_1:paid", "signing-secret").Code)
	require.ErrorContains(t, source.Run(context.Background(), target), "already running")
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	require.Zero(t, endpoint.RunningSourceCount())
	require.Equal(t, http.StatusServiceUnavailable, postWebhook(endpoint, "evt_2:paid", "signing-secret").Code)
}

func TestEndpointResolvesCredentialsWithAStableCallPerBody(t *testing.T) {
	var calls []sdkgo.Call
	var mu sync.Mutex
	endpoint := newTestEndpoint(t, func(config *webhooktrigger.EndpointConfig[testCredentials, orderEvent]) {
		config.Credentials = credentialRecorder{calls: &calls, mu: &mu}
	})
	for _, body := range []string{"evt_1:ignored", "evt_1:ignored", "evt_2:ignored"} {
		require.Equal(t, http.StatusOK, postWebhook(endpoint, body, "signing-secret").Code)
	}
	require.Len(t, calls, 3)
	require.Equal(t, calls[0].ID, calls[1].ID, "a redelivered body resolves with the same call")
	require.NotEqual(t, calls[0].ID, calls[2].ID)
	require.Regexp(t, `^[0-9a-f]{8}-[0-9a-f]{4}-5[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`, string(calls[0].ID))
	require.Equal(t, sdkgo.OperationRef{ConnectorID: "orders", OperationID: "orderUpdated"}, calls[0].Operation)
	require.Equal(t, testConnection, calls[0].Connection)
}

type credentialRecorder struct {
	calls *[]sdkgo.Call
	mu    *sync.Mutex
}

func (recorder credentialRecorder) Resolve(call sdkgo.Call) (testCredentials, error) {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	*recorder.calls = append(*recorder.calls, call)
	return testCredentials{secret: "signing-secret"}, nil
}

func TestNewEndpointRejectsIncompleteConfiguration(t *testing.T) {
	for name, configure := range map[string]func(*webhooktrigger.EndpointConfig[testCredentials, orderEvent]){
		"missing Trigger name": func(config *webhooktrigger.EndpointConfig[testCredentials, orderEvent]) { config.TriggerName = "" },
		"missing connection": func(config *webhooktrigger.EndpointConfig[testCredentials, orderEvent]) {
			config.Connection = sdkgo.ConnectionRef{}
		},
		"missing verifier":        func(config *webhooktrigger.EndpointConfig[testCredentials, orderEvent]) { config.VerifyRequest = nil },
		"non-positive body limit": func(config *webhooktrigger.EndpointConfig[testCredentials, orderEvent]) { config.MaxBodyBytes = 0 },
		"negative queue capacity": func(config *webhooktrigger.EndpointConfig[testCredentials, orderEvent]) {
			config.SourceQueueCapacity = -1
		},
		"missing credentials": func(config *webhooktrigger.EndpointConfig[testCredentials, orderEvent]) { config.Credentials = nil },
	} {
		t.Run(name, func(t *testing.T) {
			config := webhooktrigger.EndpointConfig[testCredentials, orderEvent]{
				ConnectorID: "orders", TriggerName: "orderUpdated", Connection: testConnection,
				Credentials:   sdkgo.StaticCredentialProvider[testCredentials]{},
				MaxBodyBytes:  64,
				VerifyRequest: func(webhooktrigger.Request, testCredentials) error { return nil },
				DecodeEvent: func(webhooktrigger.Request) (sdkgo.TriggerEvent[orderEvent], bool, error) {
					return sdkgo.TriggerEvent[orderEvent]{}, false, nil
				},
			}
			configure(&config)
			_, err := webhooktrigger.NewEndpoint(config)
			require.Error(t, err)
		})
	}
}
