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
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/discord/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

var fencedCreateMessageInput = discord.CreateMessageInput{ChannelID: testChannelID, Content: "Release @everyone"}

// requiredMessageFields are the message fields every conversion must find.
var requiredMessageFields = []string{"channel_id", "author", "content", "type", "timestamp"}

// messageJSONWithout returns the created message JSON without one top-level field.
func messageJSONWithout(t *testing.T, field string) string {
	t.Helper()
	var fields map[string]any
	require.NoError(t, json.Unmarshal([]byte(messageJSON(testMessageID, "Release @everyone")), &fields))
	delete(fields, field)
	encoded, err := json.Marshal(fields)
	require.NoError(t, err)
	return string(encoded)
}

func TestCreateMessageAcceptedAnswerMissingARequiredFieldIsReadBackAfterOnePost(t *testing.T) {
	for _, field := range requiredMessageFields {
		t.Run(field, func(t *testing.T) {
			provider := newRecordingDiscord(t, func(response http.ResponseWriter, request recordedRequest) {
				if request.method == http.MethodPost {
					writeJSON(t, response, http.StatusOK, messageJSONWithout(t, field))
					return
				}
				writeJSON(t, response, http.StatusOK, messageJSON(testMessageID, "Release @everyone"))
			})
			client := newTestClient(t, provider.URL, discord.Config{})
			result, err := sdkgo.RunMutation(singledispatchtest.NewContext("announcement-flow", "post"), client.CreateMessage(), discordConnection, fencedCreateMessageInput)
			require.NoError(t, err)
			require.Equal(t, discord.CreateMessageBranchCreated, result.Branch)
			requireOnePostThenReadBacks(t, provider.recordedRequests(), 1)
		})
	}
}

func TestCreateMessageReadBackMissingARequiredFieldSelectsUncertain(t *testing.T) {
	for _, field := range append([]string{"id"}, requiredMessageFields...) {
		t.Run(field, func(t *testing.T) {
			provider := newReadBackDiscord(t, nil, scriptedResponse{http.StatusOK, messageJSONWithout(t, field)})
			client := newTestClient(t, provider.URL, discord.Config{})
			result, err := sdkgo.RunMutation(singledispatchtest.NewContext("announcement-flow", "post"), client.CreateMessage(), discordConnection, fencedCreateMessageInput)
			require.NoError(t, err)
			require.Equal(t, discord.CreateMessageBranchUncertain, result.Branch)
			requireOnePostThenReadBacks(t, provider.recordedRequests(), 1)
		})
	}
}

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
	last := heartbeats[len(heartbeats)-1]
	// sdkgo v0.38.0 writes one flat Marker; later releases key Markers by Call ID.
	var checkpoint struct {
		Markers map[string]singledispatch.Marker `json:"singleDispatchMarkers"`
	}
	require.NoError(t, json.Unmarshal(last, &checkpoint))
	if len(checkpoint.Markers) > 0 {
		require.Len(t, checkpoint.Markers, 1)
		for _, marker := range checkpoint.Markers {
			return marker
		}
	}
	var marker singledispatch.Marker
	require.NoError(t, json.Unmarshal(last, &marker))
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

func TestCreateMessageOversizedConfirmationSelectsUncertainWithoutResending(t *testing.T) {
	provider := newScriptedDiscord(t, scriptedResponse{http.StatusOK, messageJSON(testMessageID, strings.Repeat("x", 2000))})
	client := newTestClient(t, provider.URL, discord.Config{MaxResponseBytes: 512})
	ctx := singledispatchtest.NewContext("announcement-flow", "post")
	result, err := sdkgo.RunMutation(ctx, client.CreateMessage(), discordConnection, fencedCreateMessageInput)
	require.NoError(t, err)
	require.Equal(t, discord.CreateMessageBranchUncertain, result.Branch, "a 2xx proves Discord created the message")
	require.Len(t, provider.recordedRequests(), 1)
}

func TestCreateMessageAcceptedAnswerWithoutAMessageIDSelectsUncertainWithoutResending(t *testing.T) {
	for name, body := range map[string]string{
		"unparseable":         `{`,
		"invalid message ID":  `{"id":"x"}`,
		"missing message ID":  `{"channel_id":"` + testChannelID + `"}`,
		"message ID not text": `{"id":998877665544332211}`,
		"message ID key case": `{"ID":"` + testMessageID + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			provider := newScriptedDiscord(t, scriptedResponse{http.StatusOK, body})
			client := newTestClient(t, provider.URL, discord.Config{})
			ctx := singledispatchtest.NewContext("announcement-flow", "post")
			result, err := sdkgo.RunMutation(ctx, client.CreateMessage(), discordConnection, fencedCreateMessageInput)
			require.NoError(t, err)
			require.Equal(t, discord.CreateMessageBranchUncertain, result.Branch)
			require.Equal(t, sdkgo.FailureProtocol, result.Failure.Kind)
			require.Len(t, provider.recordedRequests(), 1)
		})
	}
}

