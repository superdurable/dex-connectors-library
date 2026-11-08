// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package documents runs Google Firestore listDocuments as one Dex Flow.
package documents

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/google/firestore/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "GoogleFirestoreDocumentsList"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "google-firestore"
	operationStepType = "CallListDocuments"
)

var documentsAttribute = dex.DefineAttribute[firestore.DocumentsPage]("google-firestore-page")

// Flow runs ListDocuments once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection firestore.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection firestore.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{documentsAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(firestore.NewListDocumentsStep(firestore.ListDocumentsStepConfig[firestore.ListDocumentsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Google Firestore", Explanation: "List documents visible in the selected Google Cloud project."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Listed:              sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:google-firestore-page value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readDocuments(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"google-firestore-page": outcome}}, nil
}

// dex:field attribute-key:google-firestore-page value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readDocuments(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"google-firestore-page": outcome}}, nil
}

func readDocuments(ctx dex.Context) (firestore.DocumentsPage, error) {
	outcome, err := documentsAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return firestore.DocumentsPage{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input firestore.ListDocumentsInput) firestore.ListDocumentsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListDocuments" }
func (startRequest) WaitFor(dex.Context, firestore.ListDocumentsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input firestore.ListDocumentsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[firestore.ListDocumentsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[firestore.ListDocumentsResult]
}

func (completeResult) GetStepType() string { return "CompleteListDocuments" }
func (completeResult) Execute(ctx dex.Context, result firestore.ListDocumentsResult) (*dex.StepDecision, error) {
	if err := documentsAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
