// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package telegram

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

const sentMessageBody = `{"ok":true,"result":{"message_id":52,"date":1794110000,"chat":{"id":-1001234567890}}}`

var testSendInput = SendMessageInput{ChatID: "@meridian_team", Text: "Hello from Dex"}

// newScriptedServer answers each send with the next status and body, counting requests.
func newScriptedServer(t *testing.T, statuses []int, bodies []string) (*httptest.Server, *int) {
	t.Helper()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/bot"+testBotToken+"/sendMessage", r.URL.Path)
		require.Equal(t, "@meridian_team", r.PostFormValue("chat_id"))
		require.Equal(t, "Hello from Dex", r.PostFormValue("text"))
		index := min(requests, len(statuses)-1)
		requests++
		w.WriteHeader(statuses[index])
		_, err := fmt.Fprint(w, bodies[index])
		require.NoError(t, err)
	}))
	t.Cleanup(server.Close)
	return server, &requests
}

func lastMarker(t *testing.T, ctx *singledispatchtest.Context) singledispatch.Marker {
	t.Helper()
	heartbeats := ctx.Heartbeats()
	require.NotEmpty(t, heartbeats)
	var marker singledispatch.Marker
	require.NoError(t, json.Unmarshal(heartbeats[len(heartbeats)-1], &marker))
	return marker
}

func TestSendMessageNeverResendsConfirmedMessage(t *testing.T) {
	server, requests := newScriptedServer(t, []int{http.StatusOK}, []string{sentMessageBody})
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("send-flow", "send-step")
	sent, err := sdkgo.RunMutation(ctx, client.SendMessage(), testConnection, testSendInput)
	require.NoError(t, err)
	require.Equal(t, SendMessageBranchSent, sent.Branch)
	require.EqualValues(t, 52, sent.Value.MessageID)
	ctx.AdvanceAttempt()
	uncertain, err := sdkgo.RunMutation(ctx, client.SendMessage(), testConnection, testSendInput)
	require.NoError(t, err)
	require.Equal(t, SendMessageBranchUncertain, uncertain.Branch)
	require.Equal(t, 1, *requests)
}

func TestSendMessageRetries429WithCheckpoint(t *testing.T) {
	server, requests := newScriptedServer(t, []int{http.StatusTooManyRequests, http.StatusOK}, []string{`{"ok":false,"error_code":429}`, sentMessageBody})
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("retry-flow", "send-step")
	_, err := sdkgo.RunMutation(ctx, client.SendMessage(), testConnection, testSendInput)
	require.Error(t, err)
	require.Equal(t, singledispatch.StateCanResend, lastMarker(t, ctx).State)
	ctx.AdvanceAttempt()
	sent, err := sdkgo.RunMutation(ctx, client.SendMessage(), testConnection, testSendInput)
	require.NoError(t, err)
	require.Equal(t, SendMessageBranchSent, sent.Branch)
	require.Equal(t, 2, *requests)
}

func TestSendMessageResendsUnknownOutcomeBelowCap(t *testing.T) {
	for _, status := range []int{http.StatusInternalServerError, http.StatusRequestTimeout, http.StatusConflict, http.StatusTooEarly, http.StatusTeapot} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server, requests := newScriptedServer(t, []int{status, http.StatusOK}, []string{`{"ok":false}`, sentMessageBody})
			client := newTestClient(t, server.URL)
			ctx := singledispatchtest.NewContext("resend-flow", "send-step")
			_, err := sdkgo.RunMutation(ctx, client.SendMessage(), testConnection, testSendInput)
			require.Error(t, err)
			require.Equal(t, singledispatch.StateClaimed, lastMarker(t, ctx).State)
			ctx.AdvanceAttempt()
			sent, err := sdkgo.RunMutation(ctx, client.SendMessage(), testConnection, testSendInput)
			require.NoError(t, err)
			require.Equal(t, SendMessageBranchSent, sent.Branch)
			require.EqualValues(t, 2, lastMarker(t, ctx).SendCount)
			require.Equal(t, 2, *requests)
		})
	}
}

func TestSendMessageResendsMalformedSuccessBelowCap(t *testing.T) {
	server, requests := newScriptedServer(t, []int{http.StatusOK, http.StatusOK}, []string{`not JSON`, sentMessageBody})
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("malformed-flow", "send-step")
	_, err := sdkgo.RunMutation(ctx, client.SendMessage(), testConnection, testSendInput)
	require.Error(t, err)
	ctx.AdvanceAttempt()
	sent, err := sdkgo.RunMutation(ctx, client.SendMessage(), testConnection, testSendInput)
	require.NoError(t, err)
	require.Equal(t, SendMessageBranchSent, sent.Branch)
	require.Equal(t, 2, *requests)
}

