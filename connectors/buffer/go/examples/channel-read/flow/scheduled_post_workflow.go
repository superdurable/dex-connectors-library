// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package channelread

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/buffer/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// ScheduledPostFlowType is the stable scheduled-post example identity.
const ScheduledPostFlowType = "BufferScheduledPostCreate"

const scheduledPostStepType = "CallCreateScheduledPost"

var scheduledPostAttribute = dex.DefineAttribute[buffer.PostProfile]("buffer-scheduled-post")

// ScheduledPostFlow creates a text post at the requested UTC time.
type ScheduledPostFlow struct {
	dex.FlowDefaults
	connection buffer.Connection
}

// NewScheduledPostFlow binds the provider connection at registration time.
func NewScheduledPostFlow(connection buffer.Connection) *ScheduledPostFlow {
	return &ScheduledPostFlow{connection: connection}
}

func (*ScheduledPostFlow) GetFlowType() string { return ScheduledPostFlowType }

func (*ScheduledPostFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{scheduledPostAttribute}}
}

func (flow *ScheduledPostFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startScheduledPost{}),
		dex.DefineStep(buffer.NewCreateScheduledPostStep(buffer.CreateScheduledPostStepConfig[buffer.CreateScheduledPostInput]{
			StepType: scheduledPostStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Buffer", Explanation: "Schedule a text post on the selected Buffer channel."},
			Connection:  flow.connection, MapToOperationInput: mapScheduledPostInput,
			Created: sdkgo.GoTo(completeScheduledPost{}),
		})),
		dex.DefineStep(completeScheduledPost{}),
	}
}

func (flow *ScheduledPostFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:buffer-scheduled-post value-type:json editable:false description:"Scheduled post"
// GetDexSummary shows the scheduled post in Dex Web.
func (*ScheduledPostFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	post, err := readScheduledPost(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"buffer-scheduled-post": post}}, nil
}

// dex:field attribute-key:buffer-scheduled-post value-type:json editable:false description:"Scheduled post"
// GetDexDisplay shows the scheduled post in Dex Web.
func (*ScheduledPostFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	post, err := readScheduledPost(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"buffer-scheduled-post": post}}, nil
}

func readScheduledPost(ctx dex.Context) (buffer.PostProfile, error) {
	post, err := scheduledPostAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return buffer.PostProfile{}, nil
	}
	return post, err
}

func mapScheduledPostInput(input buffer.CreateScheduledPostInput) buffer.CreateScheduledPostInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the channel, text, and UTC timestamp to the provider Step."
type startScheduledPost struct{ dex.StepDefaults }

func (startScheduledPost) GetStepType() string { return "StartCreateScheduledPost" }
func (startScheduledPost) WaitFor(dex.Context, buffer.CreateScheduledPostInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startScheduledPost) Execute(_ dex.Context, input buffer.CreateScheduledPostInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[buffer.CreateScheduledPostInput](scheduledPostStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the scheduled post and complete the Flow."
type completeScheduledPost struct {
	dex.StepDefaultsNoWaitFor[buffer.CreateScheduledPostResult]
}

func (completeScheduledPost) GetStepType() string { return "CompleteCreateScheduledPost" }
func (completeScheduledPost) Execute(ctx dex.Context, result buffer.CreateScheduledPostResult) (*dex.StepDecision, error) {
	if err := scheduledPostAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*ScheduledPostFlow)(nil)
