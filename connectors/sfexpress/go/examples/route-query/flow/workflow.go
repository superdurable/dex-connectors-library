// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package routequery runs SFExpress queryRoutes as one Dex Flow.
package routequery

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/sfexpress/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "SFExpressRouteQuery"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "sfexpress"
	operationStepType = "CallQueryRoutes"
)

var shipmentListOutcomeAttribute = dex.DefineAttribute[sfexpress.RouteQueryResult]("sfexpress-route-query-outcome")

// Flow runs QueryRoutes once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection sfexpress.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection sfexpress.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{shipmentListOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(sfexpress.NewQueryRoutesStep(sfexpress.QueryRoutesStepConfig[sfexpress.QueryRoutesInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "SFExpress", Explanation: "Query an owned SF Express waybill."},
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

// dex:field attribute-key:sfexpress-route-query-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readRouteQueryResultOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"sfexpress-route-query-outcome": outcome}}, nil
}

// dex:field attribute-key:sfexpress-route-query-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readRouteQueryResultOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"sfexpress-route-query-outcome": outcome}}, nil
}

func readRouteQueryResultOutcome(ctx dex.Context) (sfexpress.RouteQueryResult, error) {
	outcome, err := shipmentListOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return sfexpress.RouteQueryResult{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input sfexpress.QueryRoutesInput) sfexpress.QueryRoutesInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartQueryRoutes" }
func (startRequest) WaitFor(dex.Context, sfexpress.QueryRoutesInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input sfexpress.QueryRoutesInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[sfexpress.QueryRoutesInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[sfexpress.QueryRoutesResult]
}

func (completeResult) GetStepType() string { return "CompleteQueryRoutes" }
func (completeResult) Execute(ctx dex.Context, result sfexpress.QueryRoutesResult) (*dex.StepDecision, error) {
	if err := shipmentListOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
