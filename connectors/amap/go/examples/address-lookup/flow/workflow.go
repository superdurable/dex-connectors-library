// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package addresslookup runs Amap geocodeAddress as one Dex Flow.
package addresslookup

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/amap/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "AmapAddressLookup"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "amap"
	operationStepType = "CallGeocodeAddress"
)

var addressLookupOutcomeAttribute = dex.DefineAttribute[amap.GeocodeAddressOutput]("amap-address-lookup-outcome")

// Flow runs GeocodeAddress once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection amap.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection amap.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{addressLookupOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(amap.NewGeocodeAddressStep(amap.GeocodeAddressStepConfig[amap.GeocodeAddressInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Amap", Explanation: "Call the provider's geocodeAddress operation."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Geocoded:            sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:amap-address-lookup-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readAddressLookupOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"amap-address-lookup-outcome": outcome}}, nil
}

// dex:field attribute-key:amap-address-lookup-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readAddressLookupOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"amap-address-lookup-outcome": outcome}}, nil
}

func readAddressLookupOutcome(ctx dex.Context) (amap.GeocodeAddressOutput, error) {
	outcome, err := addressLookupOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return amap.GeocodeAddressOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input amap.GeocodeAddressInput) amap.GeocodeAddressInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGeocodeAddress" }
func (startRequest) WaitFor(dex.Context, amap.GeocodeAddressInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input amap.GeocodeAddressInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[amap.GeocodeAddressInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[amap.GeocodeAddressResult]
}

func (completeResult) GetStepType() string { return "CompleteGeocodeAddress" }
func (completeResult) Execute(ctx dex.Context, result amap.GeocodeAddressResult) (*dex.StepDecision, error) {
	if err := addressLookupOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
