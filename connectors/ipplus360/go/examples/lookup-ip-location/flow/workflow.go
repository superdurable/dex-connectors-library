// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package iplookup runs IPPlus360 lookupIPLocation as one Dex Flow.
package iplookup

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/ipplus360/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "IPPlus360LocationLookup"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "ipplus360"
	operationStepType = "CallLookupIPLocation"
)

var ipLocationOutcomeAttribute = dex.DefineAttribute[ipplus360.LookupIPLocationOutput]("ipplus360-lookup-ip-location-outcome")

// Flow runs LookupIPLocation once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection ipplus360.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection ipplus360.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{ipLocationOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(ipplus360.NewLookupIPLocationStep(ipplus360.LookupIPLocationStepConfig[ipplus360.LookupIPLocationInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "IPPlus360", Explanation: "Read country, province, city, district, and operator for the supplied public IPv4 address."},
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

// dex:field attribute-key:ipplus360-lookup-ip-location-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readLookupIPLocationOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"ipplus360-lookup-ip-location-outcome": outcome}}, nil
}

// dex:field attribute-key:ipplus360-lookup-ip-location-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readLookupIPLocationOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"ipplus360-lookup-ip-location-outcome": outcome}}, nil
}

func readLookupIPLocationOutcome(ctx dex.Context) (ipplus360.LookupIPLocationOutput, error) {
	outcome, err := ipLocationOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return ipplus360.LookupIPLocationOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input ipplus360.LookupIPLocationInput) ipplus360.LookupIPLocationInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartLookupIPLocation" }
func (startRequest) WaitFor(dex.Context, ipplus360.LookupIPLocationInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input ipplus360.LookupIPLocationInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[ipplus360.LookupIPLocationInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[ipplus360.LookupIPLocationResult]
}

func (completeResult) GetStepType() string { return "CompleteLookupIPLocation" }
func (completeResult) Execute(ctx dex.Context, result ipplus360.LookupIPLocationResult) (*dex.StepDecision, error) {
	if err := ipLocationOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
