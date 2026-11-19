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
	// CreateFlowType is the stable example Flow identity.
	CreateFlowType          = "ElasticsearchCreateDocument"
	createOperationStepType = "CallCreateDocument"
)

var createdDocumentAttribute = dex.DefineAttribute[elasticsearch.IndexedDocument]("elasticsearch-document-create")

// CreateFlow completes with the created document identity.
type CreateFlow struct {
	dex.FlowDefaults
	connection elasticsearch.Connection
}

// NewCreateFlow binds the Elasticsearch connection at registration time.
func NewCreateFlow(connection elasticsearch.Connection) *CreateFlow {
	return &CreateFlow{connection: connection}
}

// GetFlowType preserves this example's Flow identity.
func (*CreateFlow) GetFlowType() string { return CreateFlowType }

func (*CreateFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdDocumentAttribute}}
}

func (flow *CreateFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startCreateRequest{}),
		dex.DefineStep(elasticsearch.NewCreateDocumentStep(elasticsearch.CreateDocumentStepConfig[elasticsearch.CreateDocumentInput]{
			StepType: createOperationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Elasticsearch", Explanation: "Create a document with a provider-generated ID."},
			Connection:          flow.connection,
			MapToOperationInput: mapToCreateInput,
			Created:             sdkgo.GoTo(completeCreateResult{}),
		})),
		dex.DefineStep(completeCreateResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *CreateFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:elasticsearch-document-create value-type:json editable:false description:"Updated document"
// GetDexSummary shows the created document in Dex Web.
func (*CreateFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readCreatedDocument(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"elasticsearch-document-create": page}}, nil
}

// dex:field attribute-key:elasticsearch-document-create value-type:json editable:false description:"Updated document"
// GetDexDisplay shows the created document in Dex Web.
func (*CreateFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readCreatedDocument(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"elasticsearch-document-create": page}}, nil
}

func readCreatedDocument(ctx dex.Context) (elasticsearch.IndexedDocument, error) {
	page, err := createdDocumentAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return elasticsearch.IndexedDocument{}, nil
	}
	return page, err
}

func mapToCreateInput(input elasticsearch.CreateDocumentInput) elasticsearch.CreateDocumentInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the created document input to the provider Step."
type startCreateRequest struct{ dex.StepDefaults }

func (startCreateRequest) GetStepType() string { return "StartCreateDocument" }
func (startCreateRequest) WaitFor(dex.Context, elasticsearch.CreateDocumentInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startCreateRequest) Execute(_ dex.Context, input elasticsearch.CreateDocumentInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[elasticsearch.CreateDocumentInput](createOperationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the created document."
type completeCreateResult struct {
	dex.StepDefaultsNoWaitFor[elasticsearch.CreateDocumentResult]
}

func (completeCreateResult) GetStepType() string { return "CompleteCreateDocument" }
func (completeCreateResult) Execute(ctx dex.Context, result elasticsearch.CreateDocumentResult) (*dex.StepDecision, error) {
	if err := createdDocumentAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CreateFlow)(nil)
