// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package documentdiscovery runs MongoDB findDocuments as one Dex Flow.
package documentdiscovery

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/mongodb/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "MongoDBDocumentDiscovery"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "mongodb"
	operationStepType = "CallFindDocuments"
)

var documentsAttribute = dex.DefineAttribute[mongodb.DocumentSet]("mongodb-documents")

// Flow runs FindDocuments once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection mongodb.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection mongodb.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{documentsAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(mongodb.NewFindDocumentsStep(mongodb.FindDocumentsStepConfig[mongodb.FindDocumentsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "MongoDB", Explanation: "Find documents in the selected MongoDB collection."},
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

// dex:field attribute-key:mongodb-documents value-type:json editable:false description:"Documents summary"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readDocuments(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"mongodb-documents": outcome}}, nil
}

// dex:field attribute-key:mongodb-documents value-type:json editable:false description:"Documents summary"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readDocuments(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"mongodb-documents": outcome}}, nil
}

func readDocuments(ctx dex.Context) (mongodb.DocumentSet, error) {
	outcome, err := documentsAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return mongodb.DocumentSet{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input mongodb.FindDocumentsInput) mongodb.FindDocumentsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartFindDocuments" }
func (startRequest) WaitFor(dex.Context, mongodb.FindDocumentsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input mongodb.FindDocumentsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[mongodb.FindDocumentsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[mongodb.FindDocumentsResult]
}

func (completeResult) GetStepType() string { return "CompleteFindDocuments" }
func (completeResult) Execute(ctx dex.Context, result mongodb.FindDocumentsResult) (*dex.StepDecision, error) {
	if err := documentsAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
