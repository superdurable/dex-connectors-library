// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package invoiceinspection

import (
	"github.com/superdurable/dex-connectors-library/connectors/sage/intacct/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const listInvoicesStepType = "CallListInvoices"

// InvoiceListFlow lists the first page of Accounts Receivable invoice identities.
type InvoiceListFlow struct {
	dex.FlowDefaults
	connection sageintacct.Connection
}

// NewInvoiceListFlow binds the example's static connection.
func NewInvoiceListFlow(connection sageintacct.Connection) *InvoiceListFlow {
	return &InvoiceListFlow{connection: connection}
}

// GetPersistenceSchema declares no retained state for this one-page Flow.
func (*InvoiceListFlow) GetPersistenceSchema() dex.PersistenceSchema { return dex.PersistenceSchema{} }

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

func mapInvoiceListInput(input sageintacct.ListInvoicesInput) sageintacct.ListInvoicesInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Start the invoice list Query."
type startInvoiceList struct{ dex.StepDefaults }

func (startInvoiceList) WaitFor(dex.Context, sageintacct.ListInvoicesInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (startInvoiceList) Execute(_ dex.Context, input sageintacct.ListInvoicesInput) (*dex.StepDecision, error) {
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
