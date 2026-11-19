// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package discord_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/discord/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

var fencedCreateMessageInput = discord.CreateMessageInput{ChannelID: testChannelID, Content: "Release @everyone"}

// scriptedResponse is one fake Discord answer to a createMessage POST.
type scriptedResponse struct {
	status int
	body   string
}

// newScriptedDiscord answers each POST with the next scripted response, repeating the last one.
func newScriptedDiscord(t *testing.T, responses ...scriptedResponse) *recordingDiscord {
	t.Helper()
	var requestCount atomic.Int32
	return newRecordingDiscord(t, func(response http.ResponseWriter, _ recordedRequest) {
		index := min(int(requestCount.Add(1))-1, len(responses)-1)
		writeJSON(t, response, responses[index].status, responses[index].body)
	})
}

func lastCreateMessageMarker(t *testing.T, ctx *singledispatchtest.Context) singledispatch.Marker {
	t.Helper()
	heartbeats := ctx.Heartbeats()
	require.NotEmpty(t, heartbeats)
	var marker singledispatch.Marker
	require.NoError(t, json.Unmarshal(heartbeats[len(heartbeats)-1], &marker))
	return marker
}

// requireSameEnforcedNonce asserts every request carried one enforced nonce.
func requireSameEnforcedNonce(t *testing.T, requests []recordedRequest) {
	t.Helper()
	require.NotEmpty(t, requests)
	var first map[string]any
	for index, request := range requests {
		var body map[string]any
		require.NoError(t, json.Unmarshal(request.body, &body))
		require.Equal(t, true, body["enforce_nonce"])
		require.Len(t, body["nonce"], 25)
		if index == 0 {
			first = body
			continue
		}
		require.Equal(t, first, body, "every resend repeats the request with the same enforced nonce")
	}
}

func TestCreateMessageResendsUnknownOutcomesWithTheSameNonceThenSelectsUncertainAtTheCap(t *testing.T) {
	provider := newScriptedDiscord(t, scriptedResponse{http.StatusInternalServerError, `{"code":0}`})
	client := newTestClient(t, provider.URL, discord.Config{})
	ctx := singledispatchtest.NewContext("announcement-flow", "post")
	for attempt := 1; attempt <= 2; attempt++ {
		_, err := sdkgo.RunMutation(ctx, client.CreateMessage(), discordConnection, fencedCreateMessageInput)
		_, failure := requireRetry(t, err)
		require.Equal(t, sdkgo.FailureAvailability, failure.Kind)
		require.Equal(t, int32(attempt), lastCreateMessageMarker(t, ctx).SendCount)
		ctx.AdvanceAttempt()
	}
	result, err := sdkgo.RunMutation(ctx, client.CreateMessage(), discordConnection, fencedCreateMessageInput)
	require.NoError(t, err)
	require.Equal(t, discord.CreateMessageBranchUncertain, result.Branch)
	require.Equal(t, int32(3), lastCreateMessageMarker(t, ctx).SendCount)
	requireSameEnforcedNonce(t, provider.recordedRequests())
	require.Len(t, provider.recordedRequests(), 3)

	ctx.AdvanceAttempt()
	again, err := sdkgo.RunMutation(ctx, client.CreateMessage(), discordConnection, fencedCreateMessageInput)
	require.NoError(t, err)
	require.Equal(t, discord.CreateMessageBranchUncertain, again.Branch)
	require.Len(t, provider.recordedRequests(), 3, "no send remains after the cap")
}

