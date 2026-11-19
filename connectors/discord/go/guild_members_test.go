// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package discord_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/discord/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const memberGuildID = "200000000000000001"
const memberUserID = "400000000000000001"
const memberRoleID = "500000000000000001"
const memberTimeout = "2026-11-18T12:00:00Z"

func TestGuildMemberOperationsUseExactRoutes(t *testing.T) {
	provider := newRecordingDiscord(t, func(response http.ResponseWriter, request recordedRequest) {
		switch request.method + " " + request.path {
		case "GET /guilds/" + memberGuildID + "/members":
			require.Equal(t, "limit=2", request.query)
			writeJSON(t, response, 200, `[{"user":{"id":"400000000000000001"},"nick":"releasebot","roles":[]}]`)
		case "PUT /guilds/" + memberGuildID + "/bans/" + memberUserID,
			"DELETE /guilds/" + memberGuildID + "/members/" + memberUserID,
			"PUT /guilds/" + memberGuildID + "/members/" + memberUserID + "/roles/" + memberRoleID,
			"DELETE /guilds/" + memberGuildID + "/members/" + memberUserID + "/roles/" + memberRoleID,
			"DELETE /guilds/" + memberGuildID + "/bans/" + memberUserID:
			response.WriteHeader(http.StatusNoContent)
		case "PATCH /guilds/" + memberGuildID + "/members/" + memberUserID:
			require.JSONEq(t, `{"communication_disabled_until":"2026-11-18T12:00:00Z"}`, string(request.body))
			writeJSON(t, response, 200, `{"user":{"id":"400000000000000001"},"communication_disabled_until":"2026-11-18T12:00:00Z"}`)
		default:
			t.Fatalf("unexpected member request %s %s", request.method, request.path)
		}
	})
	client := newTestClient(t, provider.URL, discord.Config{})
	listed, err := sdkgo.RunQuery(newStepDexContext("list-members"), client.ListGuildMembers(), discordConnection, discord.ListGuildMembersInput{GuildID: memberGuildID, Limit: 2})
	require.NoError(t, err)
	require.Equal(t, discord.ListGuildMembersBranchListed, listed.Branch)
	require.Equal(t, memberUserID, listed.Value.Members[0].UserID)
	ban, err := sdkgo.RunMutation(newStepDexContext("ban-member"), client.BanGuildMember(), discordConnection, discord.BanGuildMemberInput{GuildID: memberGuildID, UserID: memberUserID})
	require.NoError(t, err)
	require.Equal(t, discord.BanGuildMemberBranchBanned, ban.Branch)
	kick, err := sdkgo.RunMutation(newStepDexContext("kick-member"), client.KickGuildMember(), discordConnection, discord.KickGuildMemberInput{GuildID: memberGuildID, UserID: memberUserID})
	require.NoError(t, err)
	require.Equal(t, discord.KickGuildMemberBranchKicked, kick.Branch)
	role := discord.GuildMemberRoleInput{GuildID: memberGuildID, UserID: memberUserID, RoleID: memberRoleID}
	added, err := sdkgo.RunMutation(newStepDexContext("add-role"), client.AddGuildMemberRole(), discordConnection, role)
	require.NoError(t, err)
	require.Equal(t, discord.AddGuildMemberRoleBranchAdded, added.Branch)
	removed, err := sdkgo.RunMutation(newStepDexContext("remove-role"), client.RemoveGuildMemberRole(), discordConnection, role)
	require.NoError(t, err)
	require.Equal(t, discord.RemoveGuildMemberRoleBranchRemoved, removed.Branch)
	timedOut, err := sdkgo.RunMutation(newStepDexContext("timeout-member"), client.TimeoutGuildMember(), discordConnection, discord.TimeoutGuildMemberInput{GuildID: memberGuildID, UserID: memberUserID, TimeoutUntil: memberTimeout})
	require.NoError(t, err)
	require.Equal(t, discord.TimeoutGuildMemberBranchTimedOut, timedOut.Branch)
	unbanned, err := sdkgo.RunMutation(newStepDexContext("unban-member"), client.UnbanGuildMember(), discordConnection, discord.UnbanGuildMemberInput{GuildID: memberGuildID, UserID: memberUserID})
	require.NoError(t, err)
	require.Equal(t, discord.UnbanGuildMemberBranchUnbanned, unbanned.Branch)
}
