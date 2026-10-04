//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/webhook"
	formsubmission "github.com/superdurable/dex-connectors-library/connectors/superdurable/webhook/examples/form-submission/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

// TestSignedSubmissionStartsOneFlowAndDuplicatesAndForgeriesStartNoneWithRealDex covers README steps 4 and 5.
func TestSignedSubmissionStartsOneFlowAndDuplicatesAndForgeriesStartNoneWithRealDex(t *testing.T) {
	receiver := newForwardingReceiver(t)
	logs := newRecordedLogs(t)
	connection := newExampleConnection(t, receiver.URL+"/forward", webhook.WithHTTPClient(receiver.Client()), webhook.WithLogger(logs.logger()))
	flow := formsubmission.NewFlow(connection)
	client := startWorkerAndClient(t, flow)
	endpoint := newSubmissionEndpoint(t, connection, newSubmissionTarget(client, flow, logs.logger()))
	endpoint.start(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	eventID := "evt_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	body := submissionBody(eventID, "form_response")

	require.Equal(t, http.StatusOK, endpoint.postSubmission(t, body, false))
	outcome := waitForForwarded(t, ctx, client, eventID)
	require.Equal(t, webhook.SendEventBranchDelivered, outcome.Branch)
	require.Equal(t, http.StatusAccepted, outcome.StatusCode)
	deliveries := receiver.deliveriesFor(eventID)
	require.Len(t, deliveries, 1)
	require.True(t, deliveries[0].isSignatureValid, "the receiver verifies the Standard Webhooks signature")
	require.Equal(t, outcome.WebhookID, deliveries[0].webhookID)

	// The sender redelivers the same submission: the Flow start deduplicates it.
	require.Equal(t, http.StatusOK, endpoint.postSubmission(t, body, false))
	require.Eventually(t, func() bool {
		return len(logs.find("trigger event delivered", map[string]string{"event_id": eventID, "duplicate": "true"})) == 1
	}, 20*time.Second, 25*time.Millisecond, "the redelivery reaches Dex as a duplicate start")
	require.Len(t, logs.find("trigger event delivered", map[string]string{"event_id": eventID, "duplicate": "false"}), 1)
	require.Len(t, receiver.deliveriesFor(eventID), 1, "the Flow forwarded the submission once")

	// A forged submission and a filtered partial response start nothing.
	forgedID, partialID := eventID+"_forged", eventID+"_partial"
	require.Equal(t, http.StatusBadRequest, endpoint.postSubmission(t, submissionBody(forgedID, "form_response"), true))
	require.Equal(t, http.StatusOK, endpoint.postSubmission(t, submissionBody(partialID, "form_response_partial"), false))
	laterID := eventID + "_later"
	require.Equal(t, http.StatusOK, endpoint.postSubmission(t, submissionBody(laterID, "form_response"), false))
	waitForForwarded(t, ctx, client, laterID)
	requireNoFlow(t, ctx, client, forgedID)
	requireNoFlow(t, ctx, client, partialID)
	require.Empty(t, receiver.deliveriesFor(forgedID))
	require.NotContains(t, logs.text(), sentinelSecret)
}

// TestSubmissionBeforeTheBindingRunsIsRetriedBySenderWithRealDex covers README step 6.
func TestSubmissionBeforeTheBindingRunsIsRetriedBySenderWithRealDex(t *testing.T) {
	receiver := newForwardingReceiver(t)
	logs := newRecordedLogs(t)
	connection := newExampleConnection(t, receiver.URL+"/forward", webhook.WithHTTPClient(receiver.Client()), webhook.WithLogger(logs.logger()))
	flow := formsubmission.NewFlow(connection)
	client := startWorkerAndClient(t, flow)
	endpoint := newSubmissionEndpoint(t, connection, newSubmissionTarget(client, flow, logs.logger()))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	eventID := "evt_early_" + strconv.FormatInt(time.Now().UnixNano(), 10)

	require.Equal(t, http.StatusServiceUnavailable, endpoint.postSubmission(t, submissionBody(eventID, "form_response"), false),
		"no binding runs yet, so the sender must retry")
	requireNoFlow(t, ctx, client, eventID)

	endpoint.start(t)
	require.Equal(t, http.StatusOK, endpoint.postSubmission(t, submissionBody(eventID, "form_response"), false), "the sender's retry")
	require.Equal(t, webhook.SendEventBranchDelivered, waitForForwarded(t, ctx, client, eventID).Branch)
	require.Len(t, receiver.deliveriesFor(eventID), 1)
}

func dexAddress() string {
	return environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")
}

// waitForForwarded waits for the submission's Flow to complete and returns its forwarding outcome.
func waitForForwarded(t *testing.T, ctx context.Context, client *dex.Client, eventID string) formsubmission.ForwardingOutcome {
	t.Helper()
	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for {
		result, err := client.WaitForFlow(waitCtx, formsubmission.FlowIDPrefix+eventID, dex.WaitForFlowOptions{NeedsResults: true})
		if err == nil {
			require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
			var outcome formsubmission.ForwardingOutcome
			require.NoError(t, result.DecodeSingleOutput(&outcome))
			return outcome
		}
		var notFound *dex.FlowNotFoundError
		require.True(t, errors.As(err, &notFound) && waitCtx.Err() == nil, "the submission's Flow must complete: %v", err)
		// Delivery to Dex is asynchronous after the 200, so the Flow may not exist yet.
		time.Sleep(50 * time.Millisecond)
	}
}

func requireNoFlow(t *testing.T, ctx context.Context, client *dex.Client, eventID string) {
	t.Helper()
	_, err := client.WaitForFlow(ctx, formsubmission.FlowIDPrefix+eventID, dex.WaitForFlowOptions{})
	var notFound *dex.FlowNotFoundError
	require.ErrorAs(t, err, &notFound, "event %s must not start a Flow", eventID)
}

// newInspectionClient waits for and inspects Flows; it registers no Worker.
func newInspectionClient(t *testing.T) *dex.Client {
	t.Helper()
	inspectionClient, err := webhook.New(webhook.Config{}, sdkgo.StaticCredentialProvider[webhook.Credentials]{})
	require.NoError(t, err)
	connection, err := webhook.NewConnection(inspectionClient, sdkgo.ConnectionRef{Provider: "webhook", Name: formsubmission.ConnectionName})
	require.NoError(t, err)
	registry, err := dex.NewRegistry([]dex.Flow{formsubmission.NewFlow(connection)})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "inspection-blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: dexAddress()})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, errors.Join(client.Close(), cache.Close())) })
	return client
}

func startWorkerAndClient(t *testing.T, flow *formsubmission.Flow) *dex.Client {
	t.Helper()
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	workerAddress := "127.0.0.1:" + unusedPort(t)
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress: workerAddress, FlowServiceAddress: dexAddress(), WorkerTarget: dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: dexAddress(), WorkerTarget: worker.WorkerTarget()})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	t.Cleanup(func() {
		require.NoError(t, errors.Join(stopWorker(worker), client.Close(), cache.Close()))
		<-workerResult
	})
	return client
}

