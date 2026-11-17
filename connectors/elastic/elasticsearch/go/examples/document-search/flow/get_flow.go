// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package documentsearch

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/elastic/elasticsearch/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// GetFlowType identifies the document read example.
const GetFlowType = "ElasticsearchDocumentGet"

// GetFlow reads one document by ID through the static Elasticsearch connection.
type GetFlow struct {
	dex.FlowDefaults
	connection elasticsearch.Connection
}

var retrievedDocumentAttribute = dex.DefineAttribute[elasticsearch.RetrievedDocument]("elasticsearch-retrieved-document")

// NewGetFlow binds the connection when registering the example Worker.
func NewGetFlow(connection elasticsearch.Connection) *GetFlow {
	return &GetFlow{connection: connection}
}

// GetFlowType returns the stable Flow identity.
func (*GetFlow) GetFlowType() string { return GetFlowType }

// GetPersistenceSchema registers the persisted document for Dex Web views.
func (*GetFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{retrievedDocumentAttribute}}
}

// GetRPCs exposes read-only Dex Web views.
func (flow *GetFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:elasticsearch-retrieved-document value-type:json editable:false description:"Retrieved document"
// GetDexSummary shows the document in Dex Web.
func (*GetFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	document, err := readRetrievedDocument(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"elasticsearch-retrieved-document": document}}, nil
}

// dex:field attribute-key:elasticsearch-retrieved-document value-type:json editable:false description:"Retrieved document"
// GetDexDisplay shows the document in Dex Web.
func (*GetFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	document, err := readRetrievedDocument(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"elasticsearch-retrieved-document": document}}, nil
}

func readRetrievedDocument(ctx dex.Context) (elasticsearch.RetrievedDocument, error) {
	document, err := retrievedDocumentAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return elasticsearch.RetrievedDocument{}, nil
	}
	return document, err
}

// GetSteps declares the provider call and completion path.
func (flow *GetFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startDocumentGet{}),
		dex.DefineStep(elasticsearch.NewGetDocumentStep(elasticsearch.GetDocumentStepConfig[elasticsearch.GetDocumentInput]{
			StepType: "CallGetDocument", ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Elasticsearch", Explanation: "Read one document by its stable ID."},
			Connection:  flow.connection, MapToOperationInput: mapToGetDocumentInput,
			Found: sdkgo.GoTo(completeDocumentGet{}),
		})),
		dex.DefineStep(completeDocumentGet{}),
	}
}

func mapToGetDocumentInput(input elasticsearch.GetDocumentInput) elasticsearch.GetDocumentInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the document identity to Elasticsearch."
type startDocumentGet struct{ dex.StepDefaults }

func (startDocumentGet) GetStepType() string { return "StartGetDocument" }
func (startDocumentGet) WaitFor(dex.Context, elasticsearch.GetDocumentInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startDocumentGet) Execute(_ dex.Context, input elasticsearch.GetDocumentInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[elasticsearch.GetDocumentInput]("CallGetDocument"), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the retrieved document."
type completeDocumentGet struct {
	dex.StepDefaultsNoWaitFor[elasticsearch.GetDocumentResult]
}

func (completeDocumentGet) GetStepType() string { return "CompleteGetDocument" }
func (completeDocumentGet) Execute(ctx dex.Context, result elasticsearch.GetDocumentResult) (*dex.StepDecision, error) {
	if err := retrievedDocumentAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*GetFlow)(nil)
