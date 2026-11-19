// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package channelannouncement

import (
	"github.com/superdurable/dex-connectors-library/connectors/discord/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// MemberModerationFlowType is the stable identity of the moderation example.
const MemberModerationFlowType = "DiscordMemberModeration"

var moderationRequestAttribute = dex.DefineAttribute[MemberModerationRequest]("discord-moderation-request")
var moderationResultAttribute = dex.DefineAttribute[discord.GuildMemberChange]("discord-moderation-result")

// MemberModerationRequest selects one test member, role, and absolute timeout.
type MemberModerationRequest struct {
	// GuildID is the disposable server snowflake.
	GuildID string `json:"guildId"`
	// UserID is the test user's snowflake.
	UserID string `json:"userId"`
	// RoleID is a disposable role snowflake below the bot's role.
	RoleID string `json:"roleId"`
	// TimeoutUntil is an RFC3339 timestamp at most 28 days in the future.
	TimeoutUntil string `json:"timeoutUntil"`
}

// MemberModerationFlow lists, changes, removes, bans, and unbans a test member.
type MemberModerationFlow struct {
	dex.FlowDefaults
	connection discord.Connection
}

// NewMemberModerationFlow binds the static Discord connection.
func NewMemberModerationFlow(connection discord.Connection) *MemberModerationFlow {
	return &MemberModerationFlow{connection: connection}
}
func (*MemberModerationFlow) GetFlowType() string { return MemberModerationFlowType }
func (*MemberModerationFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{moderationRequestAttribute, moderationResultAttribute}}
}
func (flow *MemberModerationFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:discord-moderation-result value-type:json editable:false description:"Moderation result"
// GetDexSummary shows the final test member action.
func (*MemberModerationFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := optionalAttribute(ctx, moderationResultAttribute)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"discord-moderation-result": result}}, nil
}

// dex:field attribute-key:discord-moderation-result value-type:json editable:false description:"Moderation result"
// GetDexDisplay shows the final test member action.
func (*MemberModerationFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := optionalAttribute(ctx, moderationResultAttribute)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"discord-moderation-result": result}}, nil
}

