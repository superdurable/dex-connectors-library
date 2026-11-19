// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package discord

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListGuildMembersInput selects one bounded page of server members.
type ListGuildMembersInput struct {
	// GuildID is the server snowflake from Copy Server ID.
	GuildID string `json:"guildId"`
	// Limit is the number of members, 1 to 1000; zero selects 100.
	Limit int `json:"limit,omitempty"`
	// AfterUserID is the last user ID from a previous full page; blank starts at the beginning.
	AfterUserID string `json:"afterUserId,omitempty"`
}

// GuildMember contains only the member fields used for routing and moderation.
type GuildMember struct {
	// UserID is the member's Discord snowflake.
	UserID string `json:"userId"`
	// Nickname is the server nickname, blank when unset.
	Nickname string `json:"nickname,omitempty"`
	// RoleIDs are the member's role snowflakes.
	RoleIDs []string `json:"roleIds"`
	// TimeoutUntil is the provider's ISO8601 communication timeout, blank when absent.
	TimeoutUntil string `json:"timeoutUntil,omitempty"`
}

// GuildMemberPage is one page of server members.
type GuildMemberPage struct {
	// Members contains member summaries in Discord's order.
	Members []GuildMember `json:"members"`
	// NextAfter is the next cursor when the page is full; blank means the page ended early.
	NextAfter string `json:"nextAfter,omitempty"`
}

// ListGuildMembersOperation lists a bounded page of members.
type ListGuildMembersOperation struct{ client *Client }

var listGuildMembersBranches = operationBranches{
	notFound: ListGuildMembersBranchNotFound, rejected: ListGuildMembersBranchProviderRejected,
	invalidResponse: ListGuildMembersBranchInvalidResponse, defect: ListGuildMembersBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (ListGuildMembersOperation) Definition() sdkgo.QueryDefinition {
	return ListGuildMembersDefinition
}

// Invoke reads one server member page; Discord requires the Server Members privileged intent.
func (operation ListGuildMembersOperation) Invoke(call sdkgo.Call, input ListGuildMembersInput) sdkgo.QueryAttempt[GuildMemberPage] {
	if validateSnowflake("guildId", input.GuildID) != nil || input.Limit < 0 || input.Limit > 1000 || (input.AfterUserID != "" && validateSnowflake("afterUserId", input.AfterUserID) != nil) {
		return sdkgo.NewQueryBranch(ListGuildMembersBranchDefect, GuildMemberPage{}, providerFailurePointer("listGuildMembers", sdkgo.FailureValidation, "valid guild ID, limit from 1 to 1000, and optional cursor are required"), sdkgo.Receipt{})
	}
	limit := input.Limit
	if limit == 0 {
		limit = 100
	}
	path := "/guilds/" + input.GuildID + "/members?limit=" + strconv.Itoa(limit)
	if input.AfterUserID != "" {
		path += "&after=" + input.AfterUserID
	}
	response, outcome := operation.client.exchange(call, "listGuildMembers", providerRequest{method: http.MethodGet, path: path}, input.GuildID)
	if outcome != nil {
		return queryAttemptForOutcome[GuildMemberPage](*outcome, listGuildMembersBranches)
	}
	var decoded []struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
		Nick    *string  `json:"nick"`
		Roles   []string `json:"roles"`
		Timeout *string  `json:"communication_disabled_until"`
	}
	if json.Unmarshal(response.body, &decoded) != nil || decoded == nil || len(decoded) > limit {
		return sdkgo.NewQueryBranch(ListGuildMembersBranchInvalidResponse, GuildMemberPage{}, providerFailurePointer("listGuildMembers", sdkgo.FailureProtocol, "Discord returned an invalid member page"), operation.client.receipt(call, input.GuildID))
	}
	members := make([]GuildMember, 0, len(decoded))
	for _, item := range decoded {
		if validateSnowflake("userId", item.User.ID) != nil {
			return invalidGuildMemberPage(call, operation.client, input.GuildID)
		}
		member := GuildMember{UserID: item.User.ID, Nickname: optionalText(item.Nick), RoleIDs: item.Roles, TimeoutUntil: optionalText(item.Timeout)}
		if member.RoleIDs == nil {
			member.RoleIDs = []string{}
		}
		for _, roleID := range member.RoleIDs {
			if validateSnowflake("roleId", roleID) != nil {
				return invalidGuildMemberPage(call, operation.client, input.GuildID)
			}
		}
		if member.TimeoutUntil != "" {
			if _, err := time.Parse(time.RFC3339, member.TimeoutUntil); err != nil {
				return invalidGuildMemberPage(call, operation.client, input.GuildID)
			}
		}
		members = append(members, member)
	}
	page := GuildMemberPage{Members: members}
	if len(members) == limit {
		page.NextAfter = members[len(members)-1].UserID
	}
	return sdkgo.NewQueryBranch(ListGuildMembersBranchListed, page, nil, operation.client.receipt(call, input.GuildID))
}

