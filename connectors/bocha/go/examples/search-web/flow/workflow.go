// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package websearch runs Bocha searchWeb as one Dex Flow.
package websearch

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/bocha/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "BochaWebSearch"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "bocha"
	operationStepType = "CallSearchWeb"
)

var webSearchOutcomeAttribute = dex.DefineAttribute[bocha.SearchWebOutput]("bocha-search-web-outcome")

// Flow runs SearchWeb once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection bocha.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection bocha.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{webSearchOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(bocha.NewSearchWebStep(bocha.SearchWebStepConfig[bocha.SearchWebInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Bocha", Explanation: "Search public web pages with the supplied query."},
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

// dex:field attribute-key:bocha-search-web-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readSearchWebOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"bocha-search-web-outcome": outcome}}, nil
}

// dex:field attribute-key:bocha-search-web-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readSearchWebOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"bocha-search-web-outcome": outcome}}, nil
}

func readSearchWebOutcome(ctx dex.Context) (bocha.SearchWebOutput, error) {
	outcome, err := webSearchOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return bocha.SearchWebOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input bocha.SearchWebInput) bocha.SearchWebInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartSearchWeb" }
func (startRequest) WaitFor(dex.Context, bocha.SearchWebInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input bocha.SearchWebInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[bocha.SearchWebInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[bocha.SearchWebResult]
}

func (completeResult) GetStepType() string { return "CompleteSearchWeb" }
func (completeResult) Execute(ctx dex.Context, result bocha.SearchWebResult) (*dex.StepDecision, error) {
	if err := webSearchOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
