// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package flow demonstrates Supabase's selectRows operation in a Dex Flow.
package flow

import (
	"github.com/superdurable/dex-connectors-library/connectors/supabase"
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

// Flow calls Supabase once and completes with the provider result.
type Flow struct {
	dex.FlowDefaults
	connection supabase.Connection
}

// NewFlow binds the connector connection at Worker registration.
func NewFlow(connection supabase.Connection) *Flow { return &Flow{connection: connection} }

// GetPersistenceSchema declares no durable Attributes or Streams for this sample.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema { return dex.PersistenceSchema{} }

// GetFlowType returns the sample Flow identity.
func (*Flow) GetFlowType() string { return FlowType }

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

// dex:group group-id:supabase-sample group-label:"Supabase sample"
// dex:explanation text:"Pass the start input to the connector operation."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartSupabaseRequest" }
func (startRequest) WaitFor(dex.Context, supabase.SelectRowsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, request supabase.SelectRowsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[supabase.SelectRowsInput](callStepType), request), nil
}

// dex:group group-id:supabase-sample group-label:"Supabase sample"
// dex:explanation text:"Complete the Flow with the provider response."
type completeRequest struct {
	dex.StepDefaultsNoWaitFor[supabase.SelectRowsResult]
}

func (completeRequest) GetStepType() string { return "CompleteSupabaseRequest" }
func (completeRequest) Execute(_ dex.Context, result supabase.SelectRowsResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
