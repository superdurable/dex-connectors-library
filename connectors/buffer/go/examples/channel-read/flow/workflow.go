// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package channelread runs Buffer getChannel as one Dex Flow.
package channelread

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/buffer/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "BufferChannelRead"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "buffer"
	operationStepType = "CallGetChannel"
)

var channelAttribute = dex.DefineAttribute[buffer.ChannelProfile]("buffer-channel-read")

// Flow runs GetChannel once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection buffer.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection buffer.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{channelAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(buffer.NewGetChannelStep(buffer.GetChannelStepConfig[buffer.GetChannelInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Buffer", Explanation: "Find one channel by ID in Buffer."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Found:               sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:buffer-channel-read value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readChannel(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"buffer-channel-read": outcome}}, nil
}

// dex:field attribute-key:buffer-channel-read value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readChannel(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"buffer-channel-read": outcome}}, nil
}

func readChannel(ctx dex.Context) (buffer.ChannelProfile, error) {
	outcome, err := channelAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return buffer.ChannelProfile{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input buffer.GetChannelInput) buffer.GetChannelInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetChannel" }
func (startRequest) WaitFor(dex.Context, buffer.GetChannelInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input buffer.GetChannelInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[buffer.GetChannelInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[buffer.GetChannelResult]
}

func (completeResult) GetStepType() string { return "CompleteGetChannel" }
func (completeResult) Execute(ctx dex.Context, result buffer.GetChannelResult) (*dex.StepDecision, error) {
	if err := channelAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
