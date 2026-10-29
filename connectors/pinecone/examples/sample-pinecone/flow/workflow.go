// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package flow demonstrates Pinecone's upsertVectors operation in a Dex Flow.
package flow

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/pinecone"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Dex Flow type.
	FlowType = "PineconeSample"
	// ConnectionName is the static Dex Web connection name.
	ConnectionName = "pinecone"
	callStepType   = "CallPinecone"
)

var sampleResultAttribute = dex.DefineAttribute[pinecone.UpsertVectorsOutput]("pinecone-sample-result")

// Flow calls Pinecone once and completes with the provider result.
type Flow struct {
	dex.FlowDefaults
	connection pinecone.Connection
}

// NewFlow binds the connector connection at Worker registration.
func NewFlow(connection pinecone.Connection) *Flow { return &Flow{connection: connection} }

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
// dex:field attribute-key:pinecone-sample-result value-type:json editable:false description:"Provider result"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := inspectResult(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"pinecone-sample-result": result}}, nil
}

// GetDexDisplay returns the completed provider result when available.
// dex:field attribute-key:pinecone-sample-result value-type:json editable:false description:"Provider result"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := inspectResult(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"pinecone-sample-result": result}}, nil
}

func inspectResult(ctx dex.Context) (pinecone.UpsertVectorsOutput, error) {
	result, err := sampleResultAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if err != nil && !errors.As(err, &missing) {
		return pinecone.UpsertVectorsOutput{}, err
	}
	return result, nil
}

// GetSteps registers the start, provider, and completion Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(pinecone.NewUpsertVectorsStep(pinecone.UpsertVectorsStepConfig[pinecone.UpsertVectorsInput]{
			StepType: callStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "pinecone", GroupLabel: "Pinecone", Explanation: "Upsert vector IDs by absolute replacement in one namespace."},
			MapToOperationInput: MapToOperationInput, Upserted: sdkgo.GoTo(completeRequest{}),
		})),
		dex.DefineStep(completeRequest{}),
	}
}

// MapToOperationInput passes the validated Flow request to the connector.
func MapToOperationInput(request pinecone.UpsertVectorsInput) pinecone.UpsertVectorsInput {
	return request
}

// dex:group group-id:pinecone-start group-label:"Pinecone sample"
// dex:explanation text:"Pass the start input to the connector operation."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartPineconeRequest" }
func (startRequest) WaitFor(dex.Context, pinecone.UpsertVectorsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, request pinecone.UpsertVectorsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[pinecone.UpsertVectorsInput](callStepType), request), nil
}

// dex:group group-id:pinecone-complete group-label:"Pinecone sample"
// dex:explanation text:"Complete the Flow with the provider response."
type completeRequest struct {
	dex.StepDefaultsNoWaitFor[pinecone.UpsertVectorsResult]
}

func (completeRequest) GetStepType() string { return "CompletePineconeRequest" }
func (completeRequest) Execute(ctx dex.Context, result pinecone.UpsertVectorsResult) (*dex.StepDecision, error) {
	if err := sampleResultAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