func TestCreateMessageResendBelowTheCapReturnsTheMessageForTheSameNonce(t *testing.T) {
	for name, firstAnswer := range map[string]scriptedResponse{
		"server error":                    {http.StatusBadGateway, `{"code":0}`},
		"timeout status":                  {http.StatusRequestTimeout, `{}`},
		"unlisted 4xx":                    {http.StatusTooEarly, `{}`},
		"malformed 2xx":                   {http.StatusOK, `{"id":"x"}`},
		"error body that names a message": {http.StatusForbidden, messageJSON(testMessageID, "Release @everyone")},
	} {
		t.Run(name, func(t *testing.T) {
			provider := newScriptedDiscord(t, firstAnswer, scriptedResponse{http.StatusOK, messageJSON(testMessageID, "Release @everyone")})
			client := newTestClient(t, provider.URL, discord.Config{})
			ctx := singledispatchtest.NewContext("announcement-flow", "post")
			_, err := sdkgo.RunMutation(ctx, client.CreateMessage(), discordConnection, fencedCreateMessageInput)
			requireRetry(t, err)
			ctx.AdvanceAttempt()
			result, err := sdkgo.RunMutation(ctx, client.CreateMessage(), discordConnection, fencedCreateMessageInput)
			require.NoError(t, err)
			require.Equal(t, discord.CreateMessageBranchCreated, result.Branch)
			require.Equal(t, testMessageID, result.Value.ID)
			requireSameEnforcedNonce(t, provider.recordedRequests())
			require.Len(t, provider.recordedRequests(), 2)
		})
	}
}

func TestCreateMessageNeverResendsADefiniteRefusalOfTheFirstSend(t *testing.T) {
	for name, testCase := range map[string]struct {
		answer scriptedResponse
		branch sdkgo.BranchID
	}{
		"unknown channel":   {scriptedResponse{http.StatusNotFound, `{"code":10003}`}, discord.CreateMessageBranchNotFound},
		"missing access":    {scriptedResponse{http.StatusForbidden, `{"code":50013}`}, discord.CreateMessageBranchProviderRejected},
		"invalid content":   {scriptedResponse{http.StatusBadRequest, `{"code":50035}`}, discord.CreateMessageBranchProviderRejected},
		"rejected token":    {scriptedResponse{http.StatusUnauthorized, `{"code":0}`}, discord.CreateMessageBranchProviderRejected},
		"unprocessable 422": {scriptedResponse{http.StatusUnprocessableEntity, `{"code":50035}`}, discord.CreateMessageBranchProviderRejected},
	} {
		t.Run(name, func(t *testing.T) {
			provider := newScriptedDiscord(t, testCase.answer)
			client := newTestClient(t, provider.URL, discord.Config{})
			ctx := singledispatchtest.NewContext("announcement-flow", "post")
			result, err := sdkgo.RunMutation(ctx, client.CreateMessage(), discordConnection, fencedCreateMessageInput)
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
			require.Equal(t, singledispatch.StateCanResend, lastCreateMessageMarker(t, ctx).State)
			require.Len(t, provider.recordedRequests(), 1)
		})
	}
}

func TestCreateMessageRefusalWithoutAReadableBodyIsAnUnknownOutcome(t *testing.T) {
	provider := newScriptedDiscord(t, scriptedResponse{http.StatusForbidden, `{"message":"` + strings.Repeat("x", 70_000) + `"}`})
	client := newTestClient(t, provider.URL, discord.Config{})
	ctx := singledispatchtest.NewContext("announcement-flow", "post")
	_, err := sdkgo.RunMutation(ctx, client.CreateMessage(), discordConnection, fencedCreateMessageInput)
	requireRetry(t, err)
	require.Equal(t, singledispatch.StateClaimed, lastCreateMessageMarker(t, ctx).State)
}

func TestCreateMessageRefusalAfterAPossibleSendSelectsUncertain(t *testing.T) {
	provider := newScriptedDiscord(t,
		scriptedResponse{http.StatusInternalServerError, `{"code":0}`},
		scriptedResponse{http.StatusForbidden, `{"code":50013}`},
	)
	client := newTestClient(t, provider.URL, discord.Config{})
	ctx := singledispatchtest.NewContext("announcement-flow", "post")
	_, err := sdkgo.RunMutation(ctx, client.CreateMessage(), discordConnection, fencedCreateMessageInput)
	requireRetry(t, err)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.CreateMessage(), discordConnection, fencedCreateMessageInput)
	require.NoError(t, err)
	require.Equal(t, discord.CreateMessageBranchUncertain, result.Branch, "the first send may have created the message")
	require.Equal(t, sdkgo.FailureAuthorization, result.Failure.Kind)
}

