// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package transactioninspection runs Paddle getTransaction as one Dex Flow.
package transactioninspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/paddle/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "PaddleTransactionInspection"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "paddle"
	operationStepType = "CallGetTransaction"
)

var transactionAttribute = dex.DefineAttribute[paddle.TransactionStatus]("paddle-transaction")

// Flow runs GetTransaction once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection paddle.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection paddle.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{transactionAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(paddle.NewGetTransactionStep(paddle.GetTransactionStepConfig[paddle.GetTransactionInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Paddle", Explanation: "Read a transaction from Paddle."},
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

// dex:field attribute-key:paddle-transaction value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readTransaction(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"paddle-transaction": outcome}}, nil
}

// dex:field attribute-key:paddle-transaction value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readTransaction(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"paddle-transaction": outcome}}, nil
}

func readTransaction(ctx dex.Context) (paddle.TransactionStatus, error) {
	outcome, err := transactionAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return paddle.TransactionStatus{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input paddle.GetTransactionInput) paddle.GetTransactionInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetTransaction" }
func (startRequest) WaitFor(dex.Context, paddle.GetTransactionInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input paddle.GetTransactionInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[paddle.GetTransactionInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[paddle.GetTransactionResult]
}

func (completeResult) GetStepType() string { return "CompleteGetTransaction" }
func (completeResult) Execute(ctx dex.Context, result paddle.GetTransactionResult) (*dex.StepDecision, error) {
	if err := transactionAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