func (flow *MemberModerationFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startMemberModeration{}),
		dex.DefineStep(discord.NewListGuildMembersStep(discord.ListGuildMembersStepConfig[discord.ListGuildMembersInput]{StepType: "ListModerationMembers", ConnectionName: ConnectionName, Annotations: sdkgo.StepAnnotations{GroupID: "member-list", GroupLabel: "Member listing", Explanation: "List a bounded page of members in the test server."}, Connection: flow.connection, MapToOperationInput: func(input discord.ListGuildMembersInput) discord.ListGuildMembersInput { return input }, Listed: sdkgo.GoTo(moderationMembersListed{})})),
		dex.DefineStep(moderationMembersListed{}),
		dex.DefineStep(discord.NewAddGuildMemberRoleStep(discord.AddGuildMemberRoleStepConfig[discord.GuildMemberRoleInput]{StepType: "AddModerationRole", ConnectionName: ConnectionName, Annotations: sdkgo.StepAnnotations{GroupID: "role-add", GroupLabel: "Role add", Explanation: "Add one disposable role to the test member."}, Connection: flow.connection, MapToOperationInput: func(input discord.GuildMemberRoleInput) discord.GuildMemberRoleInput { return input }, Added: sdkgo.GoTo(moderationRoleAdded{})})),
		dex.DefineStep(moderationRoleAdded{}),
		dex.DefineStep(discord.NewRemoveGuildMemberRoleStep(discord.RemoveGuildMemberRoleStepConfig[discord.GuildMemberRoleInput]{StepType: "RemoveModerationRole", ConnectionName: ConnectionName, Annotations: sdkgo.StepAnnotations{GroupID: "role-remove", GroupLabel: "Role removal", Explanation: "Remove the disposable role from the test member."}, Connection: flow.connection, MapToOperationInput: func(input discord.GuildMemberRoleInput) discord.GuildMemberRoleInput { return input }, Removed: sdkgo.GoTo(moderationRoleRemoved{})})),
		dex.DefineStep(moderationRoleRemoved{}),
		dex.DefineStep(discord.NewTimeoutGuildMemberStep(discord.TimeoutGuildMemberStepConfig[discord.TimeoutGuildMemberInput]{StepType: "TimeoutModerationMember", ConnectionName: ConnectionName, Annotations: sdkgo.StepAnnotations{GroupID: "member-timeout", GroupLabel: "Member timeout", Explanation: "Set the test member's absolute communication timeout."}, Connection: flow.connection, MapToOperationInput: func(input discord.TimeoutGuildMemberInput) discord.TimeoutGuildMemberInput { return input }, TimedOut: sdkgo.GoTo(moderationMemberTimedOut{})})),
		dex.DefineStep(moderationMemberTimedOut{}),
		dex.DefineStep(discord.NewKickGuildMemberStep(discord.KickGuildMemberStepConfig[discord.KickGuildMemberInput]{StepType: "KickModerationMember", ConnectionName: ConnectionName, Annotations: sdkgo.StepAnnotations{GroupID: "member-kick", GroupLabel: "Member removal", Explanation: "Remove the test member from the disposable server."}, Connection: flow.connection, MapToOperationInput: func(input discord.KickGuildMemberInput) discord.KickGuildMemberInput { return input }, Kicked: sdkgo.GoTo(moderationMemberKicked{})})),
		dex.DefineStep(moderationMemberKicked{}),
		dex.DefineStep(discord.NewBanGuildMemberStep(discord.BanGuildMemberStepConfig[discord.BanGuildMemberInput]{StepType: "BanModerationMember", ConnectionName: ConnectionName, Annotations: sdkgo.StepAnnotations{GroupID: "member-ban", GroupLabel: "Member ban", Explanation: "Ban the removed test user without deleting message history."}, Connection: flow.connection, MapToOperationInput: func(input discord.BanGuildMemberInput) discord.BanGuildMemberInput { return input }, Banned: sdkgo.GoTo(moderationMemberBanned{})})),
		dex.DefineStep(moderationMemberBanned{}),
		dex.DefineStep(discord.NewUnbanGuildMemberStep(discord.UnbanGuildMemberStepConfig[discord.UnbanGuildMemberInput]{StepType: "UnbanModerationMember", ConnectionName: ConnectionName, Annotations: sdkgo.StepAnnotations{GroupID: "member-unban", GroupLabel: "Member unban", Explanation: "Remove the test user's ban while leaving them outside the server."}, Connection: flow.connection, MapToOperationInput: func(input discord.UnbanGuildMemberInput) discord.UnbanGuildMemberInput { return input }, Unbanned: sdkgo.GoTo(completeMemberModeration{})})),
		dex.DefineStep(completeMemberModeration{}),
	}
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Record explicit test member settings before the provider calls."
type startMemberModeration struct{ dex.StepDefaults }

func (startMemberModeration) GetStepType() string { return "StartMemberModeration" }
func (startMemberModeration) WaitFor(dex.Context, MemberModerationRequest) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startMemberModeration) Execute(ctx dex.Context, input MemberModerationRequest) (*dex.StepDecision, error) {
	if err := moderationRequestAttribute.Set(ctx, input); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[discord.ListGuildMembersInput]("ListModerationMembers"), discord.ListGuildMembersInput{GuildID: input.GuildID, Limit: 1}), nil
}

// dex:group group-id:member-listed group-label:"Listed members"
// dex:explanation text:"Continue with the explicit test member after listing the server."
type moderationMembersListed struct {
	dex.StepDefaultsNoWaitFor[discord.ListGuildMembersResult]
}

func (moderationMembersListed) GetStepType() string { return "ModerationMembersListed" }
func (moderationMembersListed) Execute(ctx dex.Context, _ discord.ListGuildMembersResult) (*dex.StepDecision, error) {
	request, err := moderationRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[discord.GuildMemberRoleInput]("AddModerationRole"), discord.GuildMemberRoleInput{GuildID: request.GuildID, UserID: request.UserID, RoleID: request.RoleID}), nil
}

