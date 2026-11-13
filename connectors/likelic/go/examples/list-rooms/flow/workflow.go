// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package roomcatalog runs Likelic listRooms as one Dex Flow.
package roomcatalog

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/likelic/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "LikelicRoomCatalog"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "likelic"
	operationStepType = "CallListRooms"
)

var roomListOutcomeAttribute = dex.DefineAttribute[likelic.ListRoomsOutput]("likelic-list-rooms-outcome")

// Flow runs ListRooms once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection likelic.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection likelic.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{roomListOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(likelic.NewListRoomsStep(likelic.ListRoomsStepConfig[likelic.ListRoomsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Likelic", Explanation: "List the rooms visible to this API Key."},
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

// dex:field attribute-key:likelic-list-rooms-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readListRoomsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"likelic-list-rooms-outcome": outcome}}, nil
}

// dex:field attribute-key:likelic-list-rooms-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readListRoomsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"likelic-list-rooms-outcome": outcome}}, nil
}

func readListRoomsOutcome(ctx dex.Context) (likelic.ListRoomsOutput, error) {
	outcome, err := roomListOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return likelic.ListRoomsOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input likelic.ListRoomsInput) likelic.ListRoomsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListRooms" }
func (startRequest) WaitFor(dex.Context, likelic.ListRoomsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input likelic.ListRoomsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[likelic.ListRoomsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[likelic.ListRoomsResult]
}

func (completeResult) GetStepType() string { return "CompleteListRooms" }
func (completeResult) Execute(ctx dex.Context, result likelic.ListRoomsResult) (*dex.StepDecision, error) {
	if err := roomListOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
