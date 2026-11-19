// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package accountinspection runs Recurly getInvoice as one Dex Flow.
package accountinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/recurly/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// InvoiceFlowType is the stable example Flow identity.
	InvoiceFlowType = "RecurlyInvoiceInspection"
	invoiceStepType = "CallGetInvoice"
)

var invoiceAttribute = dex.DefineAttribute[recurly.InvoiceSummary]("recurly-invoice")

// InvoiceFlow runs GetInvoice once and completes with its typed output.
type InvoiceFlow struct {
	dex.FlowDefaults
	connection recurly.Connection
}

// NewInvoiceFlow binds the provider connection at registration time.
func NewInvoiceFlow(connection recurly.Connection) *InvoiceFlow {
	return &InvoiceFlow{connection: connection}
}

func (*InvoiceFlow) GetFlowType() string { return InvoiceFlowType }

func (*InvoiceFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{invoiceAttribute}}
}

func (flow *InvoiceFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startInvoiceRequest{}),
		dex.DefineStep(recurly.NewGetInvoiceStep(recurly.GetInvoiceStepConfig[recurly.GetInvoiceInput]{
			StepType: invoiceStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Recurly", Explanation: "Read a invoice from Recurly."},
			Connection:          flow.connection,
			MapToOperationInput: mapInvoiceInput,
			Found:               sdkgo.GoTo(completeInvoiceResult{}),
		})),
		dex.DefineStep(completeInvoiceResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *InvoiceFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:recurly-invoice value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*InvoiceFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readInvoice(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"recurly-invoice": outcome}}, nil
}

// dex:field attribute-key:recurly-invoice value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*InvoiceFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readInvoice(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"recurly-invoice": outcome}}, nil
}

func readInvoice(ctx dex.Context) (recurly.InvoiceSummary, error) {
	outcome, err := invoiceAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return recurly.InvoiceSummary{}, nil
	}
	return outcome, err
}

// mapInvoiceInput preserves the validated typed start input for the provider call.
func mapInvoiceInput(input recurly.GetInvoiceInput) recurly.GetInvoiceInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startInvoiceRequest struct{ dex.StepDefaults }

func (startInvoiceRequest) GetStepType() string { return "StartGetInvoice" }
func (startInvoiceRequest) WaitFor(dex.Context, recurly.GetInvoiceInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startInvoiceRequest) Execute(_ dex.Context, input recurly.GetInvoiceInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[recurly.GetInvoiceInput](invoiceStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeInvoiceResult struct {
	dex.StepDefaultsNoWaitFor[recurly.GetInvoiceResult]
}

func (completeInvoiceResult) GetStepType() string { return "CompleteGetInvoice" }
func (completeInvoiceResult) Execute(ctx dex.Context, result recurly.GetInvoiceResult) (*dex.StepDecision, error) {
	if err := invoiceAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*InvoiceFlow)(nil)
