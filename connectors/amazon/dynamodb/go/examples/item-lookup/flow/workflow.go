// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package itemlookup runs Amazon DynamoDB getItem as one Dex Flow.
package itemlookup

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/amazon/dynamodb/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "AmazonDynamoDBItemLookup"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "amazon-dynamodb"
	operationStepType = "CallGetItem"
)

var itemAttribute = dex.DefineAttribute[dynamodb.ItemResult]("dynamodb-item-lookup")

// Flow runs GetItem once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection dynamodb.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection dynamodb.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{itemAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(dynamodb.NewGetItemStep(dynamodb.GetItemStepConfig[dynamodb.GetItemInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Amazon DynamoDB", Explanation: "Read one DynamoDB item."},
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

// dex:field attribute-key:dynamodb-item-lookup value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readItemResult(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"dynamodb-item-lookup": outcome}}, nil
}

// dex:field attribute-key:dynamodb-item-lookup value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readItemResult(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"dynamodb-item-lookup": outcome}}, nil
}

func readItemResult(ctx dex.Context) (dynamodb.ItemResult, error) {
	outcome, err := itemAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return dynamodb.ItemResult{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input dynamodb.GetItemInput) dynamodb.GetItemInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetItem" }
func (startRequest) WaitFor(dex.Context, dynamodb.GetItemInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input dynamodb.GetItemInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[dynamodb.GetItemInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[dynamodb.GetItemResult]
}

func (completeResult) GetStepType() string { return "CompleteGetItem" }
func (completeResult) Execute(ctx dex.Context, result dynamodb.GetItemResult) (*dex.StepDecision, error) {
	if err := itemAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
