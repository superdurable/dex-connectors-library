// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package freshbooks runs FreshBooks getInvoice as one Dex Flow.
package invoice

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/freshbooks/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "FreshBooksInvoiceLookup"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "freshbooks"
	operationStepType = "CallGetInvoice"
)

var invoiceAttribute = dex.DefineAttribute[freshbooks.Invoice]("freshbooks-invoice")

// Flow runs GetInvoice once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection freshbooks.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection freshbooks.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{invoiceAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(freshbooks.NewGetInvoiceStep(freshbooks.GetInvoiceStepConfig[freshbooks.GetInvoiceInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "FreshBooks", Explanation: "Read the selected accounting invoice with FreshBooks OAuth."},
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

// dex:field attribute-key:freshbooks-invoice value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readInvoice(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"freshbooks-invoice": outcome}}, nil
}

// dex:field attribute-key:freshbooks-invoice value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readInvoice(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"freshbooks-invoice": outcome}}, nil
}

func readInvoice(ctx dex.Context) (freshbooks.Invoice, error) {
	outcome, err := invoiceAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return freshbooks.Invoice{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input freshbooks.GetInvoiceInput) freshbooks.GetInvoiceInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetInvoice" }
func (startRequest) WaitFor(dex.Context, freshbooks.GetInvoiceInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input freshbooks.GetInvoiceInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[freshbooks.GetInvoiceInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[freshbooks.GetInvoiceResult]
}

func (completeResult) GetStepType() string { return "CompleteGetInvoice" }
func (completeResult) Execute(ctx dex.Context, result freshbooks.GetInvoiceResult) (*dex.StepDecision, error) {
	if err := invoiceAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
