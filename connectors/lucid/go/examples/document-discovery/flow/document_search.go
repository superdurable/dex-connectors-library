// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package documentdiscovery

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/lucid/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// DocumentSearchFlow finds Lucid documents by keyword.
type DocumentSearchFlow struct {
	dex.FlowDefaults
	connection lucid.Connection
}

// NewDocumentSearchFlow binds the provider connection at registration time.
func NewDocumentSearchFlow(connection lucid.Connection) *DocumentSearchFlow {
	return &DocumentSearchFlow{connection: connection}
}

func (*DocumentSearchFlow) GetFlowType() string { return "LucidDocumentSearch" }

var documentSearchAttribute = dex.DefineAttribute[lucid.DocumentsPage]("lucid-document-search")

func (*DocumentSearchFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{documentSearchAttribute}}
}

func (flow *DocumentSearchFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startDocumentSearch{}),
		dex.DefineStep(lucid.NewSearchDocumentsStep(lucid.SearchDocumentsStepConfig[lucid.SearchDocumentsInput]{
			StepType: "CallSearchDocuments", ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Lucid", Explanation: "Search non-trashed Lucid documents by keyword."},
			Connection:  flow.connection, MapToOperationInput: mapDocumentSearchInput,
			Listed: sdkgo.GoTo(completeDocumentSearch{}),
		})),
		dex.DefineStep(completeDocumentSearch{}),
	}
}

func (flow *DocumentSearchFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:lucid-document-search value-type:json editable:false description:"Document search results"
// GetDexSummary shows search results in Dex Web.
func (*DocumentSearchFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readDocumentSearch(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"lucid-document-search": value}}, nil
}

// dex:field attribute-key:lucid-document-search value-type:json editable:false description:"Document search results"
// GetDexDisplay shows search results in Dex Web.
func (*DocumentSearchFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readDocumentSearch(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"lucid-document-search": value}}, nil
}

func readDocumentSearch(ctx dex.Context) (lucid.DocumentsPage, error) {
	value, err := documentSearchAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return lucid.DocumentsPage{}, nil
	}
	return value, err
}

func mapDocumentSearchInput(input lucid.SearchDocumentsInput) lucid.SearchDocumentsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the Lucid search keywords and optional page token to the provider Step."
type startDocumentSearch struct{ dex.StepDefaults }

func (startDocumentSearch) GetStepType() string { return "StartSearchDocuments" }
func (startDocumentSearch) WaitFor(dex.Context, lucid.SearchDocumentsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startDocumentSearch) Execute(_ dex.Context, input lucid.SearchDocumentsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[lucid.SearchDocumentsInput]("CallSearchDocuments"), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Store the search results and complete the Flow."
type completeDocumentSearch struct {
	dex.StepDefaultsNoWaitFor[lucid.SearchDocumentsResult]
}

func (completeDocumentSearch) GetStepType() string { return "CompleteSearchDocuments" }
func (completeDocumentSearch) Execute(ctx dex.Context, result lucid.SearchDocumentsResult) (*dex.StepDecision, error) {
	if err := documentSearchAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*DocumentSearchFlow)(nil)
