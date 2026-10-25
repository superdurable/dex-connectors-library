// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package webhook_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/webhook"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const forwardedPayload = `{"type":"form.submitted","data":{"email":"ada@example.com"}}`

// fakeReceiver is a TLS webhook receiver that verifies Standard Webhooks signatures with its own code.
type fakeReceiver struct {
	*httptest.Server
	key               []byte
	statusCode        int
	retryAfter        string
	dropFirstResponse bool
	// replayStatusCode, when set, is the answer for a webhook-id that arrived before, as a deduplicating receiver gives.
	replayStatusCode int
	mu               sync.Mutex
	deliveries       []receivedDelivery
	seenWebhookIDs   map[string]bool
}

type receivedDelivery struct {
	webhookID        string
	timestamp        string
	signature        string
	isSignatureValid bool
	contentType      string
	body             string
}

func newFakeReceiver(t *testing.T, key []byte, statusCode int) *fakeReceiver {
	t.Helper()
	receiver := &fakeReceiver{key: key, statusCode: statusCode, seenWebhookIDs: make(map[string]bool)}
	receiver.Server = httptest.NewTLSServer(http.HandlerFunc(receiver.receiveDelivery))
	t.Cleanup(receiver.Close)
	return receiver
}

func (receiver *fakeReceiver) receiveDelivery(response http.ResponseWriter, request *http.Request) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		http.Error(response, "unreadable", http.StatusBadRequest)
		return
	}
	webhookID, timestamp := request.Header.Get("webhook-id"), request.Header.Get("webhook-timestamp")
	expected := "v1," + base64.StdEncoding.EncodeToString(hmacSHA256(receiver.key, webhookID+"."+timestamp+"."+string(body)))
	receiver.mu.Lock()
	signature := request.Header.Get("webhook-signature")
	receiver.deliveries = append(receiver.deliveries, receivedDelivery{
		webhookID: webhookID, timestamp: timestamp, signature: signature, isSignatureValid: signature == expected,
		contentType: request.Header.Get("Content-Type"), body: string(body),
	})
	statusCode := receiver.statusCode
	if receiver.seenWebhookIDs[webhookID] && receiver.replayStatusCode != 0 {
		statusCode = receiver.replayStatusCode
	}
	receiver.seenWebhookIDs[webhookID] = true
	isFirstDelivery := len(receiver.deliveries) == 1
	receiver.mu.Unlock()
	if receiver.dropFirstResponse && isFirstDelivery {
		panic(http.ErrAbortHandler)
	}
	if receiver.retryAfter != "" {
		response.Header().Set("Retry-After", receiver.retryAfter)
	}
	response.WriteHeader(statusCode)
	_, _ = fmt.Fprint(response, `{"echo":"`+sentinelSecret+`"}`) // A response body must never reach the Result.
}

func (receiver *fakeReceiver) receivedDeliveries() []receivedDelivery {
	receiver.mu.Lock()
	defer receiver.mu.Unlock()
	return append([]receivedDelivery(nil), receiver.deliveries...)
}

func newSendingClient(t *testing.T, deliveryURL string, secret string, httpClient *http.Client) *webhook.Client {
	t.Helper()
	return newSendingClientWithClock(t, deliveryURL, secret, httpClient, func() time.Time { return fixedNow })
}

func newSendingClientWithClock(t *testing.T, deliveryURL string, secret string, httpClient *http.Client, clock func() time.Time) *webhook.Client {
	t.Helper()
	client, err := webhook.New(webhook.Config{DeliveryURL: deliveryURL}, staticCredentials(secret),
		webhook.WithClock(clock), webhook.WithHTTPClient(httpClient))
	require.NoError(t, err)
	return client
}

// advancingClock moves one minute on every reading, so each attempt reads a later time than the one before.
func advancingClock() func() time.Time {
	var readings atomic.Int64
	return func() time.Time { return fixedNow.Add(time.Duration(readings.Add(1)) * time.Minute) }
}

func sendEvent(t *testing.T, client *webhook.Client, step string, payload string) (webhook.SendEventResult, error) {
	t.Helper()
	return sdkgo.RunMutation(newStepContext(step), client.SendEvent(), testConnection, webhook.SendEventInput{Payload: json.RawMessage(payload)})
}

func requireSecretFree(t *testing.T, result webhook.SendEventResult) {
	t.Helper()
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), sentinelSecret)
}

