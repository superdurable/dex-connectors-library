// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package flow

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/pinecone/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// VectorFetchFlowType is the stable identity of the vector fetch example.
const VectorFetchFlowType = "PineconeVectorFetch"

const fetchStepType = "FetchPineconeVectors"

var fetchResultAttribute = dex.DefineAttribute[pinecone.FetchedVectors]("pinecone-fetched-vectors")

// VectorFetchFlow reads selected vector IDs from one namespace.
type VectorFetchFlow struct {
	dex.FlowDefaults
	connection pinecone.Connection
}

// NewVectorFetchFlow binds the trusted connection at registration.
func NewVectorFetchFlow(connection pinecone.Connection) *VectorFetchFlow {
	return &VectorFetchFlow{connection: connection}
}

// GetFlowType returns the stable Flow identity.
func (*VectorFetchFlow) GetFlowType() string { return VectorFetchFlowType }

// GetPersistenceSchema registers the fetched vector attribute.
func (*VectorFetchFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{fetchResultAttribute}}
}

// GetSteps returns the start, provider read, and completion Steps.
func (flow *VectorFetchFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startVectorFetch{}),
		dex.DefineStep(pinecone.NewFetchVectorsStep(pinecone.FetchVectorsStepConfig[pinecone.FetchVectorsInput]{
			StepType: fetchStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "pinecone-fetch", GroupLabel: "Pinecone fetch", Explanation: "Fetch selected dense vector values."},
			MapToOperationInput: func(input pinecone.FetchVectorsInput) pinecone.FetchVectorsInput { return input },
			Fetched:             sdkgo.GoTo(completeVectorFetch{}),
		})),
		dex.DefineStep(completeVectorFetch{}),
	}
}

// GetRPCs exposes the fetched values in Dex Web.
func (flow *VectorFetchFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:pinecone-fetched-vectors value-type:json editable:false description:"Fetched vector values"
// GetDexSummary reads the persisted fetch result.
func (*VectorFetchFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := readFetchResult(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"pinecone-fetched-vectors": result}}, nil
}

// dex:field attribute-key:pinecone-fetched-vectors value-type:json editable:false description:"Fetched vector values"
// GetDexDisplay reads the persisted fetch result.
func (*VectorFetchFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := readFetchResult(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"pinecone-fetched-vectors": result}}, nil
}

func readFetchResult(ctx dex.Context) (pinecone.FetchedVectors, error) {
	result, err := fetchResultAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return pinecone.FetchedVectors{}, nil
	}
	return result, err
}

// dex:group group-id:pinecone-fetch-start group-label:"Vector fetch"
// dex:explanation text:"Pass selected vector IDs to the Pinecone fetch Step."
type startVectorFetch struct{ dex.StepDefaults }

func (startVectorFetch) GetStepType() string { return "StartVectorFetch" }
func (startVectorFetch) WaitFor(dex.Context, pinecone.FetchVectorsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startVectorFetch) Execute(_ dex.Context, input pinecone.FetchVectorsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[pinecone.FetchVectorsInput](fetchStepType), input), nil
}

// dex:group group-id:pinecone-fetch-complete group-label:"Fetched vectors"
// dex:explanation text:"Persist fetched dense vector values and complete the Flow."
type completeVectorFetch struct {
	dex.StepDefaultsNoWaitFor[pinecone.FetchVectorsResult]
}

func (completeVectorFetch) GetStepType() string { return "CompleteVectorFetch" }
func (completeVectorFetch) Execute(ctx dex.Context, result pinecone.FetchVectorsResult) (*dex.StepDecision, error) {
	if err := fetchResultAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*VectorFetchFlow)(nil)
