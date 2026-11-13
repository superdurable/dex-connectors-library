// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package sitetdk runs Chinaz getSiteTDK as one Dex Flow.
package sitetdk

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/chinaz/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "ChinazSiteTDK"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "chinaz"
	operationStepType = "CallGetSiteTDK"
)

var siteTDKOutcomeAttribute = dex.DefineAttribute[chinaz.SiteTDK]("chinaz-site-tdk-outcome")

// Flow runs GetSiteTDK once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection chinaz.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection chinaz.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{siteTDKOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(chinaz.NewGetSiteTDKStep(chinaz.GetSiteTDKStepConfig[chinaz.GetSiteTDKInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Chinaz", Explanation: "Read public Chinaz site TDK metadata."},
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

// dex:field attribute-key:chinaz-site-tdk-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readSiteTDKOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"chinaz-site-tdk-outcome": outcome}}, nil
}

// dex:field attribute-key:chinaz-site-tdk-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readSiteTDKOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"chinaz-site-tdk-outcome": outcome}}, nil
}

func readSiteTDKOutcome(ctx dex.Context) (chinaz.SiteTDK, error) {
	outcome, err := siteTDKOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return chinaz.SiteTDK{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input chinaz.GetSiteTDKInput) chinaz.GetSiteTDKInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetSiteTDK" }
func (startRequest) WaitFor(dex.Context, chinaz.GetSiteTDKInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input chinaz.GetSiteTDKInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[chinaz.GetSiteTDKInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[chinaz.GetSiteTDKResult]
}

func (completeResult) GetStepType() string { return "CompleteGetSiteTDK" }
func (completeResult) Execute(ctx dex.Context, result chinaz.GetSiteTDKResult) (*dex.StepDecision, error) {
	if err := siteTDKOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
