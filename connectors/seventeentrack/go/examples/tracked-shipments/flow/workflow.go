// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package shipmentlist runs SeventeenTrack listTrackedShipments as one Dex Flow.
package shipmentlist

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/seventeentrack/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "SeventeenTrackShipmentList"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "seventeentrack"
	operationStepType = "CallListTrackedShipments"
)

var shipmentListOutcomeAttribute = dex.DefineAttribute[seventeentrack.TrackedShipmentPage]("seventeentrack-shipment-list-outcome")

// Flow runs ListTrackedShipments once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection seventeentrack.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection seventeentrack.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{shipmentListOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(seventeentrack.NewListTrackedShipmentsStep(seventeentrack.ListTrackedShipmentsStepConfig[seventeentrack.ListTrackedShipmentsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "SeventeenTrack", Explanation: "List SeventeenTrack registered shipments."},
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

// dex:field attribute-key:seventeentrack-shipment-list-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readTrackedShipmentPageOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"seventeentrack-shipment-list-outcome": outcome}}, nil
}

// dex:field attribute-key:seventeentrack-shipment-list-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readTrackedShipmentPageOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"seventeentrack-shipment-list-outcome": outcome}}, nil
}

func readTrackedShipmentPageOutcome(ctx dex.Context) (seventeentrack.TrackedShipmentPage, error) {
	outcome, err := shipmentListOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return seventeentrack.TrackedShipmentPage{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input seventeentrack.ListTrackedShipmentsInput) seventeentrack.ListTrackedShipmentsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListTrackedShipments" }
func (startRequest) WaitFor(dex.Context, seventeentrack.ListTrackedShipmentsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input seventeentrack.ListTrackedShipmentsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[seventeentrack.ListTrackedShipmentsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[seventeentrack.ListTrackedShipmentsResult]
}

func (completeResult) GetStepType() string { return "CompleteListTrackedShipments" }
func (completeResult) Execute(ctx dex.Context, result seventeentrack.ListTrackedShipmentsResult) (*dex.StepDecision, error) {
	if err := shipmentListOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
