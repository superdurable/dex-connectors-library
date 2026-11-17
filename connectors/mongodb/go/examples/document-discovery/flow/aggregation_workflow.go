// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package documentdiscovery

import (
	"github.com/superdurable/dex-connectors-library/connectors/mongodb/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// AggregationFlow runs a read-only MongoDB pipeline against one collection.
type AggregationFlow struct {
	dex.FlowDefaults
	connection mongodb.Connection
}

// NewAggregationFlow binds the static MongoDB connection.
func NewAggregationFlow(connection mongodb.Connection) *AggregationFlow {
	return &AggregationFlow{connection: connection}
}

func (*AggregationFlow) GetFlowType() string                         { return "MongoDBDocumentAggregation" }
func (*AggregationFlow) GetPersistenceSchema() dex.PersistenceSchema { return dex.PersistenceSchema{} }
func (flow *AggregationFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(aggregationStart{}),
		dex.DefineStep(mongodb.NewAggregateDocumentsStep(mongodb.AggregateDocumentsStepConfig[mongodb.AggregateDocumentsInput]{
			StepType: "CallAggregateDocuments", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "aggregation", GroupLabel: "Aggregation", Explanation: "Run a read-only pipeline over one MongoDB collection."},
			MapToOperationInput: func(input mongodb.AggregateDocumentsInput) mongodb.AggregateDocumentsInput { return input },
			Aggregated:          sdkgo.GoTo(aggregationComplete{}),
		})),
		dex.DefineStep(aggregationComplete{}),
	}
}

// GetRPCs exposes Dex Web summary and display views.
func (flow *AggregationFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns an empty view while aggregation executes.
func (*AggregationFlow) GetDexSummary(_ dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{}}, nil
}

// GetDexDisplay returns an empty view while aggregation executes.
func (*AggregationFlow) GetDexDisplay(_ dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{}}, nil
}

// dex:group group-id:aggregation group-label:"Aggregation"
// dex:explanation text:"Pass a read-only pipeline to the MongoDB aggregation Step."
type aggregationStart struct{ dex.StepDefaults }

func (aggregationStart) GetStepType() string { return "StartMongoDBAggregation" }
func (aggregationStart) WaitFor(dex.Context, mongodb.AggregateDocumentsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (aggregationStart) Execute(_ dex.Context, input mongodb.AggregateDocumentsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[mongodb.AggregateDocumentsInput]("CallAggregateDocuments"), input), nil
}

// dex:group group-id:aggregation group-label:"Aggregation"
// dex:explanation text:"Complete with bounded aggregation output."
type aggregationComplete struct {
	dex.StepDefaultsNoWaitFor[mongodb.AggregateDocumentsResult]
}

func (aggregationComplete) GetStepType() string { return "CompleteMongoDBAggregation" }
func (aggregationComplete) Execute(_ dex.Context, result mongodb.AggregateDocumentsResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*AggregationFlow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*AggregationFlow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*AggregationFlow)(nil).GetDexDisplay
