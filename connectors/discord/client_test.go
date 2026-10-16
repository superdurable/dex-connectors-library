// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package discord_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/discord"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func messageJSON(id string, content string) string {
	encoded, _ := json.Marshal(content)
	return `{"id":"` + id + `","channel_id":"` + testChannelID + `","type":0,"content":` + string(encoded) +
		`,"timestamp":"2026-10-13T20:00:00.123000+00:00","edited_timestamp":null,` +
		`"author":{"id":"` + testAuthorID + `","username":"dex-bot","bot":true}}`
}

func TestGetChannelReadsTheChannelWithTheBotToken(t *testing.T) {
	provider := newRecordingDiscord(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusOK, `{"id":"`+testChannelID+`","type":0,"guild_id":"`+testGuildID+`",
			"name":"announcements","topic":null,"last_message_id":"`+testMessageID+`","nsfw":false}`)
	})
	client := newTestClient(t, provider.URL, discord.Config{})
	result, err := sdkgo.RunQuery(newStepDexContext("get-channel"), client.GetChannel(), discordConnection, discord.GetChannelInput{ChannelID: testChannelID})
	require.NoError(t, err)
	require.Equal(t, discord.GetChannelBranchFound, result.Branch)
	require.Equal(t, discord.Channel{ID: testChannelID, Type: 0, GuildID: testGuildID, Name: "announcements", LastMessageID: testMessageID}, result.Value)
	request := provider.recordedRequests()[0]
	require.Equal(t, http.MethodGet, request.method)
	require.Equal(t, "/channels/"+testChannelID, request.path)
	require.Equal(t, "Bot "+testBotToken, request.authorization)
	require.True(t, strings.HasPrefix(request.userAgent, "DiscordBot ("), "Discord requires a DiscordBot User-Agent")
	require.Equal(t, testChannelID, result.Receipt.ProviderObjectID)
}

func TestGetChannelClassifiesDiscordErrorsWithoutProviderText(t *testing.T) {
	for name, testCase := range map[string]struct {
		status int
		body   string
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
	}{
		"unknown channel":   {http.StatusNotFound, `{"message":"Unknown Channel ` + providerMessageSentinel + `","code":10003}`, discord.GetChannelBranchNotFound, sdkgo.FailureNotFound},
		"missing access":    {http.StatusForbidden, `{"message":"` + providerMessageSentinel + `","code":50001}`, discord.GetChannelBranchProviderRejected, sdkgo.FailureAuthorization},
		"rejected token":    {http.StatusUnauthorized, `{"message":"` + providerMessageSentinel + `","code":0}`, discord.GetChannelBranchProviderRejected, sdkgo.FailureAuthentication},
		"invalid form body": {http.StatusBadRequest, `{"message":"` + providerMessageSentinel + `","code":50035}`, discord.GetChannelBranchProviderRejected, sdkgo.FailureValidation},
		"malformed body":    {http.StatusOK, `{`, discord.GetChannelBranchInvalidResponse, sdkgo.FailureProtocol},
		"other channel":     {http.StatusOK, `{"id":"100000000000000001","type":0}`, discord.GetChannelBranchInvalidResponse, sdkgo.FailureProtocol},
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingDiscord(t, func(response http.ResponseWriter, _ recordedRequest) {
				writeJSON(t, response, testCase.status, testCase.body)
			})
			client := newTestClient(t, provider.URL, discord.Config{})
			result, err := sdkgo.RunQuery(newStepDexContext("get-"+name), client.GetChannel(), discordConnection, discord.GetChannelInput{ChannelID: testChannelID})
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
			require.Equal(t, testCase.kind, result.Failure.Kind)
			requireSecretSafeFailure(t, result.Failure)
			require.Len(t, provider.recordedRequests(), 1, "a terminal outcome is not retried inside one attempt")
		})
	}
}

func TestRateLimitsAndOutagesRetryWithDiscordsDelay(t *testing.T) {
	provider := newRecordingDiscord(t, func(response http.ResponseWriter, request recordedRequest) {
		if strings.HasSuffix(request.path, "/messages") {
			response.Header().Set("Retry-After", "1")
			writeJSON(t, response, http.StatusTooManyRequests, `{"message":"You are being rate limited.","retry_after":2.5,"global":false}`)
			return
		}
		writeJSON(t, response, http.StatusBadGateway, `<html>`+providerMessageSentinel+`</html>`)
	})
	client := newTestClient(t, provider.URL, discord.Config{})
	_, err := sdkgo.RunQuery(newStepDexContext("list-limited"), client.ListChannelMessages(), discordConnection, discord.ListChannelMessagesInput{ChannelID: testChannelID})
	delay, failure := requireRetry(t, err)
	require.Equal(t, 2500*time.Millisecond, delay, "the body's fractional retry_after wins over the whole-second header")
	require.Equal(t, sdkgo.FailureRateLimit, failure.Kind)

	_, err = sdkgo.RunQuery(newStepDexContext("get-outage"), client.GetChannel(), discordConnection, discord.GetChannelInput{ChannelID: testChannelID})
	_, failure = requireRetry(t, err)
	require.Equal(t, sdkgo.FailureAvailability, failure.Kind)
	requireSecretSafeFailure(t, &failure)
}

