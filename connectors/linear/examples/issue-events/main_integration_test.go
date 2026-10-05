//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/linear"
	issueevents "github.com/superdurable/dex-connectors-library/connectors/linear/examples/issue-events/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

// TestSignedDeliveryStartsOneFlowAndDuplicatesStaleAndForgedDeliveriesStartNoneWithRealDex covers README
// steps 5 and 6: a valid delivery, Linear's re-signed retry, a stale replay, and forged deliveries.
func TestSignedDeliveryStartsOneFlowAndDuplicatesStaleAndForgedDeliveriesStartNoneWithRealDex(t *testing.T) {
	fake := newFakeLinear(t)
	logs := newRecordedLogs(t)
	connection := newExampleConnection(t, fake.connectionOption(), linear.WithLogger(logs.logger()))
	flow := issueevents.NewFlow(connection)
	client := startWorkerAndClient(t, flow)
	endpoint := newIssueEndpoint(t, connection, newIssueTarget(client, flow, logs.logger()))
	endpoint.start(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	issueID := uniqueIssueID()
	created := triggerEventID(linear.IssueEventActionCreate, issueID)

	require.Equal(t, http.StatusOK, endpoint.deliverWebhook(t, issueWebhookBody("create", issueID, exampleTeamID, time.Now()), sentinelSigningSecret, false))
	recorded := waitForRecorded(t, ctx, client, created)
	require.Equal(t, linear.GetIssueBranchFound, recorded.Branch)
	require.Equal(t, issueID, recorded.Issue.ID)
	require.Equal(t, "ENG-7", recorded.Issue.Identifier)
	require.Equal(t, linear.WorkflowStateTypeUnstarted, recorded.Issue.State.Type)
	require.Equal(t, 1, fake.readCount(issueID), "the Flow read the issue once")

	// Linear retries with a new webhookTimestamp and signature: the event ID, and so the Flow, stay the same.
	require.Equal(t, http.StatusOK, endpoint.deliverWebhook(t, issueWebhookBody("create", issueID, exampleTeamID, time.Now().Add(time.Second)),
		sentinelSigningSecret, false))
	require.Eventually(t, func() bool {
		return len(logs.find("trigger event delivered", map[string]string{"event_id": created, "duplicate": "true"})) == 1
	}, 20*time.Second, 25*time.Millisecond, "the redelivery reaches Dex as a duplicate start")
	require.Len(t, logs.find("trigger event delivered", map[string]string{"event_id": created, "duplicate": "false"}), 1)
	require.Equal(t, 1, fake.readCount(issueID), "no second Flow read the issue")

	// A stale replay and forged deliveries answer 400; an update is filtered and another team's issue is not
	// recorded by the binding. None starts a Flow.
	staleID, forgedID, otherTeamIssueID := uniqueIssueID(), uniqueIssueID(), uniqueIssueID()
	require.Equal(t, http.StatusBadRequest, endpoint.deliverWebhook(t,
		issueWebhookBody("create", staleID, exampleTeamID, time.Now().Add(-5*time.Minute)), sentinelSigningSecret, false), "stale webhookTimestamp")
	require.Equal(t, http.StatusBadRequest, endpoint.deliverWebhook(t,
		issueWebhookBody("create", forgedID, exampleTeamID, time.Now()), "not-the-signing-secret", false), "bad signature")
	require.Equal(t, http.StatusBadRequest, endpoint.deliverWebhook(t,
		issueWebhookBody("create", forgedID, exampleTeamID, time.Now()), sentinelSigningSecret, true), "body changed after signing")
	require.Equal(t, http.StatusOK, endpoint.deliverWebhook(t,
		issueWebhookBody("create", otherTeamIssueID, otherTeamID, time.Now()), sentinelSigningSecret, false), "another team is acknowledged")
	updated := triggerEventID(linear.IssueEventActionUpdate, issueID)
	require.Equal(t, http.StatusOK, endpoint.deliverWebhook(t, issueWebhookBody("update", issueID, exampleTeamID, time.Now()), sentinelSigningSecret, false))
	require.Eventually(t, func() bool {
		return len(logs.find("trigger event skipped: filtered", map[string]string{"event_id": updated})) == 1
	}, 20*time.Second, 25*time.Millisecond, "AcceptIssueCreated consumes the update without a Flow")
	laterID := uniqueIssueID()
	require.Equal(t, http.StatusOK, endpoint.deliverWebhook(t, issueWebhookBody("create", laterID, exampleTeamID, time.Now()), sentinelSigningSecret, false))
	waitForRecorded(t, ctx, client, triggerEventID(linear.IssueEventActionCreate, laterID))
	for _, eventID := range []string{
		triggerEventID("create", staleID), triggerEventID("create", forgedID), triggerEventID("create", otherTeamIssueID), updated,
	} {
		requireNoFlow(t, ctx, client, eventID)
	}
	require.Zero(t, fake.readCount(staleID)+fake.readCount(forgedID)+fake.readCount(otherTeamIssueID))
	require.NotContains(t, logs.text(), sentinelSigningSecret)
	require.NotContains(t, logs.text(), sentinelAPIKey)
}

// TestDeliveryBeforeTheBindingRunsIsAnswered503AndTheRetryStartsTheFlowWithRealDex: while the binding is not
// running, the endpoint answers 503, so Linear retries, and the retry that arrives once it runs starts the Flow.
func TestDeliveryBeforeTheBindingRunsIsAnswered503AndTheRetryStartsTheFlowWithRealDex(t *testing.T) {
	fake := newFakeLinear(t)
	logs := newRecordedLogs(t)
	connection := newExampleConnection(t, fake.connectionOption(), linear.WithLogger(logs.logger()))
	flow := issueevents.NewFlow(connection)
	client := startWorkerAndClient(t, flow)
	endpoint := newIssueEndpoint(t, connection, newIssueTarget(client, flow, logs.logger()))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	issueID := uniqueIssueID()
	created := triggerEventID(linear.IssueEventActionCreate, issueID)

	require.Equal(t, http.StatusServiceUnavailable, endpoint.deliverWebhook(t, issueWebhookBody("create", issueID, exampleTeamID, time.Now()),
		sentinelSigningSecret, false), "no binding runs yet, so Linear must retry")
	requireNoFlow(t, ctx, client, created)

	endpoint.start(t)
	require.Equal(t, http.StatusOK, endpoint.deliverWebhook(t, issueWebhookBody("create", issueID, exampleTeamID, time.Now()),
		sentinelSigningSecret, false), "Linear's retry")
	require.Equal(t, linear.GetIssueBranchFound, waitForRecorded(t, ctx, client, created).Branch)
	require.Equal(t, 1, fake.readCount(issueID))
}

func dexAddress() string {
	return environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")
}

// uniqueIssueID returns a fresh UUID-shaped issue ID, so every run uses new Flow IDs.
func uniqueIssueID() string {
	nanos := time.Now().UnixNano()
	time.Sleep(time.Microsecond) // Consecutive calls must differ.
	return fmt.Sprintf("%08x-%04x-4%03x-8%03x-%012x", uint32(nanos>>32), uint16(nanos>>16), uint16(nanos)&0xfff, uint16(nanos>>4)&0xfff, nanos&0xffffffffffff)
}

func flowIDFor(eventID string) string {
	return issueevents.ResolveFlowID(sdkgo.TriggerEvent[linear.IssueEvent]{ID: eventID})
}

// waitForRecorded waits for the event's Flow to complete and returns its recorded issue.
func waitForRecorded(t *testing.T, ctx context.Context, client *dex.Client, eventID string) issueevents.RecordedIssue {
	t.Helper()
	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for {
		result, err := client.WaitForFlow(waitCtx, flowIDFor(eventID), dex.WaitForFlowOptions{NeedsResults: true})
		if err == nil {
			require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
			var recorded issueevents.RecordedIssue
			require.NoError(t, result.DecodeSingleOutput(&recorded))
			return recorded
		}
		var notFound *dex.FlowNotFoundError
		require.True(t, errors.As(err, &notFound) && waitCtx.Err() == nil, "the event's Flow must complete: %v", err)
		// Delivery to Dex is asynchronous after the 200, so the Flow may not exist yet.
		time.Sleep(50 * time.Millisecond)
	}
}

func requireNoFlow(t *testing.T, ctx context.Context, client *dex.Client, eventID string) {
	t.Helper()
	_, err := client.WaitForFlow(ctx, flowIDFor(eventID), dex.WaitForFlowOptions{})
	var notFound *dex.FlowNotFoundError
	require.ErrorAs(t, err, &notFound, "event %s must not start a Flow", eventID)
}

// startWorkerAndClient runs a Worker for flow against the Dex Server until the test ends and returns its Client.
func startWorkerAndClient(t *testing.T, flow *issueevents.Flow) *dex.Client {
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
