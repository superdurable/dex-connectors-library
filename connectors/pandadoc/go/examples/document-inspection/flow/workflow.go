// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package documentinspection runs PandaDoc getDocument as one Dex Flow.
package documentinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/pandadoc/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "PandaDocDocumentInspection"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "pandadoc"
	operationStepType = "CallGetDocument"
)

var documentAttribute = dex.DefineAttribute[pandadoc.DocumentStatus]("pandadoc-document")

// Flow runs GetDocument once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection pandadoc.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection pandadoc.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{documentAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(pandadoc.NewGetDocumentStep(pandadoc.GetDocumentStepConfig[pandadoc.GetDocumentInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "PandaDoc", Explanation: "Read a document from PandaDoc."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Found:               sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:pandadoc-document value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readDocument(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"pandadoc-document": outcome}}, nil
}

// dex:field attribute-key:pandadoc-document value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readDocument(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"pandadoc-document": outcome}}, nil
}

func readDocument(ctx dex.Context) (pandadoc.DocumentStatus, error) {
	outcome, err := documentAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return pandadoc.DocumentStatus{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input pandadoc.GetDocumentInput) pandadoc.GetDocumentInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetDocument" }
func (startRequest) WaitFor(dex.Context, pandadoc.GetDocumentInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input pandadoc.GetDocumentInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[pandadoc.GetDocumentInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[pandadoc.GetDocumentResult]
}

func (completeResult) GetStepType() string { return "CompleteGetDocument" }
func (completeResult) Execute(ctx dex.Context, result pandadoc.GetDocumentResult) (*dex.StepDecision, error) {
	if err := documentAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
