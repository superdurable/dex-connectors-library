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
	"github.com/superdurable/dex-connectors-library/connectors/helpscout"
	conversationtriage "github.com/superdurable/dex-connectors-library/connectors/helpscout/examples/conversation-triage/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

// TestSignedNewConversationIsTriagedOnceAndDuplicatesAndForgeriesStartNoneWithRealDex covers README steps 5 and 6.
func TestSignedNewConversationIsTriagedOnceAndDuplicatesAndForgeriesStartNoneWithRealDex(t *testing.T) {
	fake := newFakeHelpScout(t)
	logs := newRecordedLogs(t)
	credentials := newExampleCredentials()
	connection := newExampleConnection(t, credentials, fake.connectionOption(), helpscout.WithLogger(logs.logger()))
	flow := conversationtriage.NewFlow(connection)
	client := startWorkerAndClient(t, flow)
	endpoint := newConversationEndpoint(t, connection, newConversationTarget(client, flow, logs.logger()))
	endpoint.start(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	conversationID := uniqueConversationID()
	body := conversationWebhookBody(conversationID, triagedInboxID, "active")

	require.Equal(t, http.StatusOK, endpoint.deliverWebhook(t, helpscout.WebhookEventConversationCreated, body, sentinelWebhookSecret, false))
	eventID := waitForOneDeliveredEvent(t, logs, conversationID)
	triage := waitForTriage(t, ctx, client, eventID)
	require.Equal(t, "completed", triage.Stage)
	require.Equal(t, helpscout.ConversationStatusActive, triage.Status)
	require.Equal(t, []string{"customer"}, triage.NewestThreadTypes)
	require.Equal(t, customerEmail, triage.CustomerEmail)
	require.Equal(t, []int64{1001, 1002}, triage.CustomerProfileIDs, "both duplicate profiles are reported")
	require.Equal(t, []int64{otherActiveConv}, triage.OtherActiveConversationIDs)
	require.Equal(t, conversationID*10+1, triage.NoteThreadID)
	require.Equal(t, []string{"billing", conversationtriage.TriagedTag, conversationtriage.RepeatContactTag}, triage.Tags)
	require.Equal(t, []string{conversationtriage.BuildTriageNote(customerEmail, []int64{1001, 1002}, []int64{otherActiveConv})},
		fake.notesFor(conversationID), "one internal note")
	require.Equal(t, triage.Tags, fake.tagsFor(conversationID))
	require.Empty(t, fake.notesFor(otherActiveConv), "the other conversation is untouched")
	stored, expiresAt := credentials.Current()
	require.Equal(t, sentinelToken, stored.AccessToken.Reveal(), "the token obtained from the App ID and App Secret is stored")
	require.NotNil(t, expiresAt)
	require.WithinDuration(t, time.Now().Add(48*time.Hour), *expiresAt, time.Minute)
	require.Equal(t, 1, fake.requestCount(http.MethodPost, "/v2/oauth2/token"))

	// Help Scout redelivers the same body: the event ID repeats, so the Flow start is a duplicate.
	require.Equal(t, http.StatusOK, endpoint.deliverWebhook(t, helpscout.WebhookEventConversationCreated, body, sentinelWebhookSecret, false))
	require.Eventually(t, func() bool {
		return len(logs.find("trigger event delivered", map[string]string{"event_id": eventID, "duplicate": "true"})) == 1
	}, 20*time.Second, 25*time.Millisecond, "the redelivery reaches Dex as a duplicate start")
	require.Len(t, fake.notesFor(conversationID), 1, "no second note")

	// A forged delivery answers 400, and a tags event is filtered by the application; neither starts a Flow.
	forgedID := uniqueConversationID()
	require.Equal(t, http.StatusBadRequest, endpoint.deliverWebhook(t, helpscout.WebhookEventConversationCreated,
		conversationWebhookBody(forgedID, triagedInboxID, "active"), sentinelWebhookSecret, true))
	require.Equal(t, http.StatusBadRequest, endpoint.deliverWebhook(t, helpscout.WebhookEventConversationCreated,
		conversationWebhookBody(forgedID, triagedInboxID, "active"), "not-the-webhook-secret", false))
	require.Equal(t, http.StatusOK, endpoint.deliverWebhook(t, helpscout.WebhookEventConversationTagsUpdated, body, sentinelWebhookSecret, false))
	require.Eventually(t, func() bool {
		return len(logs.find("trigger event skipped: filtered", nil)) == 1
	}, 20*time.Second, 25*time.Millisecond, "AcceptNewConversation consumes the tags event without a Flow")
	laterID := uniqueConversationID()
	require.Equal(t, http.StatusOK, endpoint.deliverWebhook(t, helpscout.WebhookEventConversationCreated,
		conversationWebhookBody(laterID, triagedInboxID, "active"), sentinelWebhookSecret, false))
	require.Equal(t, "completed", waitForTriage(t, ctx, client, waitForOneDeliveredEvent(t, logs, laterID)).Stage)
	require.Zero(t, fake.requestCount(http.MethodGet, fmt.Sprintf("/v2/conversations/%d", forgedID)))
	require.Equal(t, 1, fake.requestCount(http.MethodPost, "/v2/oauth2/token"), "the second Flow reuses the stored token")
	require.NotContains(t, logs.text(), sentinelWebhookSecret)
	require.NotContains(t, logs.text(), sentinelToken)
}

// TestDeliveryBeforeTheBindingRunsIsAnswered503AndTheRetryStartsTheFlowWithRealDex: while the binding is not
// running, the endpoint answers 503, so Help Scout retries, and the retry that arrives once it runs starts the Flow.
func TestDeliveryBeforeTheBindingRunsIsAnswered503AndTheRetryStartsTheFlowWithRealDex(t *testing.T) {
	fake := newFakeHelpScout(t)
	logs := newRecordedLogs(t)
	connection := newExampleConnection(t, newExampleCredentials(), fake.connectionOption(), helpscout.WithLogger(logs.logger()))
	flow := conversationtriage.NewFlow(connection)
	client := startWorkerAndClient(t, flow)
	endpoint := newConversationEndpoint(t, connection, newConversationTarget(client, flow, logs.logger()))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	conversationID := uniqueConversationID()
	body := conversationWebhookBody(conversationID, triagedInboxID, "active")

	require.Equal(t, http.StatusServiceUnavailable,
		endpoint.deliverWebhook(t, helpscout.WebhookEventConversationCreated, body, sentinelWebhookSecret, false),
		"no binding runs yet, so Help Scout must retry")
	require.Empty(t, logs.find("trigger event delivered", nil))

	endpoint.start(t)
	require.Equal(t, http.StatusOK,
		endpoint.deliverWebhook(t, helpscout.WebhookEventConversationCreated, body, sentinelWebhookSecret, false), "Help Scout's retry")
	require.Equal(t, "completed", waitForTriage(t, ctx, client, waitForOneDeliveredEvent(t, logs, conversationID)).Stage)
	require.Len(t, fake.notesFor(conversationID), 1)
}

func dexAddress() string {
	return environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")
}

// uniqueConversationID keeps Flow IDs unique across runs against one Dex Server.
func uniqueConversationID() int64 {
	return time.Now().UnixNano() / 1000
}

// waitForOneDeliveredEvent returns the event ID of the conversation's first delivery to Dex.
func waitForOneDeliveredEvent(t *testing.T, logs *recordedLogs, conversationID int64) string {
	t.Helper()
	prefix := fmt.Sprintf("%s:%d:", helpscout.WebhookEventConversationCreated, conversationID)
	var eventID string
	require.Eventually(t, func() bool {
		for _, record := range logs.find("trigger event delivered", map[string]string{"duplicate": "false"}) {
			if candidate := record.attrs["event_id"]; len(candidate) > len(prefix) && candidate[:len(prefix)] == prefix {
				eventID = candidate
				return true
			}
		}
		return false
	}, 20*time.Second, 25*time.Millisecond, "conversation %d must start a Flow", conversationID)
	return eventID
}

// waitForTriage waits for the conversation's Flow to complete and returns its triage record.
func waitForTriage(t *testing.T, ctx context.Context, client *dex.Client, eventID string) conversationtriage.Triage {
	t.Helper()
	waitCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	flowID := conversationtriage.ResolveFlowID(sdkgo.TriggerEvent[helpscout.ConversationEvent]{ID: eventID})
	for {
		result, err := client.WaitForFlow(waitCtx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
		if err == nil {
			require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
			var triage conversationtriage.Triage
			require.NoError(t, result.DecodeSingleOutput(&triage))
			return triage
		}
		var notFound *dex.FlowNotFoundError
		require.True(t, errors.As(err, &notFound) && waitCtx.Err() == nil, "the conversation's Flow must complete: %v", err)
		// Delivery to Dex is asynchronous after the 200, so the Flow may not exist yet.
		time.Sleep(50 * time.Millisecond)
	}
}

// startWorkerAndClient runs a Worker for flow against the Dex Server until the test ends and returns its Client.
func startWorkerAndClient(t *testing.T, flow *conversationtriage.Flow) *dex.Client {
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