func TestSendMessageSelectsUncertainAtCap(t *testing.T) {
	server, requests := newScriptedServer(t, []int{http.StatusInternalServerError}, []string{`{"ok":false}`})
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("cap-flow", "send-step")
	for attempt := 1; attempt < 3; attempt++ {
		_, err := sdkgo.RunMutation(ctx, client.SendMessage(), testConnection, testSendInput)
		require.Error(t, err, "attempt %d resends", attempt)
		ctx.AdvanceAttempt()
	}
	uncertain, err := sdkgo.RunMutation(ctx, client.SendMessage(), testConnection, testSendInput)
	require.NoError(t, err)
	require.Equal(t, SendMessageBranchUncertain, uncertain.Branch)
	require.Equal(t, 3, *requests)
	ctx.AdvanceAttempt()
	refused, err := sdkgo.RunMutation(ctx, client.SendMessage(), testConnection, testSendInput)
	require.NoError(t, err)
	require.Equal(t, SendMessageBranchUncertain, refused.Branch)
	require.Equal(t, 3, *requests)
}

func TestSendMessageHonorsApplicationSendCap(t *testing.T) {
	server, requests := newScriptedServer(t, []int{http.StatusBadGateway}, []string{`{"ok":false}`})
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("override-flow", "send-step")
	uncertain, err := sdkgo.RunMutation(ctx, client.SendMessage(), testConnection, testSendInput, sdkgo.WithMaxSends(1))
	require.NoError(t, err)
	require.Equal(t, SendMessageBranchUncertain, uncertain.Branch)
	require.Equal(t, 1, *requests)
}

func TestSendMessageNeverResendsDefiniteRefusal(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server, requests := newScriptedServer(t, []int{status}, []string{fmt.Sprintf(`{"ok":false,"error_code":%d,"description":"Bad Request: chat not found"}`, status)})
			client := newTestClient(t, server.URL)
			ctx := singledispatchtest.NewContext("refusal-flow", "send-step")
			rejected, err := sdkgo.RunMutation(ctx, client.SendMessage(), testConnection, testSendInput)
			require.NoError(t, err)
			require.Equal(t, SendMessageBranchProviderRejected, rejected.Branch)
			require.NotContains(t, rejected.Failure.Message, testBotToken)
			require.Equal(t, singledispatch.StateCanResend, lastMarker(t, ctx).State)
			require.Equal(t, 1, *requests)
		})
	}
}

func TestSendMessageRefusalOfResendSelectsUncertain(t *testing.T) {
	server, requests := newScriptedServer(t, []int{http.StatusInternalServerError, http.StatusBadRequest}, []string{`{"ok":false}`, `{"ok":false,"error_code":400}`})
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("refused-resend-flow", "send-step")
	_, err := sdkgo.RunMutation(ctx, client.SendMessage(), testConnection, testSendInput)
	require.Error(t, err)
	ctx.AdvanceAttempt()
	uncertain, err := sdkgo.RunMutation(ctx, client.SendMessage(), testConnection, testSendInput)
	require.NoError(t, err)
	require.Equal(t, SendMessageBranchUncertain, uncertain.Branch)
	require.Equal(t, 2, *requests)
}

func TestSendMessageAcceptedButIncompleteResponseIsNeverResent(t *testing.T) {
	server, requests := newScriptedServer(t, []int{http.StatusOK}, []string{`{"ok":true,"result":{"message_id":52}}`})
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("incomplete-flow", "send-step")
	uncertain, err := sdkgo.RunMutation(ctx, client.SendMessage(), testConnection, testSendInput)
	require.NoError(t, err)
	require.Equal(t, SendMessageBranchUncertain, uncertain.Branch)
	require.Equal(t, 1, *requests)
}

func TestSendMessageFailClosedReturnsRetry(t *testing.T) {
	server, requests := newScriptedServer(t, []int{http.StatusOK}, []string{sentMessageBody})
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("fail-closed-flow", "send-step")
	ctx.SetReadError(errors.New("heartbeat unavailable"))
	_, err := sdkgo.RunMutation(ctx, client.SendMessage(), testConnection, testSendInput)
	require.Error(t, err)
	require.Equal(t, 0, *requests)
	ctx.SetReadError(nil)
	sent, err := sdkgo.RunMutation(ctx, client.SendMessage(), testConnection, testSendInput)
	require.NoError(t, err)
	require.Equal(t, SendMessageBranchSent, sent.Branch)
}

func TestSendMessageCredentialFailureAfterPossibleSendRetries(t *testing.T) {
	server, requests := newScriptedServer(t, []int{http.StatusInternalServerError}, []string{`{"ok":false}`})
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("credential-flow", "send-step")
	_, err := sdkgo.RunMutation(ctx, client.SendMessage(), testConnection, testSendInput)
	require.Error(t, err)
	ctx.AdvanceAttempt()
	noCredentials, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.NoError(t, err)
	_, err = sdkgo.RunMutation(ctx, noCredentials.SendMessage(), testConnection, testSendInput)
	require.Error(t, err)
	require.Equal(t, 1, *requests)
	defect, err := sdkgo.RunMutation(singledispatchtest.NewContext("first-credential-flow", "send-step"), noCredentials.SendMessage(), testConnection, testSendInput)
	require.NoError(t, err)
	require.Equal(t, SendMessageBranchDefect, defect.Branch)
}