// forwardingReceiver is the downstream TLS service that verifies each forwarded submission.
type forwardingReceiver struct {
	*httptest.Server
	mu         sync.Mutex
	deliveries []forwardedDelivery
}

type forwardedDelivery struct {
	submissionID     string
	webhookID        string
	isSignatureValid bool
}

func newForwardingReceiver(t *testing.T) *forwardingReceiver {
	t.Helper()
	receiver := &forwardingReceiver{}
	receiver.Server = httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			http.Error(response, "unreadable body", http.StatusBadRequest)
			return
		}
		webhookID, timestamp := request.Header.Get("webhook-id"), request.Header.Get("webhook-timestamp")
		mac := hmac.New(sha256.New, []byte(sentinelSecret))
		mac.Write([]byte(webhookID + "." + timestamp + "." + string(body)))
		var event formsubmission.ForwardedEvent
		if err := json.Unmarshal(body, &event); err != nil || event.Type != formsubmission.ForwardedEventType {
			http.Error(response, "unexpected event", http.StatusBadRequest)
			return
		}
		receiver.mu.Lock()
		receiver.deliveries = append(receiver.deliveries, forwardedDelivery{
			submissionID: event.SubmissionID, webhookID: webhookID,
			isSignatureValid: request.Header.Get("webhook-signature") == "v1,"+base64.StdEncoding.EncodeToString(mac.Sum(nil)),
		})
		receiver.mu.Unlock()
		response.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(receiver.Close)
	return receiver
}

func (receiver *forwardingReceiver) deliveriesFor(submissionID string) []forwardedDelivery {
	receiver.mu.Lock()
	defer receiver.mu.Unlock()
	matches := []forwardedDelivery{}
	for _, delivery := range receiver.deliveries {
		if delivery.submissionID == submissionID {
			matches = append(matches, delivery)
		}
	}
	return matches
}
