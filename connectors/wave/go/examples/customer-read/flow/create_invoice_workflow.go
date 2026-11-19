// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package customerread

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/wave/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// CreateInvoiceFlowType identifies the invoice creation example.
const CreateInvoiceFlowType = "WaveCreateInvoice"

var createdInvoiceAttribute = dex.DefineAttribute[wave.InvoiceProfile]("wave-created-invoice")

// CreateInvoiceFlow creates an invoice for one customer and product.
type CreateInvoiceFlow struct {
	dex.FlowDefaults
	connection wave.Connection
}

// NewCreateInvoiceFlow binds the static Wave connection.
func NewCreateInvoiceFlow(connection wave.Connection) *CreateInvoiceFlow {
	return &CreateInvoiceFlow{connection: connection}
}

// GetFlowType returns the stable invoice creation Flow identity.
func (*CreateInvoiceFlow) GetFlowType() string { return CreateInvoiceFlowType }

// GetPersistenceSchema registers the created invoice Attribute.
func (*CreateInvoiceFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdInvoiceAttribute}}
}

// GetSteps registers the start, create, and completion Steps.
func (flow *CreateInvoiceFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startCreateInvoice{}),
		dex.DefineStep(wave.NewCreateInvoiceStep(wave.CreateInvoiceStepConfig[wave.CreateInvoiceInput]{
			StepType: "CallCreateInvoice", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Wave", Explanation: "Create one Wave invoice for a customer and product."},
			MapToOperationInput: mapCreateInvoiceInput, Created: sdkgo.GoTo(completeCreateInvoice{}),
		})),
		dex.DefineStep(completeCreateInvoice{}),
	}
}

// GetRPCs registers read-only Dex Web views.
func (flow *CreateInvoiceFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:wave-created-invoice value-type:json editable:false description:"Created invoice"
// GetDexSummary returns the created invoice when available.
func (*CreateInvoiceFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readCreatedInvoice(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"wave-created-invoice": value}}, nil
}

// dex:field attribute-key:wave-created-invoice value-type:json editable:false description:"Created invoice"
// GetDexDisplay returns the created invoice when available.
func (*CreateInvoiceFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readCreatedInvoice(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"wave-created-invoice": value}}, nil
}

func readCreatedInvoice(ctx dex.Context) (wave.InvoiceProfile, error) {
	value, err := createdInvoiceAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return wave.InvoiceProfile{}, nil
	}
	return value, err
}
func mapCreateInvoiceInput(input wave.CreateInvoiceInput) wave.CreateInvoiceInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed business, customer, and product IDs to the provider Step."
type startCreateInvoice struct{ dex.StepDefaults }

func (startCreateInvoice) GetStepType() string { return "StartWaveInvoiceCreation" }
func (startCreateInvoice) WaitFor(dex.Context, wave.CreateInvoiceInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startCreateInvoice) Execute(_ dex.Context, input wave.CreateInvoiceInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[wave.CreateInvoiceInput]("CallCreateInvoice"), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the created invoice metadata."
type completeCreateInvoice struct {
	dex.StepDefaultsNoWaitFor[wave.CreateInvoiceResult]
}

func (completeCreateInvoice) GetStepType() string { return "CompleteWaveInvoiceCreation" }
func (completeCreateInvoice) Execute(ctx dex.Context, result wave.CreateInvoiceResult) (*dex.StepDecision, error) {
	if err := createdInvoiceAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CreateInvoiceFlow)(nil)
