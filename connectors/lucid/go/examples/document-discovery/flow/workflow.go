// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package documentdiscovery runs Lucid listDocuments as one Dex Flow.
package documentdiscovery

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/lucid/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "LucidDocumentDiscovery"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "lucid"
	operationStepType = "CallListDocuments"
)

var documentsAttribute = dex.DefineAttribute[lucid.DocumentsPage]("lucid-documents")

// Flow runs ListDocuments once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection lucid.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection lucid.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{documentsAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(lucid.NewListDocumentsStep(lucid.ListDocumentsStepConfig[lucid.ListDocumentsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Lucid", Explanation: "List one page of Lucid documents."},
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

// dex:field attribute-key:lucid-documents value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readDocuments(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"lucid-documents": outcome}}, nil
}

// dex:field attribute-key:lucid-documents value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readDocuments(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"lucid-documents": outcome}}, nil
}

func readDocuments(ctx dex.Context) (lucid.DocumentsPage, error) {
	outcome, err := documentsAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return lucid.DocumentsPage{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input lucid.ListDocumentsInput) lucid.ListDocumentsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListDocuments" }
func (startRequest) WaitFor(dex.Context, lucid.ListDocumentsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input lucid.ListDocumentsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[lucid.ListDocumentsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[lucid.ListDocumentsResult]
}

func (completeResult) GetStepType() string { return "CompleteListDocuments" }
func (completeResult) Execute(ctx dex.Context, result lucid.ListDocumentsResult) (*dex.StepDecision, error) {
	if err := documentsAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
