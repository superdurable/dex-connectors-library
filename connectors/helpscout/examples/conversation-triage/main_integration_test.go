//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
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
	setup := newExampleSetup(t, dexAddress())
	running := setup.startExample(t, fake.connectionOption())
	client := newInspectionClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	conversationID := uniqueConversationID()
	body := conversationWebhookBody(conversationID, triagedInboxID, "active")

	require.Equal(t, http.StatusOK, setup.deliverWebhook(t, helpscout.WebhookEventConversationCreated, body, sentinelWebhookSecret, false))
	eventID := waitForOneDeliveredEvent(t, setup, conversationID)
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
	storedToken, expiresAt := setup.storedAccessToken(t)
	require.Equal(t, sentinelToken, storedToken, "the token obtained from the App ID and App Secret is stored")
	require.WithinDuration(t, time.Now().Add(48*time.Hour), expiresAt, time.Minute)
	require.Equal(t, 1, fake.requestCount(http.MethodPost, "/v2/oauth2/token"))

	// Help Scout redelivers the same body: the event ID repeats, so the Flow start is a duplicate.
	require.Equal(t, http.StatusOK, setup.deliverWebhook(t, helpscout.WebhookEventConversationCreated, body, sentinelWebhookSecret, false))
	require.Eventually(t, func() bool {
		return len(setup.logs.find("trigger event delivered", map[string]string{"event_id": eventID, "duplicate": "true"})) == 1
	}, 20*time.Second, 25*time.Millisecond, "the redelivery reaches Dex as a duplicate start")
	require.Len(t, fake.notesFor(conversationID), 1, "no second note")

	// A forged delivery answers 400, and a tags event is filtered by the application; neither starts a Flow.
	forgedID := uniqueConversationID()
	require.Equal(t, http.StatusBadRequest, setup.deliverWebhook(t, helpscout.WebhookEventConversationCreated,
		conversationWebhookBody(forgedID, triagedInboxID, "active"), sentinelWebhookSecret, true))
	require.Equal(t, http.StatusBadRequest, setup.deliverWebhook(t, helpscout.WebhookEventConversationCreated,
		conversationWebhookBody(forgedID, triagedInboxID, "active"), "not-the-webhook-secret", false))
	require.Equal(t, http.StatusOK, setup.deliverWebhook(t, helpscout.WebhookEventConversationTagsUpdated, body, sentinelWebhookSecret, false))
	require.Eventually(t, func() bool {
		return len(setup.logs.find("trigger event skipped: filtered", nil)) == 1
	}, 20*time.Second, 25*time.Millisecond, "AcceptNewConversation consumes the tags event without a Flow")
	laterID := uniqueConversationID()
	require.Equal(t, http.StatusOK, setup.deliverWebhook(t, helpscout.WebhookEventConversationCreated,
		conversationWebhookBody(laterID, triagedInboxID, "active"), sentinelWebhookSecret, false))
	require.Equal(t, "completed", waitForTriage(t, ctx, client, waitForOneDeliveredEvent(t, setup, laterID)).Stage)
	require.Zero(t, fake.requestCount(http.MethodGet, fmt.Sprintf("/v2/conversations/%d", forgedID)))
	require.Equal(t, 1, fake.requestCount(http.MethodPost, "/v2/oauth2/token"), "the second Flow reuses the stored token")
	require.Empty(t, setup.pendingEventIDs(t))

	running.stop(t)
	require.NotContains(t, setup.logs.text(), sentinelWebhookSecret)
	require.NotContains(t, setup.logs.text(), sentinelToken)
}

// TestRestartReplaysADeliveryRecordedButNotDeliveredWithRealDex covers README step 7: Dex is down while the
// endpoint acknowledges a webhook, the process stops, and the next run delivers it.
func TestRestartReplaysADeliveryRecordedButNotDeliveredWithRealDex(t *testing.T) {
	fake := newFakeHelpScout(t)
	reachableDex := dexAddress()
	unreachableDex := "unix://" + filepath.Join(os.TempDir(), fmt.Sprintf("dex-missing-%d.sock", time.Now().UnixNano()))
	setup := newExampleSetup(t, unreachableDex)
	conversationID := uniqueConversationID()

	firstRun := setup.startExample(t, fake.connectionOption())
	require.Equal(t, http.StatusOK, setup.deliverWebhook(t, helpscout.WebhookEventConversationCreated,
		conversationWebhookBody(conversationID, triagedInboxID, "active"), sentinelWebhookSecret, false))
	firstRun.stop(t)
	pending := setup.pendingEventIDs(t)
	require.Len(t, pending, 1, "acknowledged but never delivered")

	t.Setenv("DEX_FLOW_SERVICE_ADDRESS", reachableDex)
	secondRun := setup.startExample(t, fake.connectionOption())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	require.Equal(t, "completed", waitForTriage(t, ctx, newInspectionClient(t), pending[0]).Stage)
	require.Len(t, fake.notesFor(conversationID), 1)
	require.Eventually(t, func() bool { return len(setup.pendingEventIDs(t)) == 0 }, 10*time.Second, 25*time.Millisecond)
	require.Len(t, setup.logs.find("replaying pending trigger events", map[string]string{"count": "1"}), 1)
	secondRun.stop(t)
}

func dexAddress() string {
	return environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")
}

// uniqueConversationID keeps Flow IDs unique across runs against one Dex Server.
func uniqueConversationID() int64 {
	return time.Now().UnixNano() / 1000
}

// waitForOneDeliveredEvent returns the event ID of the conversation's first delivery to Dex.
func waitForOneDeliveredEvent(t *testing.T, setup *exampleSetup, conversationID int64) string {
	t.Helper()
	prefix := fmt.Sprintf("%s:%d:", helpscout.WebhookEventConversationCreated, conversationID)
	var eventID string
	require.Eventually(t, func() bool {
		for _, record := range setup.logs.find("trigger event delivered", map[string]string{"duplicate": "false"}) {
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

// newInspectionClient waits for and inspects Flows; it registers no Worker.
func newInspectionClient(t *testing.T) *dex.Client {
	t.Helper()
	inspectionClient, err := helpscout.New(helpscout.Config{}, sdkgo.StaticCredentialProvider[helpscout.Credentials]{})
	require.NoError(t, err)
	connection, err := helpscout.NewConnection(inspectionClient, sdkgo.ConnectionRef{Provider: "helpscout", Name: conversationtriage.ConnectionName})
	require.NoError(t, err)
	registry, err := dex.NewRegistry([]dex.Flow{conversationtriage.NewFlow(connection)})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "inspection-blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: dexAddress()})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, errors.Join(client.Close(), cache.Close())) })
	return client
}
