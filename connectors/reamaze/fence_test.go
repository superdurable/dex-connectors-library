// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package reamaze_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/reamaze"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

func TestCreateConversationFenceRetries503OnceWithTheSameKey(t *testing.T) {
	provider := newRecordingReamaze(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			writeJSON(t, response, http.StatusServiceUnavailable, `{}`)
			return
		}
		writeValue(t, response, http.StatusCreated, conversationJSON("double-charge", 0, nil))
	})
	ctx := singledispatchtest.NewContext("reamaze-flow", "create-fence-503")
	client := newReamazeClient(t, provider.URL)
	_, err := sdkgo.RunMutation(ctx, client.CreateConversation(), reamazeConnection, validCreateConversationInput())
	require.Error(t, err)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.CreateConversation(), reamazeConnection, validCreateConversationInput())
	require.NoError(t, err)
	require.Equal(t, reamaze.CreateConversationBranchCreated, result.Branch)
	require.Equal(t, 2, provider.requestCount())
	var first, second map[string]any
	require.NoError(t, json.Unmarshal([]byte(provider.request(0).body), &first))
	require.NoError(t, json.Unmarshal([]byte(provider.request(1).body), &second))
	require.Equal(t, first["conversation"].(map[string]any)["data"], second["conversation"].(map[string]any)["data"])
}

func TestCreateConversationFenceDoesNotUseAStaleResendMarker(t *testing.T) {
	provider := newRecordingReamaze(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusServiceUnavailable, `{}`)
	})
	ctx := singledispatchtest.NewContext("reamaze-flow", "create-stale-marker")
	client := newReamazeClient(t, provider.URL)
	_, err := sdkgo.RunMutation(ctx, client.CreateConversation(), reamazeConnection, validCreateConversationInput())
	require.Error(t, err)
	ctx.AdvanceAttempt()
	ctx.AdvanceAttempt()
	_, err = sdkgo.RunMutation(ctx, client.CreateConversation(), reamazeConnection, validCreateConversationInput())
	require.Error(t, err)
	require.Equal(t, 2, provider.requestCount())
	require.Equal(t, http.MethodGet, provider.request(1).method)
}

func TestCreateConversationFenceRecordsNotSentAfterCredentialFailure(t *testing.T) {
	provider := newRecordingReamaze(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeValue(t, response, http.StatusCreated, conversationJSON("double-charge", 0, nil))
	})
	client, credentials := newSwitchingReamazeClient(t, provider.URL)
	credentials.resolveErr = errors.New("credential storage unavailable")
	ctx := singledispatchtest.NewContext("reamaze-flow", "create-credential-retry")
	_, err := sdkgo.RunMutation(ctx, client.CreateConversation(), reamazeConnection, validCreateConversationInput())
	require.Error(t, err)
	require.Zero(t, provider.requestCount())
	credentials.resolveErr = nil
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.CreateConversation(), reamazeConnection, validCreateConversationInput())
	require.NoError(t, err)
	require.Equal(t, reamaze.CreateConversationBranchCreated, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestCreateConversationFenceReadsBackAcceptedWriteWithoutResending(t *testing.T) {
	var key string
	provider := newRecordingReamaze(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodPost {
			var payload struct {
				Conversation struct {
					Data map[string]string `json:"data"`
				} `json:"conversation"`
			}
			require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
			key = payload.Conversation.Data[reamaze.DispatchKeyDataAttribute]
			writeValue(t, response, http.StatusCreated, conversationJSON("double-charge", 0, nil))
			return
		}
		conversation := conversationJSON("double-charge", 0, nil)
		conversation["data"] = map[string]string{reamaze.DispatchKeyDataAttribute: key}
		writeValue(t, response, http.StatusOK, conversation)
	})
	ctx := singledispatchtest.NewContext("reamaze-flow", "create-accepted")
	client := newReamazeClient(t, provider.URL)
	_, err := sdkgo.RunMutation(ctx, client.CreateConversation(), reamazeConnection, validCreateConversationInput())
	require.NoError(t, err)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.CreateConversation(), reamazeConnection, validCreateConversationInput())
	require.NoError(t, err)
	require.True(t, result.Value.WasAlreadyApplied)
	require.Equal(t, 2, provider.requestCount())
	require.Equal(t, http.MethodGet, provider.request(1).method)
}

func TestCreateConversationFenceStopsBeforeAnExpiredAttemptDeadline(t *testing.T) {
	provider := newRecordingReamaze(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeValue(t, response, http.StatusCreated, conversationJSON("double-charge", 0, nil))
	})
	ctx := singledispatchtest.NewContext("reamaze-flow", "create-expired-deadline")
	cancel := ctx.SetDeadline(time.Now().Add(-time.Second))
	defer cancel()
	_, err := sdkgo.RunMutation(ctx, newReamazeClient(t, provider.URL).CreateConversation(), reamazeConnection, validCreateConversationInput())
	require.Error(t, err)
	require.Zero(t, provider.requestCount())
}