// incompleteAcceptedMessageJSON is a 2xx answer with a valid message ID but no author, content, or type.
const incompleteAcceptedMessageJSON = `{"id":"` + testMessageID + `","channel_id":"` + testChannelID + `"}`

// newReadBackDiscord answers the POST with an incomplete accepted message and each GET with the next scripted response.
func newReadBackDiscord(t *testing.T, onPost func(), readBacks ...scriptedResponse) *recordingDiscord {
	t.Helper()
	var readBackCount atomic.Int32
	return newRecordingDiscord(t, func(response http.ResponseWriter, request recordedRequest) {
		if request.method == http.MethodPost {
			if onPost != nil {
				onPost()
			}
			writeJSON(t, response, http.StatusOK, incompleteAcceptedMessageJSON)
			return
		}
		index := min(int(readBackCount.Add(1))-1, len(readBacks)-1)
		writeJSON(t, response, readBacks[index].status, readBacks[index].body)
	})
}

func requireOnePostThenReadBacks(t *testing.T, requests []recordedRequest, readBackCount int) {
	t.Helper()
	require.Len(t, requests, 1+readBackCount)
	require.Equal(t, http.MethodPost, requests[0].method)
	for _, request := range requests[1:] {
		require.Equal(t, http.MethodGet, request.method, "an accepted message is never resent")
		require.Equal(t, "/channels/"+testChannelID+"/messages/"+testMessageID, request.path)
	}
}

func TestCreateMessageAcceptedAnswerWithAMessageIDReadsTheMessageBack(t *testing.T) {
	provider := newReadBackDiscord(t, nil, scriptedResponse{http.StatusOK, messageJSON(testMessageID, "Release @everyone")})
	client := newTestClient(t, provider.URL, discord.Config{})
	ctx := singledispatchtest.NewContext("announcement-flow", "post")
	result, err := sdkgo.RunMutation(ctx, client.CreateMessage(), discordConnection, fencedCreateMessageInput)
	require.NoError(t, err)
	require.Equal(t, discord.CreateMessageBranchCreated, result.Branch)
	require.Equal(t, testMessageID, result.Value.ID)
	require.Equal(t, testMessageID, lastCreateMessageMarker(t, ctx).ProviderID)
	requireOnePostThenReadBacks(t, provider.recordedRequests(), 1)
}

func TestCreateMessageUnknownReadBackRetriesWithTheSavedMessageIDAndNeverResends(t *testing.T) {
	for name, maxSends := range map[string]int32{"manifest cap": 3, "application cap of one": 1} {
		t.Run(name, func(t *testing.T) {
			provider := newReadBackDiscord(t, nil,
				scriptedResponse{http.StatusServiceUnavailable, `{"code":0}`},
				scriptedResponse{http.StatusInternalServerError, `{"code":0}`},
				scriptedResponse{http.StatusOK, messageJSON(testMessageID, "Release @everyone")},
			)
			client := newTestClient(t, provider.URL, discord.Config{})
			ctx := singledispatchtest.NewContext("announcement-flow", "post")
			for attempt := 1; attempt <= 2; attempt++ {
				_, err := sdkgo.RunMutation(ctx, client.CreateMessage(), discordConnection, fencedCreateMessageInput, sdkgo.WithMaxSends(maxSends))
				requireRetry(t, err)
				require.Equal(t, testMessageID, lastCreateMessageMarker(t, ctx).ProviderID)
				ctx.AdvanceAttempt()
			}
			result, err := sdkgo.RunMutation(ctx, client.CreateMessage(), discordConnection, fencedCreateMessageInput, sdkgo.WithMaxSends(maxSends))
			require.NoError(t, err)
			require.Equal(t, discord.CreateMessageBranchCreated, result.Branch)
			require.Equal(t, testMessageID, result.Value.ID)
			requireOnePostThenReadBacks(t, provider.recordedRequests(), 3)
		})
	}
}

func TestCreateMessageTransientReadBackAnswersRetryWithTheSavedMessageID(t *testing.T) {
	for name, readBack := range map[string]scriptedResponse{
		"timeout status": {http.StatusRequestTimeout, `{}`},
		"rate limit":     {http.StatusTooManyRequests, `{"retry_after":0.5}`},
		"oversized 2xx":  {http.StatusOK, messageJSON(testMessageID, strings.Repeat("x", 2000))},
	} {
		t.Run(name, func(t *testing.T) {
			provider := newReadBackDiscord(t, nil, readBack)
			client := newTestClient(t, provider.URL, discord.Config{MaxResponseBytes: 512})
			ctx := singledispatchtest.NewContext("announcement-flow", "post")
			_, err := sdkgo.RunMutation(ctx, client.CreateMessage(), discordConnection, fencedCreateMessageInput)
			requireRetry(t, err)
			require.Equal(t, testMessageID, lastCreateMessageMarker(t, ctx).ProviderID)
			requireOnePostThenReadBacks(t, provider.recordedRequests(), 1)
		})
	}
}