func TestInvalidInputAndCredentialsSelectDefectWithoutARequest(t *testing.T) {
	provider := newRecordingDiscord(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusOK, `{}`)
	})
	client := newTestClient(t, provider.URL, discord.Config{})
	result, err := sdkgo.RunQuery(newStepDexContext("get-bad-id"), client.GetChannel(), discordConnection, discord.GetChannelInput{ChannelID: "../guilds/1"})
	require.NoError(t, err)
	require.Equal(t, discord.GetChannelBranchDefect, result.Branch)

	unsafeClient, err := discord.New(discord.Config{Endpoint: provider.URL}, sdkgo.StaticCredentialProvider[discord.Credentials]{
		discordConnection: {BotToken: sdkgo.NewSecretString("token with spaces")},
	})
	require.NoError(t, err)
	result, err = sdkgo.RunQuery(newStepDexContext("get-bad-token"), unsafeClient.GetChannel(), discordConnection, discord.GetChannelInput{ChannelID: testChannelID})
	require.NoError(t, err)
	require.Equal(t, discord.GetChannelBranchDefect, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.Empty(t, provider.recordedRequests())
}

func TestListChannelMessagesReadsOneBoundedPageNewestFirst(t *testing.T) {
	provider := newRecordingDiscord(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusOK, `[`+messageJSON(testMessageID, "newest <b>")+`,`+messageJSON("998877665544332200", "older")+`]`)
	})
	client := newTestClient(t, provider.URL, discord.Config{})
	result, err := sdkgo.RunQuery(newStepDexContext("list"), client.ListChannelMessages(), discordConnection, discord.ListChannelMessagesInput{
		ChannelID: testChannelID, Limit: 2, BeforeMessageID: "998877665544332299",
	})
	require.NoError(t, err)
	require.Equal(t, discord.ListChannelMessagesBranchListed, result.Branch)
	request := provider.recordedRequests()[0]
	require.Equal(t, "/channels/"+testChannelID+"/messages", request.path)
	require.Equal(t, "before=998877665544332299&limit=2", request.query)
	require.Len(t, result.Value.Messages, 2)
	require.True(t, result.Value.IsFull)
	require.Equal(t, "998877665544332200", result.Value.OldestMessageID)
	newest := result.Value.Messages[0]
	require.Equal(t, "newest <b>", newest.Content)
	require.Equal(t, discord.MessageAuthor{ID: testAuthorID, Username: "dex-bot", IsBot: true}, newest.Author)
	require.Equal(t, time.Date(2026, 10, 13, 20, 0, 0, 123000000, time.UTC), newest.Timestamp)
	require.Nil(t, newest.EditedTimestamp)
}

func TestListChannelMessagesValidatesBoundsAndTheResponse(t *testing.T) {
	provider := newRecordingDiscord(t, func(response http.ResponseWriter, request recordedRequest) {
		if strings.Contains(request.query, "limit=50") {
			writeJSON(t, response, http.StatusOK, `[]`)
			return
		}
		writeJSON(t, response, http.StatusOK, `[`+messageJSON(testMessageID, "a")+`,`+messageJSON("998877665544332200", "b")+`]`)
	})
	client := newTestClient(t, provider.URL, discord.Config{})
	for name, input := range map[string]discord.ListChannelMessagesInput{
		"limit too large":   {ChannelID: testChannelID, Limit: 101},
		"negative limit":    {ChannelID: testChannelID, Limit: -1},
		"before and after":  {ChannelID: testChannelID, BeforeMessageID: testMessageID, AfterMessageID: testMessageID},
		"malformed cursor":  {ChannelID: testChannelID, AfterMessageID: "1&limit=100"},
		"malformed channel": {ChannelID: "general"},
	} {
		result, err := sdkgo.RunQuery(newStepDexContext("list-"+name), client.ListChannelMessages(), discordConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, discord.ListChannelMessagesBranchDefect, result.Branch, name)
	}
	require.Empty(t, provider.recordedRequests())

	result, err := sdkgo.RunQuery(newStepDexContext("list-empty"), client.ListChannelMessages(), discordConnection, discord.ListChannelMessagesInput{ChannelID: testChannelID})
	require.NoError(t, err)
	require.Equal(t, discord.ListChannelMessagesBranchListed, result.Branch)
	require.Equal(t, []discord.Message{}, result.Value.Messages)
	require.False(t, result.Value.IsFull)

	result, err = sdkgo.RunQuery(newStepDexContext("list-overfull"), client.ListChannelMessages(), discordConnection, discord.ListChannelMessagesInput{ChannelID: testChannelID, Limit: 1})
	require.NoError(t, err)
	require.Equal(t, discord.ListChannelMessagesBranchInvalidResponse, result.Branch, "more messages than the limit is a protocol fault")
}

