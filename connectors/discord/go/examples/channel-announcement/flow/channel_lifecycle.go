// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package channelannouncement

import (
	"github.com/superdurable/dex-connectors-library/connectors/discord/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// ChannelLifecycleFlowType is the stable identity of the channel lifecycle example.
const ChannelLifecycleFlowType = "DiscordChannelLifecycle"

var lifecycleCreatedChannelAttribute = dex.DefineAttribute[discord.Channel]("discord-lifecycle-created-channel")
var lifecycleDeletedChannelAttribute = dex.DefineAttribute[discord.Channel]("discord-lifecycle-deleted-channel")

// ChannelLifecycleFlow creates, updates, lists, and deletes a temporary text channel.
type ChannelLifecycleFlow struct {
	dex.FlowDefaults
	connection discord.Connection
}

// NewChannelLifecycleFlow binds the static Discord connection.
func NewChannelLifecycleFlow(connection discord.Connection) *ChannelLifecycleFlow {
	return &ChannelLifecycleFlow{connection: connection}
}

func (*ChannelLifecycleFlow) GetFlowType() string { return ChannelLifecycleFlowType }
func (*ChannelLifecycleFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{lifecycleCreatedChannelAttribute, lifecycleDeletedChannelAttribute}}
}
func (flow *ChannelLifecycleFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:discord-lifecycle-created-channel value-type:json editable:false description:"Created channel"
// dex:field attribute-key:discord-lifecycle-deleted-channel value-type:json editable:false description:"Deleted channel"
// GetDexSummary shows the channel lifecycle identities.
func (*ChannelLifecycleFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	created, err := optionalAttribute(ctx, lifecycleCreatedChannelAttribute)
	if err != nil {
		return nil, err
	}
	deleted, err := optionalAttribute(ctx, lifecycleDeletedChannelAttribute)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"discord-lifecycle-created-channel": created, "discord-lifecycle-deleted-channel": deleted}}, nil
}

// dex:field attribute-key:discord-lifecycle-created-channel value-type:json editable:false description:"Created channel"
// dex:field attribute-key:discord-lifecycle-deleted-channel value-type:json editable:false description:"Deleted channel"
// GetDexDisplay shows the channel lifecycle identities.
func (*ChannelLifecycleFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	created, err := optionalAttribute(ctx, lifecycleCreatedChannelAttribute)
	if err != nil {
		return nil, err
	}
	deleted, err := optionalAttribute(ctx, lifecycleDeletedChannelAttribute)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"discord-lifecycle-created-channel": created, "discord-lifecycle-deleted-channel": deleted}}, nil
}

func (flow *ChannelLifecycleFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startChannelLifecycle{}),
		dex.DefineStep(discord.NewCreateGuildChannelStep(discord.CreateGuildChannelStepConfig[discord.CreateGuildChannelInput]{
			StepType: "CreateLifecycleChannel", ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "channel-create", GroupLabel: "Channel creation", Explanation: "Create a temporary text channel with bounded sends."},
			Connection:  flow.connection, MapToOperationInput: func(input discord.CreateGuildChannelInput) discord.CreateGuildChannelInput { return input },
			Created: sdkgo.GoTo(lifecycleChannelCreated{}),
		})),
		dex.DefineStep(lifecycleChannelCreated{}),
		dex.DefineStep(discord.NewUpdateChannelStep(discord.UpdateChannelStepConfig[discord.UpdateChannelInput]{
			StepType: "UpdateLifecycleChannel", ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "channel-update", GroupLabel: "Channel update", Explanation: "Replace the temporary channel's topic."},
			Connection:  flow.connection, MapToOperationInput: func(input discord.UpdateChannelInput) discord.UpdateChannelInput { return input },
			Updated: sdkgo.GoTo(lifecycleChannelUpdated{}),
		})),
		dex.DefineStep(lifecycleChannelUpdated{}),
		dex.DefineStep(discord.NewListGuildChannelsStep(discord.ListGuildChannelsStepConfig[discord.ListGuildChannelsInput]{
			StepType: "ListLifecycleChannels", ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "channel-list", GroupLabel: "Channel listing", Explanation: "List visible channels in the temporary channel's server."},
			Connection:  flow.connection, MapToOperationInput: func(input discord.ListGuildChannelsInput) discord.ListGuildChannelsInput { return input },
			Listed: sdkgo.GoTo(lifecycleChannelsListed{}),
		})),
		dex.DefineStep(lifecycleChannelsListed{}),
		dex.DefineStep(discord.NewDeleteChannelStep(discord.DeleteChannelStepConfig[discord.DeleteChannelInput]{
			StepType: "DeleteLifecycleChannel", ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "channel-delete", GroupLabel: "Channel deletion", Explanation: "Delete the temporary channel created by this Flow."},
			Connection:  flow.connection, MapToOperationInput: func(input discord.DeleteChannelInput) discord.DeleteChannelInput { return input },
			Deleted: sdkgo.GoTo(completeChannelLifecycle{}),
		})),
		dex.DefineStep(completeChannelLifecycle{}),
	}
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the server and temporary channel settings to Discord."
type startChannelLifecycle struct{ dex.StepDefaults }

