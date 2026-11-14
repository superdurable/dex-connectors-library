// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package statuscounts runs FiftyOneTracking getShipmentStatusCounts as one Dex Flow.
package statuscounts

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/fiftyonetracking/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "FiftyOneTrackingShipmentStatusCounts"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "fiftyonetracking"
	operationStepType = "CallGetShipmentStatusCounts"
)

var statusCountsOutcomeAttribute = dex.DefineAttribute[fiftyonetracking.ShipmentStatusCounts]("fiftyonetracking-shipment-status-counts-outcome")

// Flow runs GetShipmentStatusCounts once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection fiftyonetracking.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection fiftyonetracking.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{statusCountsOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(fiftyonetracking.NewGetShipmentStatusCountsStep(fiftyonetracking.GetShipmentStatusCountsStepConfig[fiftyonetracking.GetShipmentStatusCountsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "FiftyOneTracking", Explanation: "Count 51Tracking parcels by delivery status."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Counted:             sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:fiftyonetracking-shipment-status-counts-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readShipmentStatusCountsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"fiftyonetracking-shipment-status-counts-outcome": outcome}}, nil
}

// dex:field attribute-key:fiftyonetracking-shipment-status-counts-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readShipmentStatusCountsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"fiftyonetracking-shipment-status-counts-outcome": outcome}}, nil
}

func readShipmentStatusCountsOutcome(ctx dex.Context) (fiftyonetracking.ShipmentStatusCounts, error) {
	outcome, err := statusCountsOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return fiftyonetracking.ShipmentStatusCounts{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input fiftyonetracking.GetShipmentStatusCountsInput) fiftyonetracking.GetShipmentStatusCountsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetShipmentStatusCounts" }
func (startRequest) WaitFor(dex.Context, fiftyonetracking.GetShipmentStatusCountsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input fiftyonetracking.GetShipmentStatusCountsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[fiftyonetracking.GetShipmentStatusCountsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[fiftyonetracking.GetShipmentStatusCountsResult]
}

func (completeResult) GetStepType() string { return "CompleteGetShipmentStatusCounts" }
func (completeResult) Execute(ctx dex.Context, result fiftyonetracking.GetShipmentStatusCountsResult) (*dex.StepDecision, error) {
	if err := statusCountsOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