func TestOversizedResponsesSelectInvalidResponse(t *testing.T) {
	provider := newRecordingDiscord(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusOK, `[`+messageJSON(testMessageID, strings.Repeat("x", 2000))+`]`)
	})
	client := newTestClient(t, provider.URL, discord.Config{MaxResponseBytes: 512})
	result, err := sdkgo.RunQuery(newStepDexContext("list-large"), client.ListChannelMessages(), discordConnection, discord.ListChannelMessagesInput{ChannelID: testChannelID})
	require.NoError(t, err)
	require.Equal(t, discord.ListChannelMessagesBranchInvalidResponse, result.Branch)
	require.Equal(t, sdkgo.FailureResponseTooLarge, result.Failure.Kind)
}

func TestCreateMessageSendsAStableEnforcedNonceAndDisablesMentions(t *testing.T) {
	var attempts atomic.Int32
	provider := newRecordingDiscord(t, func(response http.ResponseWriter, _ recordedRequest) {
		if attempts.Add(1) == 1 {
			writeJSON(t, response, http.StatusServiceUnavailable, `{"message":"`+providerMessageSentinel+`","code":0}`)
			return
		}
		writeJSON(t, response, http.StatusOK, messageJSON(testMessageID, "Release @everyone"))
	})
	client := newTestClient(t, provider.URL, discord.Config{})
	input := discord.CreateMessageInput{ChannelID: testChannelID, Content: "Release @everyone", ReplyToMessageID: "998877665544332200"}
	_, err := sdkgo.RunMutation(newStepDexContext("post"), client.CreateMessage(), discordConnection, input)
	_, failure := requireRetry(t, err)
	require.Equal(t, sdkgo.FailureAvailability, failure.Kind, "a possibly applied post retries because the nonce deduplicates it")
	result, err := sdkgo.RunMutation(newStepDexContext("post"), client.CreateMessage(), discordConnection, input)
	require.NoError(t, err)
	require.Equal(t, discord.CreateMessageBranchCreated, result.Branch)
	require.Equal(t, testMessageID, result.Value.ID)
	require.Equal(t, testMessageID, result.Receipt.ProviderObjectID)
	require.NotEmpty(t, result.Receipt.IdempotencyKey)

	requests := provider.recordedRequests()
	require.Len(t, requests, 2)
	var bodies [2]map[string]any
	for index, request := range requests {
		require.Equal(t, http.MethodPost, request.method)
		require.Equal(t, "/channels/"+testChannelID+"/messages", request.path)
		require.Equal(t, "application/json", request.contentType)
		require.NoError(t, json.Unmarshal(request.body, &bodies[index]))
	}
	require.Equal(t, bodies[0], bodies[1], "a retry of one Step execution sends the same nonce")
	digest := sha256.Sum256([]byte(result.Receipt.IdempotencyKey))
	require.Equal(t, base64.RawURLEncoding.EncodeToString(digest[:])[:25], bodies[0]["nonce"])
	require.Equal(t, true, bodies[0]["enforce_nonce"])
	require.Equal(t, map[string]any{"parse": []any{}}, bodies[0]["allowed_mentions"])
	require.Equal(t, map[string]any{"message_id": "998877665544332200", "fail_if_not_exists": true}, bodies[0]["message_reference"])

	other, err := sdkgo.RunMutation(newStepDexContext("post-again"), client.CreateMessage(), discordConnection, input)
	require.NoError(t, err)
	require.Equal(t, discord.CreateMessageBranchCreated, other.Branch)
	var otherBody map[string]any
	require.NoError(t, json.Unmarshal(provider.recordedRequests()[2].body, &otherBody))
	require.NotEqual(t, bodies[0]["nonce"], otherBody["nonce"], "a new Step execution is a new message")
}

