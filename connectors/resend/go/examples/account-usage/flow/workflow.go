// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package accountusage demonstrates Resend account usage retrieval.
package accountusage

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/resend/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is this example's stable Flow identity.
	FlowType = "ResendGetUsage"
	// ConnectionName is the static Dex Web connection name.
	ConnectionName = "resend"
)

// Request supplies the metric filters.
type Request resend.GetUsageInput

var outcomeAttribute = dex.DefineAttribute[resend.AccountUsage]("resend-account-usage")

// Flow retrieves aggregate Resend account usage.
type Flow struct {
	dex.FlowDefaults
	connection resend.Connection
}

// NewFlow binds a Resend connection.
func NewFlow(connection resend.Connection) *Flow { return &Flow{connection: connection} }

// GetFlowType returns the stable Flow identity.
func (*Flow) GetFlowType() string { return FlowType }

// GetRPCs exposes Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetPersistenceSchema registers the metrics result.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{outcomeAttribute}}
}

// GetDexSummary returns the account usage.
// dex:field attribute-key:resend-account-usage value-type:json editable:false description:"Account usage"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readAccountUsageOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"resend-account-usage": value}}, nil
}

// GetDexDisplay returns the account usage.
// dex:field attribute-key:resend-account-usage value-type:json editable:false description:"Account usage"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readAccountUsageOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"resend-account-usage": value}}, nil
}

func readAccountUsageOutcome(ctx dex.Context) (resend.AccountUsage, error) {
	value, err := outcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return resend.AccountUsage{}, nil
	}
	return value, err
}

// GetSteps registers the account usage query path.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(prepareAccountUsage{}),
		dex.DefineStep(resend.NewGetUsageStep(resend.GetUsageStepConfig[Request]{
			StepType: "GetUsage", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "resend-account-usage", GroupLabel: "Resend account usage", Explanation: "Retrieve delivery metrics for the selected filters."},
			MapToOperationInput: func(request Request) resend.GetUsageInput { return resend.GetUsageInput(request) },
			Found:               sdkgo.GoTo(completeAccountUsage{}),
		})),
		dex.DefineStep(completeAccountUsage{}),
	}
}

// dex:group group-id:resend-account-usage-prepare group-label:"Prepare account usage"
// dex:explanation text:"Pass the selected metric filters into the provider query."
type prepareAccountUsage struct{ dex.StepDefaults }

func (prepareAccountUsage) GetStepType() string { return "PrepareAccountUsage" }
func (prepareAccountUsage) WaitFor(dex.Context, Request) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (prepareAccountUsage) Execute(_ dex.Context, request Request) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[Request]("GetUsage"), request), nil
}

// dex:group group-id:resend-account-usage-complete group-label:"Complete account usage query"
// dex:explanation text:"Complete after Resend returns the aggregate metrics."
type completeAccountUsage struct {
	dex.StepDefaultsNoWaitFor[resend.GetUsageResult]
}

func (completeAccountUsage) GetStepType() string { return "CompleteAccountUsage" }
func (completeAccountUsage) Execute(ctx dex.Context, result resend.GetUsageResult) (*dex.StepDecision, error) {
	if err := outcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
