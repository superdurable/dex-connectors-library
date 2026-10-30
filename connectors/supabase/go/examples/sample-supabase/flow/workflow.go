// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package flow demonstrates Supabase's selectRows operation in a Dex Flow.
package flow

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/supabase/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Dex Flow type.
	FlowType = "SupabaseSample"
	// ConnectionName is the static Dex Web connection name.
	ConnectionName = "supabase"
	callStepType   = "CallSupabase"
)

var sampleResultAttribute = dex.DefineAttribute[supabase.SelectRowsOutput]("supabase-sample-result")

// Flow calls Supabase once and completes with the provider result.
type Flow struct {
	dex.FlowDefaults
	connection supabase.Connection
}

// NewFlow binds the connector connection at Worker registration.
func NewFlow(connection supabase.Connection) *Flow { return &Flow{connection: connection} }

// GetPersistenceSchema registers the completed provider result.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{sampleResultAttribute}}
}

// GetFlowType returns the sample Flow identity.
func (*Flow) GetFlowType() string { return FlowType }

// GetRPCs exposes the result to Dex Web.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the completed provider result when available.
// dex:field attribute-key:supabase-sample-result value-type:json editable:false description:"Provider result"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := inspectResult(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"supabase-sample-result": result}}, nil
}

// GetDexDisplay returns the completed provider result when available.
// dex:field attribute-key:supabase-sample-result value-type:json editable:false description:"Provider result"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := inspectResult(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"supabase-sample-result": result}}, nil
}

func inspectResult(ctx dex.Context) (supabase.SelectRowsOutput, error) {
	result, err := sampleResultAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if err != nil && !errors.As(err, &missing) {
		return supabase.SelectRowsOutput{}, err
	}
	return result, nil
}

// GetSteps registers the start, provider, and completion Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(supabase.NewSelectRowsStep(supabase.SelectRowsStepConfig[supabase.SelectRowsInput]{
			StepType: callStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "supabase", GroupLabel: "Supabase", Explanation: "Select a bounded page of table rows through PostgREST."},
			MapToOperationInput: MapToOperationInput, Selected: sdkgo.GoTo(completeRequest{}),
		})),
		dex.DefineStep(completeRequest{}),
	}
}

// MapToOperationInput passes the validated Flow request to the connector.
func MapToOperationInput(request supabase.SelectRowsInput) supabase.SelectRowsInput { return request }

// dex:group group-id:supabase-start group-label:"Supabase sample"
// dex:explanation text:"Pass the start input to the connector operation."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartSupabaseRequest" }
func (startRequest) WaitFor(dex.Context, supabase.SelectRowsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, request supabase.SelectRowsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[supabase.SelectRowsInput](callStepType), request), nil
}

// dex:group group-id:supabase-complete group-label:"Supabase sample"
// dex:explanation text:"Complete the Flow with the provider response."
type completeRequest struct {
	dex.StepDefaultsNoWaitFor[supabase.SelectRowsResult]
}

func (completeRequest) GetStepType() string { return "CompleteSupabaseRequest" }
func (completeRequest) Execute(ctx dex.Context, result supabase.SelectRowsResult) (*dex.StepDecision, error) {
	if err := sampleResultAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