func TestCreateMessageZonelessTimestampIsReadAsUTC(t *testing.T) {
	body := strings.Replace(messageJSON(testMessageID, "Release @everyone"), "2026-10-13T20:00:00.123000+00:00", "2026-10-13T20:00:00.123000", 1)
	provider := newScriptedDiscord(t, scriptedResponse{http.StatusOK, body})
	client := newTestClient(t, provider.URL, discord.Config{})
	result, err := sdkgo.RunMutation(singledispatchtest.NewContext("announcement-flow", "post"), client.CreateMessage(), discordConnection, fencedCreateMessageInput)
	require.NoError(t, err)
	require.Equal(t, discord.CreateMessageBranchCreated, result.Branch)
	require.Equal(t, "2026-10-13T20:00:00.123Z", result.Value.Timestamp.Format(time.RFC3339Nano))
	require.Len(t, provider.recordedRequests(), 1)
}

func TestCreateMessageFinalReadBackAnswerSelectsUncertain(t *testing.T) {
	for name, readBack := range map[string]scriptedResponse{
		"unknown message":         {http.StatusNotFound, `{"code":10008}`},
		"missing message history": {http.StatusForbidden, `{"code":50001}`},
		"unlisted status":         {http.StatusConflict, `{"code":0}`},
		// A complete 2xx does not change on a retry, so an unusable one is final.
		"contradicting message": {http.StatusOK, messageJSON("998877665544332212", "Release @everyone")},
		"incomplete message":    {http.StatusOK, incompleteAcceptedMessageJSON},
	} {
		t.Run(name, func(t *testing.T) {
			provider := newReadBackDiscord(t, nil, readBack)
			client := newTestClient(t, provider.URL, discord.Config{})
			ctx := singledispatchtest.NewContext("announcement-flow", "post")
			result, err := sdkgo.RunMutation(ctx, client.CreateMessage(), discordConnection, fencedCreateMessageInput)
			require.NoError(t, err)
			require.Equal(t, discord.CreateMessageBranchUncertain, result.Branch)
			require.Equal(t, testMessageID, result.Receipt.ProviderObjectID)
			requireOnePostThenReadBacks(t, provider.recordedRequests(), 1)
		})
	}
}

func TestCreateMessageUnknownReadBackWithoutASavedMessageIDSelectsUncertain(t *testing.T) {
	ctx := singledispatchtest.NewContext("announcement-flow", "post")
	// The claim is already persisted, so only the message ID checkpoint fails.
	provider := newReadBackDiscord(t, func() { ctx.SetWriteError(errors.New("heartbeat unavailable")) },
		scriptedResponse{http.StatusServiceUnavailable, `{"code":0}`})
	client := newTestClient(t, provider.URL, discord.Config{})
	result, err := sdkgo.RunMutation(ctx, client.CreateMessage(), discordConnection, fencedCreateMessageInput)
	require.NoError(t, err)
	require.Equal(t, discord.CreateMessageBranchUncertain, result.Branch)
	requireOnePostThenReadBacks(t, provider.recordedRequests(), 1)
}

func TestCreateMessageSavedMessageIDWithATransientCredentialFailureRetriesWithoutSending(t *testing.T) {
	provider := newReadBackDiscord(t, nil, scriptedResponse{http.StatusServiceUnavailable, `{"code":0}`})
	client := newTestClient(t, provider.URL, discord.Config{})
	ctx := singledispatchtest.NewContext("announcement-flow", "post")
	_, err := sdkgo.RunMutation(ctx, client.CreateMessage(), discordConnection, fencedCreateMessageInput)
	requireRetry(t, err)
	ctx.AdvanceAttempt()
	unavailable, err := discord.New(discord.Config{Endpoint: provider.URL}, failingDiscordCredentials{err: errors.New("broker timeout")})
	require.NoError(t, err)
	_, err = sdkgo.RunMutation(ctx, unavailable.CreateMessage(), discordConnection, fencedCreateMessageInput)
	requireRetry(t, err)
	ctx.AdvanceAttempt()
	revoked, err := discord.New(discord.Config{Endpoint: provider.URL}, failingDiscordCredentials{err: sdkgo.NewReauthorizationRequiredError(errors.New("revoked"))})
	require.NoError(t, err)
	result, err := sdkgo.RunMutation(ctx, revoked.CreateMessage(), discordConnection, fencedCreateMessageInput)
	require.NoError(t, err)
	require.Equal(t, discord.CreateMessageBranchUncertain, result.Branch)
	requireOnePostThenReadBacks(t, provider.recordedRequests(), 1)
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