func (startChannelLifecycle) GetStepType() string { return "StartChannelLifecycle" }
func (startChannelLifecycle) WaitFor(dex.Context, discord.CreateGuildChannelInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startChannelLifecycle) Execute(_ dex.Context, input discord.CreateGuildChannelInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[discord.CreateGuildChannelInput]("CreateLifecycleChannel"), input), nil
}

// dex:group group-id:channel-created group-label:"Created channel"
// dex:explanation text:"Save the created ID and update only this new channel."
type lifecycleChannelCreated struct {
	dex.StepDefaultsNoWaitFor[discord.CreateGuildChannelResult]
}

func (lifecycleChannelCreated) GetStepType() string { return "LifecycleChannelCreated" }
func (lifecycleChannelCreated) Execute(ctx dex.Context, result discord.CreateGuildChannelResult) (*dex.StepDecision, error) {
	if err := lifecycleCreatedChannelAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	topic := result.Value.Topic
	return dex.GoTo(sdkgo.StepRef[discord.UpdateChannelInput]("UpdateLifecycleChannel"), discord.UpdateChannelInput{ChannelID: result.Value.ID, Topic: &topic}), nil
}

// dex:group group-id:channel-updated group-label:"Updated channel"
// dex:explanation text:"List the server channels after the safe topic update."
type lifecycleChannelUpdated struct {
	dex.StepDefaultsNoWaitFor[discord.UpdateChannelResult]
}

func (lifecycleChannelUpdated) GetStepType() string { return "LifecycleChannelUpdated" }
func (lifecycleChannelUpdated) Execute(_ dex.Context, result discord.UpdateChannelResult) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[discord.ListGuildChannelsInput]("ListLifecycleChannels"), discord.ListGuildChannelsInput{GuildID: result.Value.GuildID}), nil
}

// dex:group group-id:channels-listed group-label:"Listed channels"
// dex:explanation text:"Delete only the channel whose ID this Flow created."
type lifecycleChannelsListed struct {
	dex.StepDefaultsNoWaitFor[discord.ListGuildChannelsResult]
}

func (lifecycleChannelsListed) GetStepType() string { return "LifecycleChannelsListed" }
func (lifecycleChannelsListed) Execute(ctx dex.Context, _ discord.ListGuildChannelsResult) (*dex.StepDecision, error) {
	created, err := lifecycleCreatedChannelAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[discord.DeleteChannelInput]("DeleteLifecycleChannel"), discord.DeleteChannelInput{ChannelID: created.ID}), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Store the deleted channel identity and complete the Flow."
type completeChannelLifecycle struct {
	dex.StepDefaultsNoWaitFor[discord.DeleteChannelResult]
}

func (completeChannelLifecycle) GetStepType() string { return "CompleteChannelLifecycle" }
func (completeChannelLifecycle) Execute(ctx dex.Context, result discord.DeleteChannelResult) (*dex.StepDecision, error) {
	if err := lifecycleDeletedChannelAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*ChannelLifecycleFlow)(nil)
