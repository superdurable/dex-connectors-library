//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/docusign"
	envelopesigning "github.com/superdurable/dex-connectors-library/connectors/docusign/examples/envelope-signing/flow"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

// TestConnectEventResumesTheWaitingFlowWithRealDex covers README steps 5 and 6 against a stand-in DocuSign.
func TestConnectEventResumesTheWaitingFlowWithRealDex(t *testing.T) {
	fake := newFakeDocuSign(t)
	logs := &recordedLogs{}
	t.Cleanup(func() {
		if t.Failed() {
			t.Log(logs.text())
		}
	})
	documents := t.TempDir()
	harness := startSigningHarness(t, fake, logs, documents, envelopesigning.SigningPolicy{
		StatusPollInterval: time.Hour, SigningDeadline: 24 * time.Hour, VoidReason: "The signing deadline passed.",
	})
	requestID := uniqueRequestID("opp")
	state := harness.startAndAwaitWaiting(t, requestID)

	forged := connectEventBody(docusign.ConnectEventEnvelopeCompleted, state.EnvelopeID, requestID)
	require.Equal(t, 400, harness.endpoint.deliver(t, forged, "not-the-hmac-key"), "a forged delivery is refused")
	uncorrelated := connectEventBody(docusign.ConnectEventEnvelopeCompleted, state.EnvelopeID, "")
	require.Equal(t, 200, harness.endpoint.deliver(t, uncorrelated, sentinelHMACKey))
	eventID := state.EnvelopeID + ":" + docusign.ConnectEventEnvelopeCompleted
	require.Eventually(t, func() bool {
		return logs.count("trigger event skipped: filtered", map[string]string{"event_id": eventID}) == 1
	}, 20*time.Second, 25*time.Millisecond, "AcceptEnvelopeEvent consumes an event without the correlation field")
	require.Equal(t, envelopesigning.StatusWaitingForSignature, harness.signingState(t, requestID).Status, "neither delivery resumed the Flow")

	// An immediate Connect redelivery is consumed without a second effect, during or after the download.
	signed := connectEventBody(docusign.ConnectEventEnvelopeCompleted, state.EnvelopeID, requestID)
	require.Equal(t, 200, harness.endpoint.deliver(t, signed, sentinelHMACKey))
	require.Equal(t, 200, harness.endpoint.deliver(t, signed, sentinelHMACKey))
	require.Eventually(t, func() bool {
		return logs.count("trigger event delivered", map[string]string{"event_id": eventID, "target": "rpc"})+
			logs.count("trigger event skipped: undeliverable", map[string]string{"event_id": eventID}) == 2
	}, 30*time.Second, 25*time.Millisecond, "both deliveries are consumed")

	outcome := harness.waitForOutcome(t, requestID)
	require.Equal(t, envelopesigning.StatusCompleted, outcome.Status)
	require.Equal(t, "connect", outcome.ResumedBy)
	require.Equal(t, []string{eventID}, outcome.ReceivedEventIDs)
	digest := sha256.Sum256(signedPDF)
	require.Equal(t, hex.EncodeToString(digest[:]), outcome.Document.SHA256)
	require.Equal(t, int64(len(signedPDF)), outcome.Document.ByteCount)
	stored, err := os.ReadFile(filepath.Join(documents, outcome.Document.StoredLocation))
	require.NoError(t, err)
	require.Equal(t, signedPDF, stored, "the PDF lives in the document store, not in Dex")
	creates, statusReads, downloads, voids := fake.counts()
	require.Equal(t, 1, creates)
	require.Zero(t, statusReads, "Connect resumed the Flow before any poll")
	require.Equal(t, 1, downloads, "the duplicate event downloaded nothing")
	require.Empty(t, voids)

	// A delivery after the outcome finds the Flow closed and is consumed.
	skippedBefore := logs.count("trigger event skipped: undeliverable", map[string]string{"event_id": eventID})
	require.Equal(t, 200, harness.endpoint.deliver(t, signed, sentinelHMACKey))
	require.Eventually(t, func() bool {
		return logs.count("trigger event skipped: undeliverable", map[string]string{"event_id": eventID}) == skippedBefore+1
	}, 20*time.Second, 25*time.Millisecond)
	_, _, downloads, _ = fake.counts()
	require.Equal(t, 1, downloads)
	require.NotContains(t, logs.text(), sentinelToken)
	require.NotContains(t, logs.text(), sentinelHMACKey)
}