func TestReplyFenceRetries503WithTheSameOriginID(t *testing.T) {
	provider := newRecordingReamaze(t, func(response http.ResponseWriter, request *http.Request, index int) {
		if index == 0 {
			writeJSON(t, response, http.StatusServiceUnavailable, `{}`)
			return
		}
		var payload struct {
			Message struct {
				OriginID string `json:"origin_id"`
			} `json:"message"`
		}
		require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		writeValue(t, response, http.StatusCreated, messageJSON("Dex triage note.", 1, payload.Message.OriginID))
	})
	ctx := singledispatchtest.NewContext("reamaze-flow", "reply-fence-503")
	client := newReamazeClient(t, provider.URL)
	input := reamaze.ReplyToConversationInput{ConversationID: "double-charge", Text: "Dex triage note.", IsInternalNote: true}
	_, err := sdkgo.RunMutation(ctx, client.ReplyToConversation(), reamazeConnection, input)
	require.Error(t, err)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.ReplyToConversation(), reamazeConnection, input)
	require.NoError(t, err)
	require.Equal(t, reamaze.ReplyToConversationBranchReplied, result.Branch)
	require.Equal(t, 2, provider.requestCount())
	var first, second map[string]any
	require.NoError(t, json.Unmarshal([]byte(provider.request(0).body), &first))
	require.NoError(t, json.Unmarshal([]byte(provider.request(1).body), &second))
	require.Equal(t, first["message"].(map[string]any)["origin_id"], second["message"].(map[string]any)["origin_id"])
}

func TestReplyFenceLostClaimFailsClosedWithoutAnotherPost(t *testing.T) {
	provider := newRecordingReamaze(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodPost {
			dropConnection(t, response)
			return
		}
		writeValue(t, response, http.StatusOK, pageJSON("messages", []any{}, 0))
	})
	ctx := singledispatchtest.NewContext("reamaze-flow", "reply-lost-claim")
	ctx.LoseNextHeartbeat()
	client := newReamazeClient(t, provider.URL)
	input := reamaze.ReplyToConversationInput{ConversationID: "double-charge", Text: "Dex triage note."}
	_, err := sdkgo.RunMutation(ctx, client.ReplyToConversation(), reamazeConnection, input)
	require.Error(t, err)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.ReplyToConversation(), reamazeConnection, input)
	require.NoError(t, err)
	require.Equal(t, reamaze.ReplyToConversationBranchUncertain, result.Branch)
	require.Equal(t, 2, provider.requestCount())
	require.Equal(t, http.MethodGet, provider.request(1).method)
}

func TestCreateConversationFenceCutsOffAMidRequestDeadline(t *testing.T) {
	provider := newRecordingReamaze(t, func(_ http.ResponseWriter, request *http.Request, _ int) {
		<-request.Context().Done()
	})
	ctx := singledispatchtest.NewContext("reamaze-flow", "create-mid-request-deadline")
	cancel := ctx.SetDeadline(time.Now().Add(400 * time.Millisecond))
	defer cancel()
	startedAt := time.Now()
	_, err := sdkgo.RunMutation(ctx, newReamazeClient(t, provider.URL).CreateConversation(), reamazeConnection, validCreateConversationInput())
	require.Error(t, err)
	require.Less(t, time.Since(startedAt), 400*time.Millisecond)
	require.Equal(t, 1, provider.requestCount())
}

func TestFencedWriteIsNotReplayedAfterAReusedConnectionLosesItsReply(t *testing.T) {
	provider := newRecordingReamaze(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodGet {
			writeValue(t, response, http.StatusOK, pageJSON("contacts", []any{}, 0))
			return
		}
		dropConnection(t, response)
	})
	client := newReamazeClient(t, provider.URL)
	_, err := sdkgo.RunQuery(newReamazeDexContext("prime-connection"), client.FindContactByEmail(), reamazeConnection,
		reamaze.FindContactByEmailInput{Email: "jane@acme.example.com"})
	require.NoError(t, err)
	_, err = sdkgo.RunMutation(singledispatchtest.NewContext("reamaze-flow", "create-reused-connection"), client.CreateConversation(), reamazeConnection, validCreateConversationInput())
	require.Error(t, err)
	require.Equal(t, 2, provider.requestCount())
	require.Equal(t, http.MethodPost, provider.request(1).method)
}
