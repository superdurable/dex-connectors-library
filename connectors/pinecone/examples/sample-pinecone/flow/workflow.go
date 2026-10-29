// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package flow demonstrates Pinecone's upsertVectors operation in a Dex Flow.
package flow

import (
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

// Flow calls Pinecone once and completes with the provider result.
type Flow struct {
	dex.FlowDefaults
	connection pinecone.Connection
}

// NewFlow binds the connector connection at Worker registration.
func NewFlow(connection pinecone.Connection) *Flow { return &Flow{connection: connection} }

// GetPersistenceSchema declares no durable Attributes or Streams for this sample.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema { return dex.PersistenceSchema{} }

// GetFlowType returns the sample Flow identity.
func (*Flow) GetFlowType() string { return FlowType }

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

// dex:group group-id:pinecone-sample group-label:"Pinecone sample"
// dex:explanation text:"Pass the start input to the connector operation."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartPineconeRequest" }
func (startRequest) WaitFor(dex.Context, pinecone.UpsertVectorsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, request pinecone.UpsertVectorsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[pinecone.UpsertVectorsInput](callStepType), request), nil
}

// dex:group group-id:pinecone-sample group-label:"Pinecone sample"
// dex:explanation text:"Complete the Flow with the provider response."
type completeRequest struct {
	dex.StepDefaultsNoWaitFor[pinecone.UpsertVectorsResult]
}

func (completeRequest) GetStepType() string { return "CompletePineconeRequest" }
func (completeRequest) Execute(_ dex.Context, result pinecone.UpsertVectorsResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