// TestWaitSurvivesAWorkerReplacementWithRealDex resumes the same Flow on a new Worker from a Connect decline.
func TestWaitSurvivesAWorkerReplacementWithRealDex(t *testing.T) {
	fake := newFakeDocuSign(t)
	harness := startSigningHarness(t, fake, &recordedLogs{}, t.TempDir(), envelopesigning.SigningPolicy{
		StatusPollInterval: time.Hour, SigningDeadline: 24 * time.Hour, VoidReason: "The signing deadline passed.",
	})
	requestID := uniqueRequestID("restart")
	state := harness.startAndAwaitWaiting(t, requestID)
	harness.replaceWorker(t)

	declined := connectEventBody(docusign.ConnectEventEnvelopeDeclined, state.EnvelopeID, requestID)
	require.Equal(t, 200, harness.endpoint.deliver(t, declined, sentinelHMACKey))
	outcome := harness.waitForOutcome(t, requestID)
	require.Equal(t, envelopesigning.StatusDeclined, outcome.Status)
	require.Equal(t, docusign.EnvelopeStatusDeclined, outcome.EnvelopeStatus)
	require.Equal(t, "connect", outcome.ResumedBy)
	require.Equal(t, state.EnvelopeID, outcome.EnvelopeID)
	creates, statusReads, downloads, voids := fake.counts()
	require.Equal(t, 1, creates, "the replacement Worker did not send the envelope again")
	require.Zero(t, statusReads+downloads+len(voids))
}

// TestStatusPollResumesWhenConnectStaysSilentWithRealDex covers the Timer fallback: no Connect event
// arrives, and the next status poll finds the envelope declined.
func TestStatusPollResumesWhenConnectStaysSilentWithRealDex(t *testing.T) {
	fake := newFakeDocuSign(t)
	harness := startSigningHarness(t, fake, &recordedLogs{}, t.TempDir(), envelopesigning.SigningPolicy{
		StatusPollInterval: 2 * time.Second, SigningDeadline: time.Hour, VoidReason: "The signing deadline passed.",
	})
	requestID := uniqueRequestID("poll")
	state := harness.startAndAwaitWaiting(t, requestID)
	fake.setStatus(state.EnvelopeID, "declined")

	outcome := harness.waitForOutcome(t, requestID)
	require.Equal(t, envelopesigning.StatusDeclined, outcome.Status)
	require.Equal(t, "statusPoll", outcome.ResumedBy)
	require.Equal(t, 1, outcome.StatusPollCount)
	_, _, downloads, voids := fake.counts()
	require.Zero(t, downloads)
	require.Empty(t, voids)
}

// TestSigningDeadlineVoidsTheUnsignedEnvelopeWithRealDex voids an envelope still unsigned at the deadline.
func TestSigningDeadlineVoidsTheUnsignedEnvelopeWithRealDex(t *testing.T) {
	fake := newFakeDocuSign(t)
	harness := startSigningHarness(t, fake, &recordedLogs{}, t.TempDir(), envelopesigning.SigningPolicy{
		StatusPollInterval: time.Second, SigningDeadline: 3 * time.Second, VoidReason: "The signing deadline passed.",
	})
	requestID := uniqueRequestID("expire")
	harness.startAndAwaitWaiting(t, requestID)

	outcome := harness.waitForOutcome(t, requestID)
	require.Equal(t, envelopesigning.StatusExpired, outcome.Status)
	require.Equal(t, docusign.EnvelopeStatusVoided, outcome.EnvelopeStatus)
	require.GreaterOrEqual(t, outcome.StatusPollCount, 1)
	_, _, _, voids := fake.counts()
	require.Len(t, voids, 1)
	require.JSONEq(t, `{"status":"voided","voidedReason":"The signing deadline passed."}`, voids[0])
}

// signingHarness is one Worker, Client, and Connect endpoint for the example Flow.
type signingHarness struct {
	client        *dex.Client
	flow          *envelopesigning.EnvelopeSigningFlow
	endpoint      *connectEndpoint
	registry      *dex.Registry
	cache         *blobcache.Cache
	workerAddress string
	worker        *dex.Worker
	workerResult  chan error
}

