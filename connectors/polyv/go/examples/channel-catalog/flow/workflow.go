// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package channelcatalog runs Polyv listChannels as one Dex Flow.
package channelcatalog

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/polyv/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "PolyvChannelCatalog"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "polyv"
	operationStepType = "CallListChannels"
)

var channelCatalogOutcomeAttribute = dex.DefineAttribute[polyv.ListChannelsOutput]("polyv-channel-catalog-outcome")

// Flow runs ListChannels once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection polyv.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection polyv.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{channelCatalogOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(polyv.NewListChannelsStep(polyv.ListChannelsStepConfig[polyv.ListChannelsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Polyv", Explanation: "Read account channel IDs without channel configuration."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Listed:              sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:polyv-channel-catalog-outcome value-type:json editable:false description:"Channel catalog"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readChannelCatalogOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"polyv-channel-catalog-outcome": outcome}}, nil
}

// dex:field attribute-key:polyv-channel-catalog-outcome value-type:json editable:false description:"Channel catalog"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readChannelCatalogOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"polyv-channel-catalog-outcome": outcome}}, nil
}

func readChannelCatalogOutcome(ctx dex.Context) (polyv.ListChannelsOutput, error) {
	outcome, err := channelCatalogOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return polyv.ListChannelsOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input polyv.ListChannelsInput) polyv.ListChannelsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListChannels" }
func (startRequest) WaitFor(dex.Context, polyv.ListChannelsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input polyv.ListChannelsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[polyv.ListChannelsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[polyv.ListChannelsResult]
}

func (completeResult) GetStepType() string { return "CompleteListChannels" }
func (completeResult) Execute(ctx dex.Context, result polyv.ListChannelsResult) (*dex.StepDecision, error) {
	if err := channelCatalogOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
