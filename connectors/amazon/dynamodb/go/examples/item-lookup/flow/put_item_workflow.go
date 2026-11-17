// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package itemlookup

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/amazon/dynamodb/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// PutItemFlowType identifies the complete item replacement example.
	PutItemFlowType = "AmazonDynamoDBPutItem"
	putItemStepType = "CallPutItem"
)

var putItemOutcomeAttribute = dex.DefineAttribute[dynamodb.ItemWriteResult]("dynamodb-put-item-outcome")

// PutItemFlow writes one complete non-secret item to a configured table.
type PutItemFlow struct {
	dex.FlowDefaults
	connection dynamodb.Connection
}

// NewPutItemFlow binds the static DynamoDB connection at registration.
func NewPutItemFlow(connection dynamodb.Connection) *PutItemFlow {
	return &PutItemFlow{connection: connection}
}

func (*PutItemFlow) GetFlowType() string { return PutItemFlowType }
func (*PutItemFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{putItemOutcomeAttribute}}
}
func (flow *PutItemFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startPutItem{}),
		dex.DefineStep(dynamodb.NewPutItemStep(dynamodb.PutItemStepConfig[dynamodb.PutItemInput]{
			StepType: putItemStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Amazon DynamoDB", Explanation: "Replace one complete item by primary key."},
			Connection:  flow.connection, MapToOperationInput: MapToPutItemInput,
			Written: sdkgo.GoTo(completePutItem{}),
		})),
		dex.DefineStep(completePutItem{}),
	}
}

// GetRPCs exposes the write outcome in Dex Web summary and display.
func (flow *PutItemFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:dynamodb-put-item-outcome value-type:json editable:false description:"Item write outcome"
// GetDexSummary returns the persisted write outcome.
func (*PutItemFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readPutItemOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"dynamodb-put-item-outcome": outcome}}, nil
}

// dex:field attribute-key:dynamodb-put-item-outcome value-type:json editable:false description:"Item write outcome"
// GetDexDisplay returns the persisted write outcome.
func (*PutItemFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readPutItemOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"dynamodb-put-item-outcome": outcome}}, nil
}

func readPutItemOutcome(ctx dex.Context) (dynamodb.ItemWriteResult, error) {
	outcome, err := putItemOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return dynamodb.ItemWriteResult{}, nil
	}
	return outcome, err
}

// MapToPutItemInput preserves the typed write request.
func MapToPutItemInput(input dynamodb.PutItemInput) dynamodb.PutItemInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the complete item to its provider Step."
type startPutItem struct{ dex.StepDefaults }

func (startPutItem) GetStepType() string { return "StartPutItem" }
func (startPutItem) WaitFor(dex.Context, dynamodb.PutItemInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startPutItem) Execute(_ dex.Context, input dynamodb.PutItemInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[dynamodb.PutItemInput](putItemStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete after DynamoDB accepts the replacement."
type completePutItem struct {
	dex.StepDefaultsNoWaitFor[dynamodb.PutItemResult]
}

func (completePutItem) GetStepType() string { return "CompletePutItem" }
func (completePutItem) Execute(ctx dex.Context, result dynamodb.PutItemResult) (*dex.StepDecision, error) {
	if err := putItemOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*PutItemFlow)(nil)
