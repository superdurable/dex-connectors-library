// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package documentindex runs an OpenSearch document mutation as a Dex Flow.
package documentindex

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/opensearch/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "OpenSearchIndexDocument"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "opensearch"
	operationStepType = "CallIndexDocument"
)

var indexedDocumentAttribute = dex.DefineAttribute[opensearch.IndexedDocument]("opensearch-document-index")

// Flow completes with the indexed document identity.
type Flow struct {
	dex.FlowDefaults
	connection opensearch.Connection
}

// NewFlow binds the OpenSearch connection at registration time.
func NewFlow(connection opensearch.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{indexedDocumentAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(opensearch.NewIndexDocumentStep(opensearch.IndexDocumentStepConfig[opensearch.IndexDocumentInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "OpenSearch", Explanation: "Operate on one document by stable ID."},
			Connection:          flow.connection,
			MapToOperationInput: mapToOperationInput,
			Indexed:             sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:opensearch-document-index value-type:json editable:false description:"IndexedDocument"
// GetDexSummary shows the indexed document in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readIndexedDocument(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"opensearch-document-index": page}}, nil
}

// dex:field attribute-key:opensearch-document-index value-type:json editable:false description:"IndexedDocument"
// GetDexDisplay shows the indexed document in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readIndexedDocument(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"opensearch-document-index": page}}, nil
}

func readIndexedDocument(ctx dex.Context) (opensearch.IndexedDocument, error) {
	page, err := indexedDocumentAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return opensearch.IndexedDocument{}, nil
	}
	return page, err
}

func mapToOperationInput(input opensearch.IndexDocumentInput) opensearch.IndexDocumentInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the indexed document input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartIndexDocument" }
func (startRequest) WaitFor(dex.Context, opensearch.IndexDocumentInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input opensearch.IndexDocumentInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[opensearch.IndexDocumentInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the indexeddocument."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[opensearch.IndexDocumentResult]
}

func (completeResult) GetStepType() string { return "CompleteIndexDocument" }
func (completeResult) Execute(ctx dex.Context, result opensearch.IndexDocumentResult) (*dex.StepDecision, error) {
	if err := indexedDocumentAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
