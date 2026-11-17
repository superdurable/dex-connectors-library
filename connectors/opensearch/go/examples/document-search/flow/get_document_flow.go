// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package documentsearch

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/opensearch/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var retrievedDocumentAttribute = dex.DefineAttribute[opensearch.RetrievedDocument]("opensearch-retrieved-document")

// DocumentGetFlow retrieves one document by its stable index and ID.
type DocumentGetFlow struct {
	dex.FlowDefaults
	connection opensearch.Connection
}

// NewDocumentGetFlow binds the static OpenSearch connection.
func NewDocumentGetFlow(connection opensearch.Connection) *DocumentGetFlow {
	return &DocumentGetFlow{connection: connection}
}

// GetFlowType preserves the example's document retrieval identity.
func (*DocumentGetFlow) GetFlowType() string { return "OpenSearchDocumentGet" }

// GetPersistenceSchema registers the retrieved document for Dex Web views.
func (*DocumentGetFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{retrievedDocumentAttribute}}
}

// GetSteps returns the start, provider, and completion Steps.
func (flow *DocumentGetFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startDocumentGet{}),
		dex.DefineStep(opensearch.NewGetDocumentStep(opensearch.GetDocumentStepConfig[opensearch.GetDocumentInput]{
			StepType: "CallGetDocument", ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "OpenSearch", Explanation: "Retrieve one document by stable ID."},
			Connection:          flow.connection,
			MapToOperationInput: func(input opensearch.GetDocumentInput) opensearch.GetDocumentInput { return input },
			Found:               sdkgo.GoTo(completeDocumentGet{}),
		})),
		dex.DefineStep(completeDocumentGet{}),
	}
}

// GetRPCs exposes summary and display views in Dex Web.
func (flow *DocumentGetFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the retained document metadata and source.
// dex:field attribute-key:opensearch-retrieved-document value-type:json editable:false description:"Retrieved document"
func (*DocumentGetFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	document, err := readRetrievedDocument(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"opensearch-retrieved-document": document}}, nil
}

// GetDexDisplay returns the retained document metadata and source.
// dex:field attribute-key:opensearch-retrieved-document value-type:json editable:false description:"Retrieved document"
func (*DocumentGetFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	document, err := readRetrievedDocument(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"opensearch-retrieved-document": document}}, nil
}

func readRetrievedDocument(ctx dex.Context) (opensearch.RetrievedDocument, error) {
	document, err := retrievedDocumentAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return opensearch.RetrievedDocument{}, nil
	}
	return document, err
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the index and document ID to the provider Step."
type startDocumentGet struct{ dex.StepDefaults }

func (startDocumentGet) GetStepType() string { return "StartGetDocument" }
func (startDocumentGet) WaitFor(dex.Context, opensearch.GetDocumentInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startDocumentGet) Execute(_ dex.Context, input opensearch.GetDocumentInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[opensearch.GetDocumentInput]("CallGetDocument"), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Retain the retrieved document and complete the Flow."
type completeDocumentGet struct {
	dex.StepDefaultsNoWaitFor[opensearch.GetDocumentResult]
}

func (completeDocumentGet) GetStepType() string { return "CompleteGetDocument" }
func (completeDocumentGet) Execute(ctx dex.Context, result opensearch.GetDocumentResult) (*dex.StepDecision, error) {
	if err := retrievedDocumentAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*DocumentGetFlow)(nil)
