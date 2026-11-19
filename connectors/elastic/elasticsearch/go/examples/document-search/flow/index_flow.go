// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package documentsearch

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/elastic/elasticsearch/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// IndexFlowType identifies the index metadata example.
const IndexFlowType = "ElasticsearchIndexGet"

// IndexFlow reads one index's metadata through the static connection.
type IndexFlow struct {
	dex.FlowDefaults
	connection elasticsearch.Connection
}

var indexDetailsAttribute = dex.DefineAttribute[elasticsearch.IndexDetails]("elasticsearch-index-details")

// NewIndexFlow binds the connector to the example Worker.
func NewIndexFlow(connection elasticsearch.Connection) *IndexFlow {
	return &IndexFlow{connection: connection}
}

// GetFlowType preserves the index example's Flow identity.
func (*IndexFlow) GetFlowType() string { return IndexFlowType }

// GetPersistenceSchema registers index metadata for Dex Web views.
func (*IndexFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{indexDetailsAttribute}}
}

// GetRPCs exposes index metadata in Dex Web.
func (flow *IndexFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:elasticsearch-index-details value-type:json editable:false description:"Index metadata"
// GetDexSummary shows index metadata in Dex Web.
func (*IndexFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	details, err := readIndexDetails(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"elasticsearch-index-details": details}}, nil
}

// dex:field attribute-key:elasticsearch-index-details value-type:json editable:false description:"Index metadata"
// GetDexDisplay shows index metadata in Dex Web.
func (*IndexFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	details, err := readIndexDetails(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"elasticsearch-index-details": details}}, nil
}

func readIndexDetails(ctx dex.Context) (elasticsearch.IndexDetails, error) {
	details, err := indexDetailsAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return elasticsearch.IndexDetails{}, nil
	}
	return details, err
}

// GetSteps calls Elasticsearch and stores the index metadata.
func (flow *IndexFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startIndexGet{}),
		dex.DefineStep(elasticsearch.NewGetIndexStep(elasticsearch.GetIndexStepConfig[elasticsearch.IndexNameInput]{
			StepType: "CallGetIndex", ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Elasticsearch", Explanation: "Read metadata for one index."},
			Connection:  flow.connection, MapToOperationInput: mapToGetIndexInput,
			Found: sdkgo.GoTo(completeIndexGet{}),
		})),
		dex.DefineStep(completeIndexGet{}),
	}
}

func mapToGetIndexInput(input elasticsearch.IndexNameInput) elasticsearch.IndexNameInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the index name to Elasticsearch."
type startIndexGet struct{ dex.StepDefaults }

func (startIndexGet) GetStepType() string { return "StartGetIndex" }
func (startIndexGet) WaitFor(dex.Context, elasticsearch.IndexNameInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startIndexGet) Execute(_ dex.Context, input elasticsearch.IndexNameInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[elasticsearch.IndexNameInput]("CallGetIndex"), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the index metadata."
type completeIndexGet struct {
	dex.StepDefaultsNoWaitFor[elasticsearch.GetIndexResult]
}

func (completeIndexGet) GetStepType() string { return "CompleteGetIndex" }
func (completeIndexGet) Execute(ctx dex.Context, result elasticsearch.GetIndexResult) (*dex.StepDecision, error) {
	if err := indexDetailsAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*IndexFlow)(nil)