func TestSendEventSignsWithStandardWebhooksAndKeepsTheWebhookIDAcrossRetries(t *testing.T) {
	receiver := newFakeReceiver(t, standardKey(t), http.StatusNoContent)
	client := newSendingClient(t, receiver.URL+"/hooks?tenant=forms", standardSecret, receiver.Client())

	first, err := sendEvent(t, client, "forward-1", forwardedPayload)
	require.NoError(t, err)
	retried, err := sendEvent(t, client, "forward-1", forwardedPayload)
	require.NoError(t, err)
	other, err := sendEvent(t, client, "forward-2", forwardedPayload)
	require.NoError(t, err)

	require.Equal(t, webhook.SendEventBranchDelivered, first.Branch)
	require.Regexp(t, regexp.MustCompile(`^msg_[0-9a-f]{32}$`), first.Value.WebhookID)
	require.Equal(t, http.StatusNoContent, first.Value.StatusCode)
	require.Equal(t, first.Value.WebhookID, retried.Value.WebhookID, "a retry of one Step execution resends the same webhook-id")
	require.NotEqual(t, first.Value.WebhookID, other.Value.WebhookID, "another Step execution sends another webhook-id")
	require.Equal(t, sdkgo.IdempotencyKey(first.Value.WebhookID), first.Receipt.IdempotencyKey)
	require.Equal(t, first.Value.WebhookID, first.Receipt.ProviderObjectID)
	requireSecretFree(t, first)

	deliveries := receiver.receivedDeliveries()
	require.Len(t, deliveries, 3)
	for _, delivery := range deliveries {
		require.True(t, delivery.isSignatureValid, "the receiver verifies every signature")
		require.Equal(t, strconv.FormatInt(fixedNow.Unix(), 10), delivery.timestamp)
		require.Equal(t, webhook.ContentTypeJSON, delivery.contentType)
		require.Equal(t, forwardedPayload, delivery.body)
	}
	require.Equal(t, first.Value.WebhookID, deliveries[0].webhookID)
}

func TestSendEventSignsWithTheBytesOfASecretWithoutThePrefix(t *testing.T) {
	receiver := newFakeReceiver(t, []byte(sentinelSecret), http.StatusOK)
	result, err := sendEvent(t, newSendingClient(t, receiver.URL, sentinelSecret, receiver.Client()), "forward-raw", forwardedPayload)
	require.NoError(t, err)
	require.Equal(t, webhook.SendEventBranchDelivered, result.Branch)
	require.True(t, receiver.receivedDeliveries()[0].isSignatureValid)
}

func TestSendEventMapsReceiverStatusesToBranches(t *testing.T) {
	for _, test := range []struct {
		statusCode     int
		expectedBranch sdkgo.BranchID
		expectedKind   sdkgo.FailureKind
	}{
		{http.StatusOK, webhook.SendEventBranchDelivered, ""},
		{http.StatusAccepted, webhook.SendEventBranchDelivered, ""},
		{http.StatusFound, webhook.SendEventBranchRejected, sdkgo.FailureProviderRejection},
		{http.StatusBadRequest, webhook.SendEventBranchRejected, sdkgo.FailureProviderRejection},
		{http.StatusUnauthorized, webhook.SendEventBranchRejected, sdkgo.FailureAuthentication},
		{http.StatusForbidden, webhook.SendEventBranchRejected, sdkgo.FailureAuthorization},
		{http.StatusNotFound, webhook.SendEventBranchRejected, sdkgo.FailureNotFound},
		{http.StatusConflict, webhook.SendEventBranchRejected, sdkgo.FailureConflict},
	} {
		t.Run(strconv.Itoa(test.statusCode), func(t *testing.T) {
			receiver := newFakeReceiver(t, standardKey(t), test.statusCode)
			result, err := sendEvent(t, newSendingClient(t, receiver.URL, standardSecret, receiver.Client()), "forward", forwardedPayload)
			require.NoError(t, err)
			require.Equal(t, test.expectedBranch, result.Branch)
			require.Equal(t, test.statusCode, result.Value.StatusCode)
			require.Len(t, receiver.receivedDeliveries(), 1, "redirects are never followed")
			if test.expectedKind == "" {
				require.Nil(t, result.Failure)
			} else {
				require.Equal(t, test.expectedKind, result.Failure.Kind)
				require.Contains(t, result.Failure.Message, strconv.Itoa(test.statusCode))
				require.Equal(t, test.statusCode == http.StatusConflict, strings.Contains(result.Failure.Message, "must answer 2xx"),
					"only a 409 carries the deduplication guidance")
			}
			requireSecretFree(t, result)
		})
	}
}