func invalidGuildMemberPage(call sdkgo.Call, client *Client, guildID string) sdkgo.QueryAttempt[GuildMemberPage] {
	return sdkgo.NewQueryBranch(ListGuildMembersBranchInvalidResponse, GuildMemberPage{}, providerFailurePointer("listGuildMembers", sdkgo.FailureProtocol, "Discord returned an invalid member in the page"), client.receipt(call, guildID))
}

// GuildMemberChange identifies a completed member action without storing profile data.
type GuildMemberChange struct {
	// GuildID is the target server snowflake.
	GuildID string `json:"guildId"`
	// UserID is the target member snowflake.
	UserID string `json:"userId"`
	// RoleID is the changed role when the action concerns one role.
	RoleID string `json:"roleId,omitempty"`
	// TimeoutUntil is the absolute timeout when the action sets one.
	TimeoutUntil string `json:"timeoutUntil,omitempty"`
}

// BanGuildMemberInput identifies one user to ban without deleting previous messages.
type BanGuildMemberInput struct {
	// GuildID is the target server snowflake.
	GuildID string `json:"guildId"`
	// UserID is the target user's snowflake.
	UserID string `json:"userId"`
}

// KickGuildMemberInput identifies one server member to remove.
type KickGuildMemberInput struct {
	// GuildID is the target server snowflake.
	GuildID string `json:"guildId"`
	// UserID is the member's snowflake.
	UserID string `json:"userId"`
}

// GuildMemberRoleInput identifies one member and role to add or remove.
type GuildMemberRoleInput struct {
	// GuildID is the target server snowflake.
	GuildID string `json:"guildId"`
	// UserID is the member's snowflake.
	UserID string `json:"userId"`
	// RoleID is the role snowflake to add or remove.
	RoleID string `json:"roleId"`
}

// TimeoutGuildMemberInput sets an absolute ISO8601 timeout no more than 28 days in the future.
type TimeoutGuildMemberInput struct {
	// GuildID is the target server snowflake.
	GuildID string `json:"guildId"`
	// UserID is the member's snowflake.
	UserID string `json:"userId"`
	// TimeoutUntil is an absolute RFC3339 timestamp at most 28 days in the future.
	TimeoutUntil string `json:"timeoutUntil"`
}

// UnbanGuildMemberInput identifies one user whose ban should be removed.
type UnbanGuildMemberInput struct {
	// GuildID is the target server snowflake.
	GuildID string `json:"guildId"`
	// UserID is the banned user's snowflake.
	UserID string `json:"userId"`
}

// BanGuildMemberOperation bans one user.
type BanGuildMemberOperation struct{ client *Client }

// KickGuildMemberOperation removes one server member.
type KickGuildMemberOperation struct{ client *Client }

// AddGuildMemberRoleOperation adds one role to a member.
type AddGuildMemberRoleOperation struct{ client *Client }

// RemoveGuildMemberRoleOperation removes one role from a member.
type RemoveGuildMemberRoleOperation struct{ client *Client }

// TimeoutGuildMemberOperation sets an absolute member timeout.
type TimeoutGuildMemberOperation struct{ client *Client }

// UnbanGuildMemberOperation removes one user's ban.
type UnbanGuildMemberOperation struct{ client *Client }

// Definition returns the immutable BanGuildMember connector mutation.
func (BanGuildMemberOperation) Definition() sdkgo.MutationDefinition { return BanGuildMemberDefinition }

// Definition returns the immutable KickGuildMember connector mutation.
func (KickGuildMemberOperation) Definition() sdkgo.MutationDefinition {
	return KickGuildMemberDefinition
}

// Definition returns the immutable AddGuildMemberRole connector mutation.
func (AddGuildMemberRoleOperation) Definition() sdkgo.MutationDefinition {
	return AddGuildMemberRoleDefinition
}

// Definition returns the immutable RemoveGuildMemberRole connector mutation.
func (RemoveGuildMemberRoleOperation) Definition() sdkgo.MutationDefinition {
	return RemoveGuildMemberRoleDefinition
}

// Definition returns the immutable TimeoutGuildMember connector mutation.
func (TimeoutGuildMemberOperation) Definition() sdkgo.MutationDefinition {
	return TimeoutGuildMemberDefinition
}

