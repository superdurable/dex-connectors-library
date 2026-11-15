// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package invoicechanged demonstrates the Xero invoiceChanged Trigger: every sales invoice or bill created
// or changed in the organisation starts one Flow, which reads the invoice back and records its state.
package invoicechanged

import (
	"errors"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/xero/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity each invoice change starts.
	FlowType = "XeroInvoiceChange"
	// ConnectionName is the static Dex Web connection for Xero.
	ConnectionName = "xero-invoice-watcher"
	// InvoiceChangedTriggerBinding is the stable invoiceChanged binding that starts this Flow.
	InvoiceChangedTriggerBinding = "invoice-changes"

	recordInvoiceChangeStepType   = "RecordInvoiceChange"
	readChangedInvoiceStepType    = "ReadChangedInvoice"
	completeInvoiceChangeStepType = "CompleteInvoiceChange"
)

var (
	invoiceChangeAttribute        = dex.DefineAttribute[Input]("xero-invoice-change")
	invoiceChangeOutcomeAttribute = dex.DefineAttribute[ChangeOutcome]("xero-invoice-change-outcome")
)

// Input is one invoiceChanged delivery.
type Input struct {
	// TriggerEventID is the stable Trigger event ID that also names the Flow.
	TriggerEventID string `json:"triggerEventId"`
	// Change is the delivered change.
	Change xero.InvoiceChangedEvent `json:"change"`
}

// ChangeOutcome is the Flow result and the value of its outcome Attribute.
type ChangeOutcome struct {
	// InvoiceID is the changed invoice.
	InvoiceID string `json:"invoiceId"`
	// DeliveredStatus is the invoice status in the delivered change.
	DeliveredStatus xero.InvoiceStatus `json:"deliveredStatus"`
	// Current is the invoice read back from Xero, which a later change may already have updated.
	Current xero.Invoice `json:"current"`
}

// Flow records one Xero invoice change.
type Flow struct {
	dex.FlowDefaults
	connection xero.Connection
}

// NewFlow binds the Xero Connection at registration time.
func NewFlow(connection xero.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and Xero connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordInvoiceChange{}),
		dex.DefineStep(xero.NewGetInvoiceStep(xero.GetInvoiceStepConfig[Input]{
			StepType: readChangedInvoiceStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "change", GroupLabel: "Change",
				Explanation: "Read the changed invoice back, so the record holds its current state rather than the polled copy.",
			},
			Connection: flow.connection, MapToOperationInput: MapToGetInvoiceInput,
			Found: sdkgo.GoTo(completeInvoiceChange{}),
		})),
		dex.DefineStep(completeInvoiceChange{}),
	}
}

// GetConnectorTriggerBindings declares the invoiceChanged binding and its invoice filter for Dex Web.
func (*Flow) GetConnectorTriggerBindings() []sdkgo.TriggerBindingDefinition {
	return []sdkgo.TriggerBindingDefinition{
		xero.DefineInvoiceChangedTriggerBinding(xero.InvoiceChangedTriggerBindingConfig{
			ConnectionName: ConnectionName, BindingName: InvoiceChangedTriggerBinding,
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
				ID: "invoiceFilter", UnitID: xero.UIUnitInvoiceChangeFilter, Label: "Invoices that start this Flow",
				Description: "Choose Sales invoices (ACCREC), Bills (ACCPAY), or both, and optionally the statuses an invoice must have after its change, such as PAID to react only to payments in full; Xero's own values are saved. Leave it unsaved to start a Flow for every invoice and bill created or changed in the organisation. The first start records existing invoices as a baseline and starts no Flow for them. Restart the Worker after saving; a changed filter records a new baseline.",
				Bindings: []sdkgo.ConnectorUIBinding{
					{Port: xero.UIInvoiceChangeFilterPortType, JSONPointer: "/type"},
					{Port: xero.UIInvoiceChangeFilterPortStatuses, JSONPointer: "/statuses"},
				},
			}}},
		}),
	}
}

// GetRPCs returns the summary and display RPCs used by Dex Web.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the change and outcome Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{invoiceChangeAttribute, invoiceChangeOutcomeAttribute}}
}