func startSigningHarness(t *testing.T, fake *fakeDocuSign, logs *recordedLogs, documents string, policy envelopesigning.SigningPolicy) *signingHarness {
	t.Helper()
	// Like main, route slog.Default too: the Trigger runner logs consumed undeliverable events there.
	previousDefault := slog.Default()
	slog.SetDefault(logs.logger())
	t.Cleanup(func() { slog.SetDefault(previousDefault) })
	store, err := newDirectoryDocumentStore(documents)
	require.NoError(t, err)
	connection := newExampleConnection(t, fake, docusign.WithCombinedDocumentStore(store), docusign.WithLogger(logs.logger()))
	flow, err := envelopesigning.NewEnvelopeSigningFlow(connection, policy)
	require.NoError(t, err)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	harness := &signingHarness{flow: flow, registry: registry, cache: cache, workerAddress: "127.0.0.1:" + unusedPort(t)}
	harness.startWorker(t)
	harness.client, err = dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: dexAddress(), WorkerTarget: harness.worker.WorkerTarget()})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, errors.Join(stopWorker(harness.worker), harness.client.Close(), cache.Close()))
		<-harness.workerResult
	})
	harness.endpoint = newConnectEndpoint(t, connection, newEnvelopeEventTarget(harness.client, flow, logs.logger()))
	return harness
}

func (harness *signingHarness) startWorker(t *testing.T) {
	t.Helper()
	worker, err := dex.NewWorker(harness.registry, harness.cache, dex.WorkerOptions{
		BindAddress: harness.workerAddress, FlowServiceAddress: dexAddress(), WorkerTarget: dex.WorkerTarget{Address: harness.workerAddress},
	})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	harness.worker, harness.workerResult = worker, workerResult
}

// replaceWorker force-stops the Worker, like a crash, and starts a new one with the same definitions.
func (harness *signingHarness) replaceWorker(t *testing.T) {
	t.Helper()
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	// A forced stop returns the expired context's error; nothing is in flight during the durable wait.
	_ = harness.worker.Stop(expired)
	require.NoError(t, <-harness.workerResult)
	harness.startWorker(t)
}

// startAndAwaitWaiting starts the request's Flow and polls its state until the envelope is out for signature.
func (harness *signingHarness) startAndAwaitWaiting(t *testing.T, requestID string) envelopesigning.SigningState {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	request := envelopesigning.SigningRequest{
		RequestID: requestID, TemplateID: exampleTemplateID, EmailSubject: "Meridian Corp: MSA",
		Signers: []docusign.TemplateRole{{RoleName: "Customer", Name: "Priya Raman", Email: "priya@meridian.example.com"}},
	}
	flowID := envelopesigning.FlowIDForRequest(requestID)
	_, err := harness.client.StartFlow(ctx, harness.flow, flowID, request, dex.StartFlowOptions{RequestID: &flowID})
	require.NoError(t, err)
	var state envelopesigning.SigningState
	require.Eventually(t, func() bool {
		state = harness.signingState(t, requestID)
		return state.Status == envelopesigning.StatusWaitingForSignature
	}, 30*time.Second, 25*time.Millisecond, "the Flow must send the envelope and wait")
	require.NotEmpty(t, state.EnvelopeID)
	return state
}

// signingState reads the Flow's state through its RPC; a Flow not started yet reads as empty.
func (harness *signingHarness) signingState(t *testing.T, requestID string) envelopesigning.SigningState {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var state envelopesigning.SigningState
	err := harness.client.InvokeRPC(ctx, envelopesigning.FlowIDForRequest(requestID), harness.flow.GetSigningState, dex.None(nil), &state)
	var notStarted *dex.FlowNotFoundError
	if errors.As(err, &notStarted) {
		return envelopesigning.SigningState{}
	}
	require.NoError(t, err)
	return state
}

func (harness *signingHarness) waitForOutcome(t *testing.T, requestID string) envelopesigning.SigningState {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	result, err := harness.client.WaitForFlow(ctx, envelopesigning.FlowIDForRequest(requestID), dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var outcome envelopesigning.SigningState
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	return outcome
}

func dexAddress() string {
	return environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")
}

func uniqueRequestID(prefix string) string {
	return prefix + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
}

func unusedPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	return strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
}
