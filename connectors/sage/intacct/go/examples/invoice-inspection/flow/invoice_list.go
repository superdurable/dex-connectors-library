// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package invoiceinspection

import (
	"github.com/superdurable/dex-connectors-library/connectors/sage/intacct/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const listInvoicesStepType = "CallListInvoices"

var invoiceListOperationAttribute = dex.DefineAttribute[string]("operation")

// InvoiceListFlow lists the first page of Accounts Receivable invoice identities.
type InvoiceListFlow struct {
	dex.FlowDefaults
	connection sageintacct.Connection
}

// NewInvoiceListFlow binds the example's static connection.
func NewInvoiceListFlow(connection sageintacct.Connection) *InvoiceListFlow {
	return &InvoiceListFlow{connection: connection}
}

// GetPersistenceSchema retains the operation label for Dex Web views.
func (*InvoiceListFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{invoiceListOperationAttribute}}
}

// GetSteps registers the invoice list Query and result Step.
func (flow *InvoiceListFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startInvoiceList{}),
		dex.DefineStep(sageintacct.NewListInvoicesStep(sageintacct.ListInvoicesStepConfig[sageintacct.ListInvoicesInput]{
			StepType:            listInvoicesStepType,
			ConnectionName:      ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Sage Intacct", Explanation: "List Accounts Receivable invoice identities."},
			Connection:          flow.connection,
			MapToOperationInput: mapInvoiceListInput,
			Found:               sdkgo.GoTo(completeInvoiceList{}),
		})),
		dex.DefineStep(completeInvoiceList{}),
	}
}

// GetRPCs exposes the invoice list Flow's read-only Dex Web views.
func (flow *InvoiceListFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:operation value-type:string editable:false description:"Provider operation"
// GetDexSummary describes the invoice list Flow in Dex Web.
func (*InvoiceListFlow) GetDexSummary(dex.Context, dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"operation": "listInvoices"}}, nil
}

// dex:field attribute-key:operation value-type:string editable:false description:"Provider operation"
// GetDexDisplay describes the invoice list Flow in Dex Web.
func (*InvoiceListFlow) GetDexDisplay(dex.Context, dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"operation": "listInvoices"}}, nil
}

func mapInvoiceListInput(input sageintacct.ListInvoicesInput) sageintacct.ListInvoicesInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Start the invoice list Query."
type startInvoiceList struct{ dex.StepDefaults }

func (startInvoiceList) WaitFor(dex.Context, sageintacct.ListInvoicesInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (startInvoiceList) Execute(ctx dex.Context, input sageintacct.ListInvoicesInput) (*dex.StepDecision, error) {
	if err := invoiceListOperationAttribute.Set(ctx, "listInvoices"); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[sageintacct.ListInvoicesInput](listInvoicesStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the invoice list."
type completeInvoiceList struct {
	dex.StepDefaultsNoWaitFor[sageintacct.ListInvoicesResult]
}

func (completeInvoiceList) Execute(_ dex.Context, result sageintacct.ListInvoicesResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(result.Value), nil
}
