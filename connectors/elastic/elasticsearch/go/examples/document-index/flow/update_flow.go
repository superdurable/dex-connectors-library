// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package documentindex

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/elastic/elasticsearch/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// UpdateFlowType is the stable example Flow identity.
	UpdateFlowType          = "ElasticsearchUpdateDocument"
	updateOperationStepType = "CallUpdateDocument"
)

var updatedDocumentAttribute = dex.DefineAttribute[elasticsearch.IndexedDocument]("elasticsearch-document-update")

// UpdateFlow completes with the updated document identity.
type UpdateFlow struct {
	dex.FlowDefaults
	connection elasticsearch.Connection
}

// NewUpdateFlow binds the Elasticsearch connection at registration time.
func NewUpdateFlow(connection elasticsearch.Connection) *UpdateFlow {
	return &UpdateFlow{connection: connection}
}

// GetFlowType preserves this example's Flow identity.
func (*UpdateFlow) GetFlowType() string { return UpdateFlowType }

func (*UpdateFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{updatedDocumentAttribute}}
}

func (flow *UpdateFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startUpdateRequest{}),
		dex.DefineStep(elasticsearch.NewUpdateDocumentStep(elasticsearch.UpdateDocumentStepConfig[elasticsearch.UpdateDocumentInput]{
			StepType: updateOperationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Elasticsearch", Explanation: "Operate on one document by stable ID."},
			Connection:          flow.connection,
			MapToOperationInput: mapToUpdateInput,
			Updated:             sdkgo.GoTo(completeUpdateResult{}),
		})),
		dex.DefineStep(completeUpdateResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *UpdateFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:elasticsearch-document-update value-type:json editable:false description:"Updated document"
// GetDexSummary shows the updated document in Dex Web.
func (*UpdateFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readUpdatedDocument(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"elasticsearch-document-update": page}}, nil
}

// dex:field attribute-key:elasticsearch-document-update value-type:json editable:false description:"Updated document"
// GetDexDisplay shows the updated document in Dex Web.
func (*UpdateFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readUpdatedDocument(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"elasticsearch-document-update": page}}, nil
}

func readUpdatedDocument(ctx dex.Context) (elasticsearch.IndexedDocument, error) {
	page, err := updatedDocumentAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return elasticsearch.IndexedDocument{}, nil
	}
	return page, err
}

func mapToUpdateInput(input elasticsearch.UpdateDocumentInput) elasticsearch.UpdateDocumentInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the updated document input to the provider Step."
type startUpdateRequest struct{ dex.StepDefaults }

func (startUpdateRequest) GetStepType() string { return "StartUpdateDocument" }
func (startUpdateRequest) WaitFor(dex.Context, elasticsearch.UpdateDocumentInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startUpdateRequest) Execute(_ dex.Context, input elasticsearch.UpdateDocumentInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[elasticsearch.UpdateDocumentInput](updateOperationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the updated document."
type completeUpdateResult struct {
	dex.StepDefaultsNoWaitFor[elasticsearch.UpdateDocumentResult]
}

func (completeUpdateResult) GetStepType() string { return "CompleteUpdateDocument" }
func (completeUpdateResult) Execute(ctx dex.Context, result elasticsearch.UpdateDocumentResult) (*dex.StepDecision, error) {
	if err := updatedDocumentAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*UpdateFlow)(nil)
