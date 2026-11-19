// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package documents

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/google/firestore/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// DeleteFlowType is the stable example document deletion Flow identity.
	DeleteFlowType          = "GoogleFirestoreDocumentDelete"
	deleteOperationStepType = "CallDeleteDocument"
)

var deletedDocumentAttribute = dex.DefineAttribute[firestore.FirestoreDocument]("google-firestore-deleted-document")

// DeleteFlow removes one document at a known identity.
type DeleteFlow struct {
	dex.FlowDefaults
	connection firestore.Connection
}

// NewDeleteFlow binds the provider connection before registration.
func NewDeleteFlow(connection firestore.Connection) *DeleteFlow {
	return &DeleteFlow{connection: connection}
}
func (*DeleteFlow) GetFlowType() string { return DeleteFlowType }
func (*DeleteFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{deletedDocumentAttribute}}
}
func (flow *DeleteFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startDocumentDelete{}),
		dex.DefineStep(firestore.NewDeleteDocumentStep(firestore.DeleteDocumentStepConfig[firestore.GetDocumentInput]{
			StepType: deleteOperationStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Google Firestore", Explanation: "Delete the selected Firestore document."},
			Connection:  flow.connection, MapToOperationInput: mapDeleteDocumentInput,
			Deleted: sdkgo.GoTo(completeDocumentDelete{}),
		})),
		dex.DefineStep(completeDocumentDelete{}),
	}
}

// GetRPCs exposes read-only deletion summary and display views.
func (flow *DeleteFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:google-firestore-deleted-document value-type:json editable:false description:"Deleted document"
// GetDexSummary shows the deleted document in Dex Web.
func (*DeleteFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	document, err := readDeletedDocument(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"google-firestore-deleted-document": document}}, nil
}

// dex:field attribute-key:google-firestore-deleted-document value-type:json editable:false description:"Deleted document"
// GetDexDisplay shows the deleted document in Dex Web.
func (*DeleteFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	document, err := readDeletedDocument(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"google-firestore-deleted-document": document}}, nil
}

func readDeletedDocument(ctx dex.Context) (firestore.FirestoreDocument, error) {
	document, err := deletedDocumentAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return firestore.FirestoreDocument{}, nil
	}
	return document, err
}

func mapDeleteDocumentInput(input firestore.GetDocumentInput) firestore.GetDocumentInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Send the selected document identity to Firestore."
type startDocumentDelete struct{ dex.StepDefaults }

func (startDocumentDelete) GetStepType() string { return "StartDeleteDocument" }
func (startDocumentDelete) WaitFor(dex.Context, firestore.GetDocumentInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startDocumentDelete) Execute(_ dex.Context, input firestore.GetDocumentInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[firestore.GetDocumentInput](deleteOperationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Store and return the deleted document identity."
type completeDocumentDelete struct {
	dex.StepDefaultsNoWaitFor[firestore.DeleteDocumentResult]
}

func (completeDocumentDelete) GetStepType() string { return "CompleteDeleteDocument" }
func (completeDocumentDelete) Execute(ctx dex.Context, result firestore.DeleteDocumentResult) (*dex.StepDecision, error) {
	if err := deletedDocumentAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*DeleteFlow)(nil)
