// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package iplookup runs HAIMOU lookupIPLocation as one Dex Flow.
package iplookup

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/haimou/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "HAIMOULocationLookup"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "haimou"
	operationStepType = "CallLookupIPLocation"
)

var ipLocationOutcomeAttribute = dex.DefineAttribute[haimou.LookupIPLocationOutput]("haimou-lookup-ip-location-outcome")

// Flow runs LookupIPLocation once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection haimou.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection haimou.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{ipLocationOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(haimou.NewLookupIPLocationStep(haimou.LookupIPLocationStepConfig[haimou.LookupIPLocationInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "HAIMOU", Explanation: "Read country, province, city, district, and ISP for the supplied public IPv4 address."},
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

// dex:field attribute-key:haimou-lookup-ip-location-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readLookupIPLocationOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"haimou-lookup-ip-location-outcome": outcome}}, nil
}

// dex:field attribute-key:haimou-lookup-ip-location-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readLookupIPLocationOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"haimou-lookup-ip-location-outcome": outcome}}, nil
}

func readLookupIPLocationOutcome(ctx dex.Context) (haimou.LookupIPLocationOutput, error) {
	outcome, err := ipLocationOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return haimou.LookupIPLocationOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input haimou.LookupIPLocationInput) haimou.LookupIPLocationInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartLookupIPLocation" }
func (startRequest) WaitFor(dex.Context, haimou.LookupIPLocationInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input haimou.LookupIPLocationInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[haimou.LookupIPLocationInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[haimou.LookupIPLocationResult]
}

func (completeResult) GetStepType() string { return "CompleteLookupIPLocation" }
func (completeResult) Execute(ctx dex.Context, result haimou.LookupIPLocationResult) (*dex.StepDecision, error) {
	if err := ipLocationOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
