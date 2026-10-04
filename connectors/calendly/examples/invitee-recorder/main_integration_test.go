//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/calendly"
	inviteerecorder "github.com/superdurable/dex-connectors-library/connectors/calendly/examples/invitee-recorder/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

// TestSignedDeliveryStartsOneFlowAndDuplicatesAndForgeriesStartNoneWithRealDex covers README steps 5 and 6.
func TestSignedDeliveryStartsOneFlowAndDuplicatesAndForgeriesStartNoneWithRealDex(t *testing.T) {
	fake := newFakeCalendly(t)
	logs := newRecordedLogs(t)
	connection := newExampleConnection(t, fake.connectionOption(), calendly.WithLogger(logs.logger()))
	flow := inviteerecorder.NewFlow(connection)
	client := startWorkerAndClient(t, flow)
	endpoint := newInviteeEndpoint(t, connection, newInviteeTarget(client, flow, logs.logger()))
	endpoint.start(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	eventID, inviteeID := uniqueID("EVT"), uniqueID("INV")
	triggerEventID := "invitee.created:" + eventID + ":" + inviteeID
	body := inviteeWebhookBody(calendly.WebhookEventInviteeCreated, eventID, inviteeID, bookedEventTypeURI)

	require.Equal(t, http.StatusOK, endpoint.deliverWebhook(t, body, sentinelSigningKey, false))
	recorded := waitForRecorded(t, ctx, client, triggerEventID)
	require.Equal(t, calendly.GetScheduledEventBranchFound, recorded.Branch)
	require.Equal(t, "https://api.calendly.com/scheduled_events/"+eventID, recorded.ScheduledEvent.URI)
	require.Equal(t, time.Date(2026, time.October, 2, 17, 0, 0, 0, time.UTC), recorded.ScheduledEvent.StartTime)
	require.Equal(t, bookedEventTypeURI, recorded.ScheduledEvent.EventTypeURI)
	require.Equal(t, 1, fake.readCount(eventID), "the Flow read the scheduled event once")

	// Calendly redelivers the same webhook: the inbox and the Flow start both deduplicate it.
	require.Equal(t, http.StatusOK, endpoint.deliverWebhook(t, body, sentinelSigningKey, false))
	require.Eventually(t, func() bool {
		return len(logs.find("trigger event delivered", map[string]string{"event_id": triggerEventID, "duplicate": "true"})) == 1
	}, 20*time.Second, 25*time.Millisecond, "the redelivery reaches Dex as a duplicate start")
	require.Len(t, logs.find("trigger event delivered", map[string]string{"event_id": triggerEventID, "duplicate": "false"}), 1)
	require.Equal(t, 1, fake.readCount(eventID), "no second Flow read the event")

	// A forged delivery answers 400, and a cancellation is filtered by the application; neither starts a Flow.
	forgedEventID := uniqueID("EVT")
	require.Equal(t, http.StatusBadRequest, endpoint.deliverWebhook(t,
		inviteeWebhookBody(calendly.WebhookEventInviteeCreated, forgedEventID, inviteeID, bookedEventTypeURI), sentinelSigningKey, true))
	require.Equal(t, http.StatusBadRequest, endpoint.deliverWebhook(t,
		inviteeWebhookBody(calendly.WebhookEventInviteeCreated, forgedEventID, inviteeID, bookedEventTypeURI), "not-the-signing-key", false))
	canceledID := "invitee.canceled:" + eventID + ":" + inviteeID
	require.Equal(t, http.StatusOK, endpoint.deliverWebhook(t,
		inviteeWebhookBody(calendly.WebhookEventInviteeCanceled, eventID, inviteeID, bookedEventTypeURI), sentinelSigningKey, false))
	require.Eventually(t, func() bool {
		return len(logs.find("trigger event skipped: filtered", map[string]string{"event_id": canceledID})) == 1
	}, 20*time.Second, 25*time.Millisecond, "AcceptBooking consumes the cancellation without a Flow")
	laterEventID, laterInviteeID := uniqueID("EVT"), uniqueID("INV")
	require.Equal(t, http.StatusOK, endpoint.deliverWebhook(t,
		inviteeWebhookBody(calendly.WebhookEventInviteeCreated, laterEventID, laterInviteeID, bookedEventTypeURI), sentinelSigningKey, false))
	waitForRecorded(t, ctx, client, "invitee.created:"+laterEventID+":"+laterInviteeID)
	requireNoFlow(t, ctx, client, "invitee.created:"+forgedEventID+":"+inviteeID)
	requireNoFlow(t, ctx, client, canceledID)
	require.Zero(t, fake.readCount(forgedEventID))
	require.NotContains(t, logs.text(), sentinelSigningKey)
	require.NotContains(t, logs.text(), sentinelToken)
}

// TestDeliveryBeforeTheBindingRunsIsAnswered503AndTheRetryStartsTheFlowWithRealDex: while the binding is not
// running, the endpoint answers 503, so Calendly retries, and the retry that arrives once it runs starts the Flow.
func TestDeliveryBeforeTheBindingRunsIsAnswered503AndTheRetryStartsTheFlowWithRealDex(t *testing.T) {
	fake := newFakeCalendly(t)
	logs := newRecordedLogs(t)
	connection := newExampleConnection(t, fake.connectionOption(), calendly.WithLogger(logs.logger()))
	flow := inviteerecorder.NewFlow(connection)
	client := startWorkerAndClient(t, flow)
	endpoint := newInviteeEndpoint(t, connection, newInviteeTarget(client, flow, logs.logger()))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	eventID, inviteeID := uniqueID("EVT"), uniqueID("INV")
	triggerEventID := "invitee.created:" + eventID + ":" + inviteeID
	body := inviteeWebhookBody(calendly.WebhookEventInviteeCreated, eventID, inviteeID, bookedEventTypeURI)

	require.Equal(t, http.StatusServiceUnavailable, endpoint.deliverWebhook(t, body, sentinelSigningKey, false),
		"no binding runs yet, so Calendly must retry")
	requireNoFlow(t, ctx, client, triggerEventID)

	endpoint.start(t)
	require.Equal(t, http.StatusOK, endpoint.deliverWebhook(t, body, sentinelSigningKey, false), "Calendly's retry")
	require.Equal(t, calendly.GetScheduledEventBranchFound, waitForRecorded(t, ctx, client, triggerEventID).Branch)
	require.Equal(t, 1, fake.readCount(eventID))
}

func dexAddress() string {
	return environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")
}

func uniqueID(prefix string) string {
	return prefix + strconv.FormatInt(time.Now().UnixNano(), 36)
}

func flowIDFor(triggerEventID string) string {
	return inviteerecorder.ResolveFlowID(sdkgo.TriggerEvent[calendly.InviteeEvent]{ID: triggerEventID})
}

// waitForRecorded waits for the booking's Flow to complete and returns its recorded scheduled event.
func waitForRecorded(t *testing.T, ctx context.Context, client *dex.Client, triggerEventID string) inviteerecorder.RecordedScheduledEvent {
	t.Helper()
	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for {
		result, err := client.WaitForFlow(waitCtx, flowIDFor(triggerEventID), dex.WaitForFlowOptions{NeedsResults: true})
		if err == nil {
			require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
			var recorded inviteerecorder.RecordedScheduledEvent
			require.NoError(t, result.DecodeSingleOutput(&recorded))
			return recorded
		}
		var notFound *dex.FlowNotFoundError
		require.True(t, errors.As(err, &notFound) && waitCtx.Err() == nil, "the booking's Flow must complete: %v", err)
		// Delivery to Dex is asynchronous after the 200, so the Flow may not exist yet.
		time.Sleep(50 * time.Millisecond)
	}
}

func requireNoFlow(t *testing.T, ctx context.Context, client *dex.Client, triggerEventID string) {
	t.Helper()
	_, err := client.WaitForFlow(ctx, flowIDFor(triggerEventID), dex.WaitForFlowOptions{})
	var notFound *dex.FlowNotFoundError
	require.ErrorAs(t, err, &notFound, "event %s must not start a Flow", triggerEventID)
}

// startWorkerAndClient runs a Worker for flow against the Dex Server until the test ends and returns its Client.
func startWorkerAndClient(t *testing.T, flow *inviteerecorder.Flow) *dex.Client {
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