// GetDexSummary returns the delivered change and its outcome.
//
// dex:field attribute-key:xero-invoice-change value-type:json editable:false description:"Delivered invoice change"
// dex:field attribute-key:xero-invoice-change-outcome value-type:json editable:false description:"Recorded change"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	change, outcome, err := invoiceChangeInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"xero-invoice-change":         change,
		"xero-invoice-change-outcome": outcome,
	}}, nil
}

// GetDexDisplay returns the delivered change and the invoice read back from Xero.
//
// dex:field attribute-key:xero-invoice-change value-type:json editable:false description:"Invoice as the poll listed it"
// dex:field attribute-key:xero-invoice-change-outcome value-type:json editable:false description:"Current invoice read back from Xero"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	change, outcome, err := invoiceChangeInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"xero-invoice-change":         change,
		"xero-invoice-change-outcome": outcome,
	}}, nil
}

func invoiceChangeInspection(ctx dex.Context) (Input, ChangeOutcome, error) {
	change, err := optionalAttribute(ctx, invoiceChangeAttribute)
	if err != nil {
		return Input{}, ChangeOutcome{}, err
	}
	outcome, err := optionalAttribute(ctx, invoiceChangeOutcomeAttribute)
	return change, outcome, err
}

// optionalAttribute returns the zero value while a running Flow has not set the Attribute yet.
func optionalAttribute[T any](ctx dex.Context, attribute dex.Attribute[T]) (T, error) {
	value, err := attribute.Get(ctx)
	var missingAttribute *dex.AttributeNotFoundError
	if errors.As(err, &missingAttribute) {
		var zero T
		return zero, nil
	}
	return value, err
}

// ResolveFlowID names one Flow per change; a redelivered change finds its existing Flow.
func ResolveFlowID(event sdkgo.TriggerEvent[xero.InvoiceChangedEvent]) string {
	return "xero-invoice-change-" + event.ID
}

// MapToFlowInput keeps the Trigger event ID beside the delivered change.
func MapToFlowInput(event sdkgo.TriggerEvent[xero.InvoiceChangedEvent]) Input {
	return Input{TriggerEventID: event.ID, Change: event.Payload}
}

// AcceptEveryInvoiceChange admits every change; the binding's filter already narrows type and status.
func AcceptEveryInvoiceChange(sdkgo.TriggerEvent[xero.InvoiceChangedEvent]) bool { return true }

// MapToGetInvoiceInput reads the changed invoice by its InvoiceID.
func MapToGetInvoiceInput(input Input) xero.GetInvoiceInput {
	return xero.GetInvoiceInput{InvoiceID: input.Change.Invoice.InvoiceID}
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the delivered invoice change before reading the invoice back from Xero."
type recordInvoiceChange struct {
	dex.StepDefaults
}

func (recordInvoiceChange) GetStepType() string { return recordInvoiceChangeStepType }

// WaitFor skips immediately because the Trigger starts the Flow at this Step.
func (recordInvoiceChange) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordInvoiceChange) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	if err := validateInput(input); err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	if err := invoiceChangeAttribute.Set(ctx, input); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[Input](readChangedInvoiceStepType), input), nil
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the invoice read back from Xero as the outcome and complete the Flow."
type completeInvoiceChange struct {
	dex.StepDefaultsNoWaitFor[xero.GetInvoiceResult]
}

func (completeInvoiceChange) GetStepType() string { return completeInvoiceChangeStepType }

func (completeInvoiceChange) Execute(ctx dex.Context, result xero.GetInvoiceResult) (*dex.StepDecision, error) {
	change, err := invoiceChangeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome := ChangeOutcome{
		InvoiceID: change.Change.Invoice.InvoiceID, DeliveredStatus: change.Change.Invoice.Status, Current: result.Value,
	}
	if err := invoiceChangeOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

func validateInput(input Input) error {
	switch {
	case strings.TrimSpace(input.TriggerEventID) == "":
		return errors.New("triggerEventId is required")
	case strings.TrimSpace(input.Change.Invoice.InvoiceID) == "":
		return errors.New("change.invoice.invoiceId is required")
	case input.Change.Invoice.Status == "":
		return errors.New("change.invoice.status is required")
	}
	return nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
