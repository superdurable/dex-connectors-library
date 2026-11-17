// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package discord_test

import (
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/discord/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

const testChannelGuildID = "200000000000000001"
const testChannelJSON = `{"id":"100000000000000001","type":0,"guild_id":"200000000000000001","name":"announcements","topic":"Release news"}`

func TestGuildChannelOperationsUseTypedRoutes(t *testing.T) {
	provider := newRecordingDiscord(t, func(response http.ResponseWriter, request recordedRequest) {
		switch request.method + " " + request.path {
		case "GET /guilds/" + testChannelGuildID + "/channels":
			writeJSON(t, response, 200, "["+testChannelJSON+"]")
		case "POST /guilds/" + testChannelGuildID + "/channels":
			var body map[string]any
			require.NoError(t, json.Unmarshal(request.body, &body))
			require.Equal(t, "dex-updates", body["name"])
			require.Equal(t, float64(0), body["type"])
			writeJSON(t, response, 201, `{"id":"100000000000000003","type":0,"guild_id":"200000000000000001","name":"dex-updates","topic":"Dex updates"}`)
		case "PATCH /channels/" + "100000000000000001":
			writeJSON(t, response, 200, `{"id":"100000000000000001","type":0,"guild_id":"200000000000000001","name":"dex-updates","topic":"Release news"}`)
		case "DELETE /channels/" + "100000000000000001":
			writeJSON(t, response, 200, testChannelJSON)
		default:
			t.Fatalf("unexpected request %s %s", request.method, request.path)
		}
	})
	client := newTestClient(t, provider.URL, discord.Config{})
	listed, err := sdkgo.RunQuery(newStepDexContext("list-channels"), client.ListGuildChannels(), discordConnection, discord.ListGuildChannelsInput{GuildID: testChannelGuildID})
	require.NoError(t, err)
	require.Equal(t, discord.ListGuildChannelsBranchListed, listed.Branch)
	require.Len(t, listed.Value.Channels, 1)
	created, err := sdkgo.RunMutation(singledispatchtest.NewContext("discord", "create-channel"), client.CreateGuildChannel(), discordConnection, discord.CreateGuildChannelInput{GuildID: testChannelGuildID, Name: "dex-updates", Topic: "Dex updates"})
	require.NoError(t, err)
	require.Equal(t, discord.CreateGuildChannelBranchCreated, created.Branch)
	require.Equal(t, "100000000000000003", created.Value.ID)
	name := "dex-updates"
	updated, err := sdkgo.RunMutation(newStepDexContext("update-channel"), client.UpdateChannel(), discordConnection, discord.UpdateChannelInput{ChannelID: "100000000000000001", Name: &name})
	require.NoError(t, err)
	require.Equal(t, discord.UpdateChannelBranchUpdated, updated.Branch)
	deleted, err := sdkgo.RunMutation(newStepDexContext("delete-channel"), client.DeleteChannel(), discordConnection, discord.DeleteChannelInput{ChannelID: "100000000000000001"})
	require.NoError(t, err)
	require.Equal(t, discord.DeleteChannelBranchDeleted, deleted.Branch)
}

func TestCreateGuildChannelStopsAfterTwoUnknownSends(t *testing.T) {
	var requests atomic.Int32
	provider := newRecordingDiscord(t, func(response http.ResponseWriter, _ recordedRequest) {
		requests.Add(1)
		writeJSON(t, response, 500, `{}`)
	})
	client := newTestClient(t, provider.URL, discord.Config{})
	ctx := singledispatchtest.NewContext("discord", "create-channel-unknown")
	for attempt := 1; attempt <= 2; attempt++ {
		result, err := sdkgo.RunMutation(ctx, client.CreateGuildChannel(), discordConnection, discord.CreateGuildChannelInput{GuildID: testChannelGuildID, Name: "dex-updates", Topic: "Dex updates"})
		if attempt == 1 {
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			ctx.AdvanceAttempt()
			continue
		}
		require.NoError(t, err)
		require.Equal(t, discord.CreateGuildChannelBranchUncertain, result.Branch)
	}
	require.Equal(t, int32(2), requests.Load())
}

func TestCreateGuildChannelReadsBackKnownIDWithoutResending(t *testing.T) {
	var posts atomic.Int32
	provider := newRecordingDiscord(t, func(response http.ResponseWriter, request recordedRequest) {
		switch request.method {
		case http.MethodPost:
			posts.Add(1)
			writeJSON(t, response, 201, `{"id":"100000000000000003"}`)
		case http.MethodGet:
			require.Equal(t, "/channels/100000000000000003", request.path)
			writeJSON(t, response, 200, `{"id":"100000000000000003","type":0,"guild_id":"200000000000000001","name":"dex-updates","topic":"Dex updates"}`)
		default:
			t.Fatalf("unexpected method %s", request.method)
		}
	})
	client := newTestClient(t, provider.URL, discord.Config{})
	result, err := sdkgo.RunMutation(singledispatchtest.NewContext("discord", "create-channel-known-id"), client.CreateGuildChannel(), discordConnection, discord.CreateGuildChannelInput{GuildID: testChannelGuildID, Name: "dex-updates", Topic: "Dex updates"})
	require.NoError(t, err)
	require.Equal(t, discord.CreateGuildChannelBranchCreated, result.Branch)
	require.Equal(t, int32(1), posts.Load())
}
