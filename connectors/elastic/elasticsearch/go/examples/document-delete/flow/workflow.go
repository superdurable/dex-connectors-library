// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package documentdelete runs an Elasticsearch document mutation as a Dex Flow.
package documentdelete

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/elastic/elasticsearch/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "ElasticsearchDeleteDocument"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "elasticsearch"
	operationStepType = "CallDeleteDocument"
)

var deletedDocumentAttribute = dex.DefineAttribute[elasticsearch.DeletedDocument]("elasticsearch-document-delete")

// Flow completes with the deleted document identity.
type Flow struct {
	dex.FlowDefaults
	connection elasticsearch.Connection
}

// NewFlow binds the Elasticsearch connection at registration time.
func NewFlow(connection elasticsearch.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{deletedDocumentAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(elasticsearch.NewDeleteDocumentStep(elasticsearch.DeleteDocumentStepConfig[elasticsearch.DeleteDocumentInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Elasticsearch", Explanation: "Operate on one document by stable ID."},
			Connection:          flow.connection,
			MapToOperationInput: mapToOperationInput,
			Absent:              sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:elasticsearch-document-delete value-type:json editable:false description:"DeletedDocument"
// GetDexSummary shows the deleted document in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readDeletedDocument(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"elasticsearch-document-delete": page}}, nil
}

// dex:field attribute-key:elasticsearch-document-delete value-type:json editable:false description:"DeletedDocument"
// GetDexDisplay shows the deleted document in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readDeletedDocument(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"elasticsearch-document-delete": page}}, nil
}

func readDeletedDocument(ctx dex.Context) (elasticsearch.DeletedDocument, error) {
	page, err := deletedDocumentAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return elasticsearch.DeletedDocument{}, nil
	}
	return page, err
}

func mapToOperationInput(input elasticsearch.DeleteDocumentInput) elasticsearch.DeleteDocumentInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the deleted document input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartDeleteDocument" }
func (startRequest) WaitFor(dex.Context, elasticsearch.DeleteDocumentInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input elasticsearch.DeleteDocumentInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[elasticsearch.DeleteDocumentInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the deleteddocument."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[elasticsearch.DeleteDocumentResult]
}

func (completeResult) GetStepType() string { return "CompleteDeleteDocument" }
func (completeResult) Execute(ctx dex.Context, result elasticsearch.DeleteDocumentResult) (*dex.StepDecision, error) {
	if err := deletedDocumentAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