func TestSendEventRetriesReceiverFailuresWithTheSameWebhookID(t *testing.T) {
	for _, statusCode := range []int{http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable} {
		t.Run(strconv.Itoa(statusCode), func(t *testing.T) {
			receiver := newFakeReceiver(t, standardKey(t), statusCode)
			receiver.retryAfter = "7"
			_, err := sendEvent(t, newSendingClient(t, receiver.URL, standardSecret, receiver.Client()), "forward", forwardedPayload)
			var retryAfter *dex.RetryAfterError
			require.ErrorAs(t, err, &retryAfter)
			require.Equal(t, 7*time.Second, retryAfter.After)
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
			require.True(t, receiver.receivedDeliveries()[0].isSignatureValid)
		})
	}
}

func TestSendEventRetriesThrottledRequestsWithTheReceiversDelay(t *testing.T) {
	for _, statusCode := range []int{http.StatusRequestTimeout, http.StatusTooManyRequests} {
		t.Run(strconv.Itoa(statusCode), func(t *testing.T) {
			receiver := newFakeReceiver(t, standardKey(t), statusCode)
			receiver.retryAfter = "7"
			_, err := sendEvent(t, newSendingClient(t, receiver.URL, standardSecret, receiver.Client()), "forward", forwardedPayload)
			var retryAfter *dex.RetryAfterError
			require.ErrorAs(t, err, &retryAfter)
			require.Equal(t, 7*time.Second, retryAfter.After)
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			require.Equal(t, sdkgo.FailureRateLimit, retry.Failure.Kind)
		})
	}
}

func TestSendEventRetriesAnUndispatchedRequestAndALostAnswer(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	closedURL := "https://" + listener.Addr().String()
	require.NoError(t, listener.Close())
	_, err = sendEvent(t, newSendingClient(t, closedURL, standardSecret, nil), "forward", forwardedPayload)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "a refused connection never sent the event")
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)

	hanging := httptest.NewTLSServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		_, _ = io.ReadAll(request.Body)
		<-request.Context().Done()
	}))
	t.Cleanup(hanging.Close)
	impatient := hanging.Client()
	impatient.Timeout = 200 * time.Millisecond
	_, err = sendEvent(t, newSendingClient(t, hanging.URL, standardSecret, impatient), "forward", forwardedPayload)
	require.ErrorAs(t, err, &retry, "the receiver read the event and may have accepted it")
	require.Equal(t, sdkgo.FailureTransport, retry.Failure.Kind)
}

func TestSendEventRetriesALostResponseWithTheSameWebhookID(t *testing.T) {
	receiver := newFakeReceiver(t, standardKey(t), http.StatusAccepted)
	receiver.dropFirstResponse = true
	receiver.replayStatusCode = http.StatusOK // a deduplicating receiver acknowledges a repeated id with 2xx
	client := newSendingClient(t, receiver.URL, standardSecret, receiver.Client())
	_, err := sendEvent(t, client, "forward", forwardedPayload)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureTransport, retry.Failure.Kind)

	result, err := sendEvent(t, client, "forward", forwardedPayload)
	require.NoError(t, err)
	require.Equal(t, webhook.SendEventBranchDelivered, result.Branch)
	require.Equal(t, http.StatusOK, result.Value.StatusCode)
	deliveries := receiver.receivedDeliveries()
	require.Len(t, deliveries, 2)
	require.Equal(t, deliveries[0].webhookID, deliveries[1].webhookID)
	require.Equal(t, deliveries[0].body, deliveries[1].body)
	require.True(t, deliveries[0].isSignatureValid)
	require.True(t, deliveries[1].isSignatureValid)
}

func TestSendEventSignsEveryAttemptWithItsOwnTimestamp(t *testing.T) {
	receiver := newFakeReceiver(t, standardKey(t), http.StatusServiceUnavailable)
	client := newSendingClientWithClock(t, receiver.URL, standardSecret, receiver.Client(), advancingClock())
	for range 2 {
		_, err := sendEvent(t, client, "forward", forwardedPayload)
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry)
	}

	deliveries := receiver.receivedDeliveries()
	require.Len(t, deliveries, 2)
	require.Equal(t, deliveries[0].webhookID, deliveries[1].webhookID)
	require.Equal(t, deliveries[0].body, deliveries[1].body)
	firstTimestamp, err := strconv.ParseInt(deliveries[0].timestamp, 10, 64)
	require.NoError(t, err)
	secondTimestamp, err := strconv.ParseInt(deliveries[1].timestamp, 10, 64)
	require.NoError(t, err)
	require.Greater(t, secondTimestamp, firstTimestamp, "a retry reads the clock again instead of reusing the first timestamp")
	require.NotEqual(t, deliveries[0].signature, deliveries[1].signature, "the signature covers the new timestamp")
	require.True(t, deliveries[0].isSignatureValid)
	require.True(t, deliveries[1].isSignatureValid)
}

