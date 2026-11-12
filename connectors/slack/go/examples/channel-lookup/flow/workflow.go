// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package channellookup demonstrates the Slack getWorkspaceIdentity and listChannels Steps in a runnable Dex Flow.
package channellookup

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/slack/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// FlowType is the stable Dex identity for this example.
const FlowType = "SlackChannelLookup"

// ConnectionName is the static Dex Web connection used by the example.
const ConnectionName = "slack-channel-lookup"

const (
	workspaceIdentityStepType = "GetWorkspaceIdentitySlack"
	listChannelsStepType      = "ListChannelsSlack"
)

var (
	lookupRequestAttribute     = dex.DefineAttribute[LookupRequest]("slack-channel-lookup-request")
	workspaceIdentityAttribute = dex.DefineAttribute[slack.GetWorkspaceIdentityOutput]("slack-channel-lookup-workspace")
	channelPageAttribute       = dex.DefineAttribute[slack.ListChannelsOutput]("slack-channel-lookup-channels")
)

// LookupRequest selects the channel page entered in Dex Web Start Flow, such as {"nameFilter":"#general"}.
type LookupRequest = slack.ListChannelsInput

// LookupOutput names the connected workspace and the channels on the requested page.
type LookupOutput struct {
	// Workspace identifies the Slack workspace and bot behind the connection.
	Workspace slack.GetWorkspaceIdentityOutput `json:"workspace"`
	// Channels is the filtered channel page, including the cursor for the next page.
	Channels slack.ListChannelsOutput `json:"channels"`
}

// Flow reads the connection's workspace identity, then one page of matching channels.
type Flow struct {
	dex.FlowDefaults
	connection slack.Connection
}

// NewFlow binds the connection at registration time.
func NewFlow(connection slack.Connection) *Flow { return &Flow{connection: connection} }

// GetFlowType returns the stable Flow type.
func (*Flow) GetFlowType() string { return FlowType }

// GetPersistenceSchema registers the request, workspace, and channel page Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{lookupRequestAttribute, workspaceIdentityAttribute, channelPageAttribute}}
}

// GetRPCs registers read-only summary and display RPCs for Dex Web.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the stored request, workspace, and channel page when available.
// dex:field attribute-key:slack-channel-lookup-request value-type:object editable:false description:"Slack channel lookup request"
// dex:field attribute-key:slack-channel-lookup-workspace value-type:json editable:false description:"Connected Slack workspace"
// dex:field attribute-key:slack-channel-lookup-channels value-type:json editable:false description:"Matching Slack channels"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, workspace, channels, err := lookupInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"slack-channel-lookup-request": request, "slack-channel-lookup-workspace": workspace, "slack-channel-lookup-channels": channels,
	}}, nil
}

// GetDexDisplay returns the stored request, workspace, and channel page when available.
// dex:field attribute-key:slack-channel-lookup-request value-type:object editable:false description:"Slack channel lookup request"
// dex:field attribute-key:slack-channel-lookup-workspace value-type:json editable:false description:"Connected Slack workspace"
// dex:field attribute-key:slack-channel-lookup-channels value-type:json editable:false description:"Matching Slack channels"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, workspace, channels, err := lookupInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"slack-channel-lookup-request": request, "slack-channel-lookup-workspace": workspace, "slack-channel-lookup-channels": channels,
	}}, nil
}

func lookupInspection(ctx dex.Context) (LookupRequest, slack.GetWorkspaceIdentityOutput, slack.ListChannelsOutput, error) {
	request, err := optionalAttribute(ctx, lookupRequestAttribute)
	if err != nil {
		return LookupRequest{}, slack.GetWorkspaceIdentityOutput{}, slack.ListChannelsOutput{}, err
	}
	workspace, err := optionalAttribute(ctx, workspaceIdentityAttribute)
	if err != nil {
		return LookupRequest{}, slack.GetWorkspaceIdentityOutput{}, slack.ListChannelsOutput{}, err
	}
	channels, err := optionalAttribute(ctx, channelPageAttribute)
	return request, workspace, channels, err
}

func optionalAttribute[T any](ctx dex.Context, attribute dex.Attribute[T]) (T, error) {
	value, err := attribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		var zero T
		return zero, nil
	}
	return value, err
}

// GetSteps registers the start, provider, and completion Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startLookup{}),
		dex.DefineStep(slack.NewGetWorkspaceIdentityStep(slack.GetWorkspaceIdentityStepConfig[LookupRequest]{
			StepType: workspaceIdentityStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "slack", GroupLabel: "Slack", Explanation: "Read the Slack workspace and bot behind the connection."},
			MapToOperationInput: MapToWorkspaceIdentityInput,
			Read:                sdkgo.GoTo(recordWorkspace{}),
		})),
		dex.DefineStep(recordWorkspace{}),
		dex.DefineStep(slack.NewListChannelsStep(slack.ListChannelsStepConfig[LookupRequest]{
			StepType: listChannelsStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "slack", GroupLabel: "Slack", Explanation: "Read one page of Slack channels that match the request."},
			MapToOperationInput: MapToListChannelsInput,
			Listed:              sdkgo.GoTo(completeLookup{}),
		})),
		dex.DefineStep(completeLookup{}),
	}
}

// MapToWorkspaceIdentityInput ignores the request because the connection determines the workspace.
func MapToWorkspaceIdentityInput(LookupRequest) slack.GetWorkspaceIdentityInput {
	return slack.GetWorkspaceIdentityInput{}
}

// MapToListChannelsInput passes the typed start request to the channel listing Step.
func MapToListChannelsInput(request LookupRequest) slack.ListChannelsInput { return request }

// dex:group group-id:slack group-label:"Slack"
// dex:explanation text:"Store the channel lookup request and read the workspace first."
type startLookup struct{ dex.StepDefaults }

func (startLookup) GetStepType() string { return "StartSlackChannelLookup" }
func (startLookup) WaitFor(dex.Context, LookupRequest) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startLookup) Execute(ctx dex.Context, request LookupRequest) (*dex.StepDecision, error) {
	if err := lookupRequestAttribute.Set(ctx, request); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[LookupRequest](workspaceIdentityStepType), request), nil
}

// dex:group group-id:slack group-label:"Slack"
// dex:explanation text:"Store the workspace identity, then list the requested channels."
type recordWorkspace struct {
	dex.StepDefaultsNoWaitFor[slack.GetWorkspaceIdentityResult]
}

func (recordWorkspace) GetStepType() string { return "RecordSlackWorkspace" }
func (recordWorkspace) Execute(ctx dex.Context, result slack.GetWorkspaceIdentityResult) (*dex.StepDecision, error) {
	if err := workspaceIdentityAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	request, err := lookupRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[LookupRequest](listChannelsStepType), request), nil
}

// dex:group group-id:slack group-label:"Slack"
// dex:explanation text:"Complete with the workspace and the matching channels."
type completeLookup struct {
	dex.StepDefaultsNoWaitFor[slack.ListChannelsResult]
}

func (completeLookup) GetStepType() string { return "CompleteSlackChannelLookup" }
func (completeLookup) Execute(ctx dex.Context, result slack.ListChannelsResult) (*dex.StepDecision, error) {
	if err := channelPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	workspace, err := workspaceIdentityAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return dex.GracefulComplete(LookupOutput{Workspace: workspace, Channels: result.Value}), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