func TestCreateMessageValidatesContentAndClassifiesRejections(t *testing.T) {
	provider := newRecordingDiscord(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusForbidden, `{"message":"Missing Permissions `+providerMessageSentinel+`","code":50013}`)
	})
	client := newTestClient(t, provider.URL, discord.Config{})
	for name, input := range map[string]discord.CreateMessageInput{
		"blank":     {ChannelID: testChannelID, Content: " \n"},
		"too long":  {ChannelID: testChannelID, Content: strings.Repeat("é", discord.MaximumMessageCharacters+1)},
		"bad reply": {ChannelID: testChannelID, Content: "hi", ReplyToMessageID: "latest"},
	} {
		result, err := sdkgo.RunMutation(newStepDexContext("post-"+name), client.CreateMessage(), discordConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, discord.CreateMessageBranchDefect, result.Branch, name)
	}
	require.Empty(t, provider.recordedRequests())

	result, err := sdkgo.RunMutation(newStepDexContext("post-denied"), client.CreateMessage(), discordConnection, discord.CreateMessageInput{
		ChannelID: testChannelID, Content: strings.Repeat("é", discord.MaximumMessageCharacters),
	})
	require.NoError(t, err)
	require.Equal(t, discord.CreateMessageBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthorization, result.Failure.Kind)
	require.Contains(t, result.Failure.Message, "code 50013")
	requireSecretSafeFailure(t, result.Failure)
}

func TestAddReactionPutsTheEscapedEmojiForTheBot(t *testing.T) {
	provider := newRecordingDiscord(t, func(response http.ResponseWriter, _ recordedRequest) {
		response.WriteHeader(http.StatusNoContent)
	})
	client := newTestClient(t, provider.URL, discord.Config{})
	for _, emoji := range []string{"👍", "party_blob:123456789012345678"} {
		input := discord.AddReactionInput{ChannelID: testChannelID, MessageID: testMessageID, Emoji: emoji}
		result, err := sdkgo.RunMutation(newStepDexContext("react-"+emoji), client.AddReaction(), discordConnection, input)
		require.NoError(t, err)
		require.Equal(t, discord.AddReactionBranchAdded, result.Branch)
		require.Equal(t, discord.AddedReaction(input), result.Value)
	}
	requests := provider.recordedRequests()
	require.Equal(t, http.MethodPut, requests[0].method)
	require.Equal(t, "/channels/"+testChannelID+"/messages/"+testMessageID+"/reactions/%F0%9F%91%8D/@me", requests[0].rawPath)
	require.Equal(t, "/channels/"+testChannelID+"/messages/"+testMessageID+"/reactions/party_blob:123456789012345678/@me", requests[1].rawPath)
	require.Empty(t, requests[0].body)
}

func TestAddReactionRejectsUnsafeEmojiAndRoutesUnknownMessages(t *testing.T) {
	provider := newRecordingDiscord(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusNotFound, `{"message":"Unknown Message","code":10008}`)
	})
	client := newTestClient(t, provider.URL, discord.Config{})
	for _, emoji := range []string{"", "thumbsup", "../../@me", "👍 👍", "a:1"} {
		result, err := sdkgo.RunMutation(newStepDexContext("react-invalid"), client.AddReaction(), discordConnection, discord.AddReactionInput{
			ChannelID: testChannelID, MessageID: testMessageID, Emoji: emoji,
		})
		require.NoError(t, err, emoji)
		require.Equal(t, discord.AddReactionBranchDefect, result.Branch, emoji)
	}
	require.Empty(t, provider.recordedRequests())
	result, err := sdkgo.RunMutation(newStepDexContext("react-missing"), client.AddReaction(), discordConnection, discord.AddReactionInput{
		ChannelID: testChannelID, MessageID: testMessageID, Emoji: "👍",
	})
	require.NoError(t, err)
	require.Equal(t, discord.AddReactionBranchNotFound, result.Branch)
}

func TestNewValidatesConfiguration(t *testing.T) {
	credentials := sdkgo.StaticCredentialProvider[discord.Credentials]{discordConnection: {BotToken: sdkgo.NewSecretString(testBotToken)}}
	for _, endpoint := range []string{"http://discord.example/api", "https://user:secret@discord.com/api", "https://discord.com/api?x=1"} {
		_, err := discord.New(discord.Config{Endpoint: endpoint}, credentials)
		require.Error(t, err, endpoint)
		require.NotContains(t, err.Error(), "secret")
	}
	_, err := discord.New(discord.Config{}, nil)
	require.Error(t, err)
	_, err = discord.New(discord.Config{}, credentials, nil)
	require.Error(t, err)
	_, err = discord.New(discord.Config{MaxResponseBytes: -1}, credentials)
	require.Error(t, err)
	client, err := discord.New(discord.Config{}, credentials)
	require.NoError(t, err)
	require.NotNil(t, client)
	require.Equal(t, "https://discord.com/api/v10", discord.DefaultConfig().Endpoint)
}