func TestSendEventReportsAReplayAnsweredWith409AsRejected(t *testing.T) {
	receiver := newFakeReceiver(t, standardKey(t), http.StatusAccepted)
	receiver.dropFirstResponse = true
	receiver.replayStatusCode = http.StatusConflict // a receiver that deduplicates but answers a repeated id with 409
	client := newSendingClient(t, receiver.URL, standardSecret, receiver.Client())
	_, err := sendEvent(t, client, "forward", forwardedPayload)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "the first answer was lost")

	result, err := sendEvent(t, client, "forward", forwardedPayload)
	require.NoError(t, err)
	require.Equal(t, webhook.SendEventBranchRejected, result.Branch, "the connector does not guess that the lost attempt delivered the event")
	require.Equal(t, sdkgo.FailureConflict, result.Failure.Kind)
	require.Equal(t, http.StatusConflict, result.Value.StatusCode)
	require.Contains(t, result.Failure.Message, "must answer 2xx for a webhook-id it already processed")
	deliveries := receiver.receivedDeliveries()
	require.Len(t, deliveries, 2)
	require.Equal(t, deliveries[0].webhookID, result.Value.WebhookID)
	require.Equal(t, deliveries[0].webhookID, deliveries[1].webhookID)
}

func TestSendEventSelectsDefectWithoutSendingInvalidRequests(t *testing.T) {
	receiver := newFakeReceiver(t, standardKey(t), http.StatusOK)
	smallLimit, err := webhook.New(webhook.Config{DeliveryURL: receiver.URL, MaxBodyBytes: 16}, staticCredentials(standardSecret),
		webhook.WithHTTPClient(receiver.Client()))
	require.NoError(t, err)
	for _, test := range []struct {
		name           string
		client         *webhook.Client
		payload        string
		expectedReason string
	}{
		{name: "blank deliveryUrl", client: newSendingClient(t, "", standardSecret, receiver.Client()), payload: forwardedPayload, expectedReason: "set it in Dex Web Connectors"},
		{name: "empty payload", client: newSendingClient(t, receiver.URL, standardSecret, receiver.Client()), payload: "", expectedReason: "valid JSON"},
		{name: "invalid payload", client: newSendingClient(t, receiver.URL, standardSecret, receiver.Client()), payload: `{"type":`, expectedReason: "valid JSON"},
		{name: "oversized payload", client: smallLimit, payload: forwardedPayload, expectedReason: "maxBodyBytes"},
		{name: "blank secret", client: newSendingClient(t, receiver.URL, "", receiver.Client()), payload: forwardedPayload, expectedReason: "signing secret"},
		{name: "invalid whsec_ secret", client: newSendingClient(t, receiver.URL, "whsec_not base64!", receiver.Client()), payload: forwardedPayload, expectedReason: "whsec_"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := sendEvent(t, test.client, "forward", test.payload)
			require.NoError(t, err)
			require.Equal(t, webhook.SendEventBranchDefect, result.Branch)
			require.Contains(t, result.Failure.Message, test.expectedReason)
		})
	}
	require.Empty(t, receiver.receivedDeliveries())
}

func TestNewAcceptsOnlyAnHTTPSDeliveryURL(t *testing.T) {
	for _, deliveryURL := range []string{
		"http://hooks.example.com/dex", "https://user:password@hooks.example.com/dex", "https://hooks.example.com/dex#fragment",
		"ftp://hooks.example.com", "https:///no-host", "https://hooks.example.com/with space",
	} {
		t.Run(deliveryURL, func(t *testing.T) {
			_, err := webhook.New(webhook.Config{DeliveryURL: deliveryURL}, staticCredentials(standardSecret))
			require.Error(t, err)
			require.NotContains(t, err.Error(), "password")
		})
	}
	_, err := webhook.New(webhook.Config{DeliveryURL: "https://hooks.example.com/dex?tenant=forms"}, staticCredentials(standardSecret))
	require.NoError(t, err)
}
