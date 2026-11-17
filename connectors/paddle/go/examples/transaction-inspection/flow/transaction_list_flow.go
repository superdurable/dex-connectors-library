// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package transactioninspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/paddle/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// TransactionFlowType is the stable Paddle transaction listing Flow identity.
const TransactionFlowType = "PaddleTransactionListing"

const transactionStepType = "CallListTransactions"

var transactionPageAttribute = dex.DefineAttribute[paddle.TransactionPage]("paddle-transaction-page")

// TransactionFlow lists one page of Paddle transactions.
type TransactionFlow struct {
	dex.FlowDefaults
	connection paddle.Connection
}

// NewTransactionFlow binds the Paddle connection for listing transactions.
func NewTransactionFlow(connection paddle.Connection) *TransactionFlow {
	return &TransactionFlow{connection: connection}
}

func (*TransactionFlow) GetFlowType() string { return TransactionFlowType }

func (*TransactionFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{transactionPageAttribute}}
}

func (flow *TransactionFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startTransactionList{}),
		dex.DefineStep(paddle.NewListTransactionsStep(paddle.ListTransactionsStepConfig[paddle.ListTransactionsInput]{
			StepType: transactionStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Paddle", Explanation: "List one page of Paddle transactions."},
			Connection:  flow.connection, MapToOperationInput: mapTransactionListInput,
			Listed: sdkgo.GoTo(completeTransactionList{}),
		})),
		dex.DefineStep(completeTransactionList{}),
	}
}

func (flow *TransactionFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:paddle-transaction-page value-type:json editable:false description:"Transaction page"
func (*TransactionFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readTransactionPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"paddle-transaction-page": page}}, nil
}

// dex:field attribute-key:paddle-transaction-page value-type:json editable:false description:"Transaction page"
func (*TransactionFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readTransactionPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"paddle-transaction-page": page}}, nil
}

func readTransactionPage(ctx dex.Context) (paddle.TransactionPage, error) {
	page, err := transactionPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return paddle.TransactionPage{}, nil
	}
	return page, err
}

func mapTransactionListInput(input paddle.ListTransactionsInput) paddle.ListTransactionsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the requested transaction page and cursor to Paddle."
type startTransactionList struct{ dex.StepDefaults }

func (startTransactionList) GetStepType() string { return "StartListTransactions" }
func (startTransactionList) WaitFor(dex.Context, paddle.ListTransactionsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startTransactionList) Execute(_ dex.Context, input paddle.ListTransactionsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[paddle.ListTransactionsInput](transactionStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the returned transaction page."
type completeTransactionList struct {
	dex.StepDefaultsNoWaitFor[paddle.ListTransactionsResult]
}

func (completeTransactionList) GetStepType() string { return "CompleteListTransactions" }
func (completeTransactionList) Execute(ctx dex.Context, result paddle.ListTransactionsResult) (*dex.StepDecision, error) {
	if err := transactionPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*TransactionFlow)(nil)