func TestCreateMessageRateLimitsAndOutagesRetryWithoutCountingASend(t *testing.T) {
	provider := newScriptedDiscord(t,
		scriptedResponse{http.StatusTooManyRequests, `{"retry_after":1.5,"global":false}`},
		scriptedResponse{http.StatusServiceUnavailable, `{"code":0}`},
		scriptedResponse{http.StatusTooManyRequests, `{"retry_after":0.5,"global":false}`},
		scriptedResponse{http.StatusOK, messageJSON(testMessageID, "Release @everyone")},
	)
	client := newTestClient(t, provider.URL, discord.Config{})
	ctx := singledispatchtest.NewContext("announcement-flow", "post")
	for attempt := 1; attempt <= 3; attempt++ {
		_, err := sdkgo.RunMutation(ctx, client.CreateMessage(), discordConnection, fencedCreateMessageInput)
		requireRetry(t, err)
		require.Equal(t, singledispatch.StateCanResend, lastCreateMessageMarker(t, ctx).State)
		ctx.AdvanceAttempt()
	}
	result, err := sdkgo.RunMutation(ctx, client.CreateMessage(), discordConnection, fencedCreateMessageInput)
	require.NoError(t, err)
	require.Equal(t, discord.CreateMessageBranchCreated, result.Branch, "rate limits never exhaust the send cap")
	requireSameEnforcedNonce(t, provider.recordedRequests())
}

func TestCreateMessageUnreadableCheckpointRetriesWithoutSending(t *testing.T) {
	provider := newScriptedDiscord(t, scriptedResponse{http.StatusOK, messageJSON(testMessageID, "Release @everyone")})
	client := newTestClient(t, provider.URL, discord.Config{})
	ctx := singledispatchtest.NewContext("announcement-flow", "post")
	ctx.SetReadError(errors.New("heartbeat unavailable"))
	_, err := sdkgo.RunMutation(ctx, client.CreateMessage(), discordConnection, fencedCreateMessageInput)
	requireRetry(t, err)
	require.Empty(t, provider.recordedRequests())
}

func TestCreateMessageOversizedConfirmationResendsBelowTheCapAndIsUncertainAtIt(t *testing.T) {
	provider := newScriptedDiscord(t, scriptedResponse{http.StatusOK, messageJSON(testMessageID, strings.Repeat("x", 2000))})
	client := newTestClient(t, provider.URL, discord.Config{MaxResponseBytes: 512})
	ctx := singledispatchtest.NewContext("announcement-flow", "post")
	_, err := sdkgo.RunMutation(ctx, client.CreateMessage(), discordConnection, fencedCreateMessageInput, sdkgo.WithMaxSends(2))
	requireRetry(t, err)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.CreateMessage(), discordConnection, fencedCreateMessageInput, sdkgo.WithMaxSends(2))
	require.NoError(t, err)
	require.Equal(t, discord.CreateMessageBranchUncertain, result.Branch)
	require.Len(t, provider.recordedRequests(), 2, "the application's send cap of two replaces the manifest's three")
}

func TestCreateMessageTerminalCredentialFailureAfterAPossibleSendSelectsUncertain(t *testing.T) {
	provider := newScriptedDiscord(t, scriptedResponse{http.StatusInternalServerError, `{"code":0}`})
	client := newTestClient(t, provider.URL, discord.Config{})
	ctx := singledispatchtest.NewContext("announcement-flow", "post")
	_, err := sdkgo.RunMutation(ctx, client.CreateMessage(), discordConnection, fencedCreateMessageInput)
	requireRetry(t, err)
	ctx.AdvanceAttempt()
	revoked, err := discord.New(discord.Config{Endpoint: provider.URL}, failingDiscordCredentials{err: sdkgo.NewReauthorizationRequiredError(errors.New("revoked"))})
	require.NoError(t, err)
	result, err := sdkgo.RunMutation(ctx, revoked.CreateMessage(), discordConnection, fencedCreateMessageInput)
	require.NoError(t, err)
	require.Equal(t, discord.CreateMessageBranchUncertain, result.Branch)
	require.Len(t, provider.recordedRequests(), 1)
}