// Definition returns the immutable UnbanGuildMember connector mutation.
func (UnbanGuildMemberOperation) Definition() sdkgo.MutationDefinition {
	return UnbanGuildMemberDefinition
}

// IdempotencyKey identifies the BanGuildMember call without sending a provider key.
func (BanGuildMemberOperation) IdempotencyKey(id sdkgo.CallID, _ BanGuildMemberInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// IdempotencyKey identifies the KickGuildMember call without sending a provider key.
func (KickGuildMemberOperation) IdempotencyKey(id sdkgo.CallID, _ KickGuildMemberInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// IdempotencyKey identifies the AddGuildMemberRole call without sending a provider key.
func (AddGuildMemberRoleOperation) IdempotencyKey(id sdkgo.CallID, _ GuildMemberRoleInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// IdempotencyKey identifies the RemoveGuildMemberRole call without sending a provider key.
func (RemoveGuildMemberRoleOperation) IdempotencyKey(id sdkgo.CallID, _ GuildMemberRoleInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// IdempotencyKey identifies the TimeoutGuildMember call without sending a provider key.
func (TimeoutGuildMemberOperation) IdempotencyKey(id sdkgo.CallID, _ TimeoutGuildMemberInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// IdempotencyKey identifies the UnbanGuildMember call without sending a provider key.
func (UnbanGuildMemberOperation) IdempotencyKey(id sdkgo.CallID, _ UnbanGuildMemberInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

type guildMemberAction struct {
	operation string
	success   sdkgo.BranchID
	branches  operationBranches
	method    string
	path      string
	body      []byte
	change    GuildMemberChange
}

func invokeGuildMemberAction(client *Client, call sdkgo.Call, action guildMemberAction) sdkgo.MutationAttempt[GuildMemberChange] {
	if validateSnowflake("guildId", action.change.GuildID) != nil || validateSnowflake("userId", action.change.UserID) != nil || (action.change.RoleID != "" && validateSnowflake("roleId", action.change.RoleID) != nil) {
		return sdkgo.NewMutationBranch(action.branches.defect, GuildMemberChange{}, providerFailurePointer(action.operation, sdkgo.FailureValidation, "valid server, member, and optional role IDs are required"), sdkgo.Receipt{})
	}
	response, outcome := client.exchange(call, action.operation, providerRequest{method: action.method, path: action.path, body: action.body}, action.change.UserID)
	if outcome != nil {
		return mutationAttemptForOutcome[GuildMemberChange](*outcome, action.branches)
	}
	if action.operation == "timeoutGuildMember" {
		var member struct {
			User struct {
				ID string `json:"id"`
			} `json:"user"`
			Timeout *string `json:"communication_disabled_until"`
		}
		decodeError := json.Unmarshal(response.body, &member)
		expectedUntil, _ := time.Parse(time.RFC3339, action.change.TimeoutUntil)
		actualUntil, parseError := time.Parse(time.RFC3339, optionalText(member.Timeout))
		if decodeError != nil || (member.User.ID != "" && member.User.ID != action.change.UserID) || parseError != nil || !actualUntil.Equal(expectedUntil) {
			return sdkgo.NewMutationRetry[GuildMemberChange](providerFailure(action.operation, sdkgo.FailureProtocol, "Discord returned a timeout inconsistent with the request"), 0)
		}
	}
	return sdkgo.NewMutationBranch(action.success, action.change, nil, client.receipt(call, action.change.UserID))
}

// Invoke bans one user; repeated PUTs converge on the same ban.
func (operation BanGuildMemberOperation) Invoke(call sdkgo.Call, input BanGuildMemberInput) sdkgo.MutationAttempt[GuildMemberChange] {
	return invokeGuildMemberAction(operation.client, call, guildMemberAction{operation: "banGuildMember", success: BanGuildMemberBranchBanned, branches: operationBranches{notFound: BanGuildMemberBranchNotFound, rejected: BanGuildMemberBranchProviderRejected, defect: BanGuildMemberBranchDefect}, method: http.MethodPut, path: memberBanPath(input.GuildID, input.UserID), change: GuildMemberChange{GuildID: input.GuildID, UserID: input.UserID}})
}

// Invoke removes one member; repeated DELETEs cannot remove another member.
func (operation KickGuildMemberOperation) Invoke(call sdkgo.Call, input KickGuildMemberInput) sdkgo.MutationAttempt[GuildMemberChange] {
	return invokeGuildMemberAction(operation.client, call, guildMemberAction{operation: "kickGuildMember", success: KickGuildMemberBranchKicked, branches: operationBranches{notFound: KickGuildMemberBranchNotFound, rejected: KickGuildMemberBranchProviderRejected, defect: KickGuildMemberBranchDefect}, method: http.MethodDelete, path: memberPath(input.GuildID, input.UserID), change: GuildMemberChange{GuildID: input.GuildID, UserID: input.UserID}})
}

// Invoke adds one role; repeated PUTs converge on the same membership.
func (operation AddGuildMemberRoleOperation) Invoke(call sdkgo.Call, input GuildMemberRoleInput) sdkgo.MutationAttempt[GuildMemberChange] {
	return invokeGuildMemberAction(operation.client, call, guildMemberAction{operation: "addGuildMemberRole", success: AddGuildMemberRoleBranchAdded, branches: operationBranches{notFound: AddGuildMemberRoleBranchNotFound, rejected: AddGuildMemberRoleBranchProviderRejected, defect: AddGuildMemberRoleBranchDefect}, method: http.MethodPut, path: memberRolePath(input), change: GuildMemberChange{GuildID: input.GuildID, UserID: input.UserID, RoleID: input.RoleID}})
}

// Invoke removes one role; repeated DELETEs converge on the same membership.
func (operation RemoveGuildMemberRoleOperation) Invoke(call sdkgo.Call, input GuildMemberRoleInput) sdkgo.MutationAttempt[GuildMemberChange] {
	return invokeGuildMemberAction(operation.client, call, guildMemberAction{operation: "removeGuildMemberRole", success: RemoveGuildMemberRoleBranchRemoved, branches: operationBranches{notFound: RemoveGuildMemberRoleBranchNotFound, rejected: RemoveGuildMemberRoleBranchProviderRejected, defect: RemoveGuildMemberRoleBranchDefect}, method: http.MethodDelete, path: memberRolePath(input), change: GuildMemberChange{GuildID: input.GuildID, UserID: input.UserID, RoleID: input.RoleID}})
}

// Invoke replaces one member's timeout with an explicit absolute timestamp.
func (operation TimeoutGuildMemberOperation) Invoke(call sdkgo.Call, input TimeoutGuildMemberInput) sdkgo.MutationAttempt[GuildMemberChange] {
	_, err := time.Parse(time.RFC3339, input.TimeoutUntil)
	if err != nil || input.TimeoutUntil == "" {
		return sdkgo.NewMutationBranch(TimeoutGuildMemberBranchDefect, GuildMemberChange{}, providerFailurePointer("timeoutGuildMember", sdkgo.FailureValidation, "a valid ISO8601 timeout is required"), sdkgo.Receipt{})
	}
	encoded, err := json.Marshal(map[string]string{"communication_disabled_until": input.TimeoutUntil})
	if err != nil {
		return sdkgo.NewMutationBranch(TimeoutGuildMemberBranchDefect, GuildMemberChange{}, providerFailurePointer("timeoutGuildMember", sdkgo.FailureLocalDefect, "timeout request could not be encoded"), sdkgo.Receipt{})
	}
	return invokeGuildMemberAction(operation.client, call, guildMemberAction{operation: "timeoutGuildMember", success: TimeoutGuildMemberBranchTimedOut, branches: operationBranches{notFound: TimeoutGuildMemberBranchNotFound, rejected: TimeoutGuildMemberBranchProviderRejected, defect: TimeoutGuildMemberBranchDefect}, method: http.MethodPatch, path: memberPath(input.GuildID, input.UserID), body: encoded, change: GuildMemberChange{GuildID: input.GuildID, UserID: input.UserID, TimeoutUntil: input.TimeoutUntil}})
}

// Invoke removes one user's ban; repeated DELETEs cannot unban another user.
func (operation UnbanGuildMemberOperation) Invoke(call sdkgo.Call, input UnbanGuildMemberInput) sdkgo.MutationAttempt[GuildMemberChange] {
	return invokeGuildMemberAction(operation.client, call, guildMemberAction{operation: "unbanGuildMember", success: UnbanGuildMemberBranchUnbanned, branches: operationBranches{notFound: UnbanGuildMemberBranchNotFound, rejected: UnbanGuildMemberBranchProviderRejected, defect: UnbanGuildMemberBranchDefect}, method: http.MethodDelete, path: memberBanPath(input.GuildID, input.UserID), change: GuildMemberChange{GuildID: input.GuildID, UserID: input.UserID}})
}

func memberPath(guildID string, userID string) string {
	return "/guilds/" + guildID + "/members/" + userID
}
func memberRolePath(input GuildMemberRoleInput) string {
	return memberPath(input.GuildID, input.UserID) + "/roles/" + input.RoleID
}
func memberBanPath(guildID string, userID string) string {
	return "/guilds/" + guildID + "/bans/" + userID
}
