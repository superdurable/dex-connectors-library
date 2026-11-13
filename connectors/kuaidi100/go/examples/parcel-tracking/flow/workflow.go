// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package parceltracking runs Kuaidi100 queryTracking as one Dex Flow.
package parceltracking

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/kuaidi100/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "Kuaidi100Tracking"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "kuaidi100"
	operationStepType = "CallQueryTracking"
)

var trackingOutcomeAttribute = dex.DefineAttribute[kuaidi100.QueryTrackingOutput]("kuaidi100-parcel-tracking-outcome")

// Flow runs QueryTracking once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection kuaidi100.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection kuaidi100.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{trackingOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(kuaidi100.NewQueryTrackingStep(kuaidi100.QueryTrackingStepConfig[kuaidi100.QueryTrackingInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Kuaidi100", Explanation: "Read the Kuaidi100 parcel tracking history."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Retrieved:           sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:kuaidi100-parcel-tracking-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readTrackingOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"kuaidi100-parcel-tracking-outcome": outcome}}, nil
}

// dex:field attribute-key:kuaidi100-parcel-tracking-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readTrackingOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"kuaidi100-parcel-tracking-outcome": outcome}}, nil
}

func readTrackingOutcome(ctx dex.Context) (kuaidi100.QueryTrackingOutput, error) {
	outcome, err := trackingOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return kuaidi100.QueryTrackingOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input kuaidi100.QueryTrackingInput) kuaidi100.QueryTrackingInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartQueryTracking" }
func (startRequest) WaitFor(dex.Context, kuaidi100.QueryTrackingInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input kuaidi100.QueryTrackingInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[kuaidi100.QueryTrackingInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[kuaidi100.QueryTrackingResult]
}

func (completeResult) GetStepType() string { return "CompleteQueryTracking" }
func (completeResult) Execute(ctx dex.Context, result kuaidi100.QueryTrackingResult) (*dex.StepDecision, error) {
	if err := trackingOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