// dex:group group-id:role-added group-label:"Role added"
// dex:explanation text:"Remove the same role from the same test member."
type moderationRoleAdded struct {
	dex.StepDefaultsNoWaitFor[discord.AddGuildMemberRoleResult]
}

func (moderationRoleAdded) GetStepType() string { return "ModerationRoleAdded" }
func (moderationRoleAdded) Execute(_ dex.Context, result discord.AddGuildMemberRoleResult) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[discord.GuildMemberRoleInput]("RemoveModerationRole"), discord.GuildMemberRoleInput{GuildID: result.Value.GuildID, UserID: result.Value.UserID, RoleID: result.Value.RoleID}), nil
}

// dex:group group-id:role-removed group-label:"Role removed"
// dex:explanation text:"Apply the requested absolute timeout to the test member."
type moderationRoleRemoved struct {
	dex.StepDefaultsNoWaitFor[discord.RemoveGuildMemberRoleResult]
}

func (moderationRoleRemoved) GetStepType() string { return "ModerationRoleRemoved" }
func (moderationRoleRemoved) Execute(ctx dex.Context, result discord.RemoveGuildMemberRoleResult) (*dex.StepDecision, error) {
	request, err := moderationRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[discord.TimeoutGuildMemberInput]("TimeoutModerationMember"), discord.TimeoutGuildMemberInput{GuildID: result.Value.GuildID, UserID: result.Value.UserID, TimeoutUntil: request.TimeoutUntil}), nil
}

// dex:group group-id:member-timed-out group-label:"Timed out member"
// dex:explanation text:"Remove only the test member named in the request."
type moderationMemberTimedOut struct {
	dex.StepDefaultsNoWaitFor[discord.TimeoutGuildMemberResult]
}

func (moderationMemberTimedOut) GetStepType() string { return "ModerationMemberTimedOut" }
func (moderationMemberTimedOut) Execute(_ dex.Context, result discord.TimeoutGuildMemberResult) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[discord.KickGuildMemberInput]("KickModerationMember"), discord.KickGuildMemberInput{GuildID: result.Value.GuildID, UserID: result.Value.UserID}), nil
}

// dex:group group-id:member-kicked group-label:"Removed member"
// dex:explanation text:"Ban the removed test user without deleting their prior messages."
type moderationMemberKicked struct {
	dex.StepDefaultsNoWaitFor[discord.KickGuildMemberResult]
}

func (moderationMemberKicked) GetStepType() string { return "ModerationMemberKicked" }
func (moderationMemberKicked) Execute(_ dex.Context, result discord.KickGuildMemberResult) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[discord.BanGuildMemberInput]("BanModerationMember"), discord.BanGuildMemberInput{GuildID: result.Value.GuildID, UserID: result.Value.UserID}), nil
}

// dex:group group-id:member-banned group-label:"Banned member"
// dex:explanation text:"Remove the test user's ban after the ban succeeds."
type moderationMemberBanned struct {
	dex.StepDefaultsNoWaitFor[discord.BanGuildMemberResult]
}

func (moderationMemberBanned) GetStepType() string { return "ModerationMemberBanned" }
func (moderationMemberBanned) Execute(_ dex.Context, result discord.BanGuildMemberResult) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[discord.UnbanGuildMemberInput]("UnbanModerationMember"), discord.UnbanGuildMemberInput{GuildID: result.Value.GuildID, UserID: result.Value.UserID}), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Record the unbanned user ID and complete the Flow."
type completeMemberModeration struct {
	dex.StepDefaultsNoWaitFor[discord.UnbanGuildMemberResult]
}

func (completeMemberModeration) GetStepType() string { return "CompleteMemberModeration" }
func (completeMemberModeration) Execute(ctx dex.Context, result discord.UnbanGuildMemberResult) (*dex.StepDecision, error) {
	if err := moderationResultAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*MemberModerationFlow)(nil)
