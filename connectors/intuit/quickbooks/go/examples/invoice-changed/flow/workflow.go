// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package invoicechanged demonstrates the QuickBooks invoiceChanged Trigger: every invoice created or
// changed in the connected company starts one Flow, which reads the invoice back and records whether it is
// paid.
package invoicechanged

import (
	"errors"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/intuit/quickbooks/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity each invoice change starts.
	FlowType = "QuickBooksInvoiceChange"
	// ConnectionName is the static Dex Web connection for the watched QuickBooks company.
	ConnectionName = "quickbooks-invoice-watcher"
	// InvoiceChangedTriggerBinding is the stable invoiceChanged binding that starts this Flow.
	InvoiceChangedTriggerBinding = "quickbooks-invoice-changes"

	recordInvoiceChangeStepType   = "RecordInvoiceChange"
	readChangedInvoiceStepType    = "ReadChangedInvoice"
	completeInvoiceChangeStepType = "CompleteInvoiceChange"
)

var (
	invoiceChangeAttribute        = dex.DefineAttribute[Input]("quickbooks-invoice-change")
	invoiceChangeOutcomeAttribute = dex.DefineAttribute[ChangeOutcome]("quickbooks-invoice-change-outcome")
)

// Input is one invoiceChanged delivery.
type Input struct {
	// TriggerEventID is the stable Trigger event ID that also names the Flow.
	TriggerEventID string `json:"triggerEventId"`
	// Change is the delivered invoice change.
	Change quickbooks.InvoiceChangedEvent `json:"change"`
}

// ChangeOutcome is the Flow result and the value of its outcome Attribute.
type ChangeOutcome struct {
	// RealmID is the QuickBooks company.
	RealmID string `json:"realmId"`
	// InvoiceID is the changed invoice.
	InvoiceID string `json:"invoiceId"`
	// ChangeKind is the delivered change kind.
	ChangeKind quickbooks.InvoiceChangeKind `json:"changeKind"`
	// IsPaid reports a zero Balance on the invoice read back.
	IsPaid bool `json:"isPaid"`
	// Current is the invoice read back from QuickBooks.
	Current quickbooks.Invoice `json:"current"`
}

// Flow records one QuickBooks invoice change.
type Flow struct {
	dex.FlowDefaults
	connection quickbooks.Connection
}

// NewFlow binds the QuickBooks Connection at registration time.
func NewFlow(connection quickbooks.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and QuickBooks connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordInvoiceChange{}),
		dex.DefineStep(quickbooks.NewGetInvoiceStep(quickbooks.GetInvoiceStepConfig[Input]{
			StepType: readChangedInvoiceStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "change", GroupLabel: "Change",
				Explanation: "Read the changed invoice back, so the record holds its current balance rather than the polled copy.",
			},
			Connection: flow.connection, MapToOperationInput: MapToGetInvoiceInput,
			Found: sdkgo.GoTo(completeInvoiceChange{}),
		})),
		dex.DefineStep(completeInvoiceChange{}),
	}
}

// GetConnectorTriggerBindings declares the invoiceChanged binding. It watches every invoice of the
// connection's company, so it has no configuration fields.
func (*Flow) GetConnectorTriggerBindings() []sdkgo.TriggerBindingDefinition {
	return []sdkgo.TriggerBindingDefinition{
		quickbooks.DefineInvoiceChangedTriggerBinding(quickbooks.InvoiceChangedTriggerBindingConfig{
			ConnectionName: ConnectionName, BindingName: InvoiceChangedTriggerBinding,
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

// GetDexSummary returns the change and its outcome.
//
// dex:field attribute-key:quickbooks-invoice-change value-type:json editable:false description:"Delivered invoice change"
// dex:field attribute-key:quickbooks-invoice-change-outcome value-type:json editable:false description:"Recorded change"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	change, outcome, err := invoiceChangeInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"quickbooks-invoice-change":         change,
		"quickbooks-invoice-change-outcome": outcome,
	}}, nil
}

// GetDexDisplay returns the delivered change and the invoice read back from QuickBooks.
//
// dex:field attribute-key:quickbooks-invoice-change value-type:json editable:false description:"Company, change kind, and invoice from the poll"
// dex:field attribute-key:quickbooks-invoice-change-outcome value-type:json editable:false description:"Current invoice read back from QuickBooks"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	change, outcome, err := invoiceChangeInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"quickbooks-invoice-change":         change,
		"quickbooks-invoice-change-outcome": outcome,
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

// ResolveFlowID names one Flow per invoice version; a redelivered change finds its existing Flow.
func ResolveFlowID(event sdkgo.TriggerEvent[quickbooks.InvoiceChangedEvent]) string {
	return "quickbooks-invoice-change-" + event.ID
}

// MapToFlowInput keeps the Trigger event ID beside the delivered change.
func MapToFlowInput(event sdkgo.TriggerEvent[quickbooks.InvoiceChangedEvent]) Input {
	return Input{TriggerEventID: event.ID, Change: event.Payload}
}

// AcceptEveryInvoiceChange admits every change; an application narrows it, for example to one customer.
func AcceptEveryInvoiceChange(sdkgo.TriggerEvent[quickbooks.InvoiceChangedEvent]) bool { return true }

// MapToGetInvoiceInput reads the changed invoice by Id.
func MapToGetInvoiceInput(input Input) quickbooks.GetInvoiceInput {
	return quickbooks.GetInvoiceInput{InvoiceID: input.Change.Invoice.InvoiceID}
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Validate and record the delivered invoice change, then read the invoice back."
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
// dex:explanation text:"Record the invoice read back from QuickBooks and whether it is paid, then complete the Flow."
type completeInvoiceChange struct {
	dex.StepDefaultsNoWaitFor[quickbooks.GetInvoiceResult]
}

func (completeInvoiceChange) GetStepType() string { return completeInvoiceChangeStepType }

func (completeInvoiceChange) Execute(ctx dex.Context, result quickbooks.GetInvoiceResult) (*dex.StepDecision, error) {
	change, err := invoiceChangeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome := ChangeOutcome{
		RealmID: change.Change.RealmID, InvoiceID: change.Change.Invoice.InvoiceID, ChangeKind: change.Change.ChangeKind,
		IsPaid: result.Value.Balance.IsZero(), Current: result.Value,
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
	case strings.TrimSpace(input.Change.RealmID) == "" || strings.TrimSpace(input.Change.Invoice.InvoiceID) == "":
		return errors.New("change.realmId and change.invoice.invoiceId are required")
	}
	switch input.Change.ChangeKind {
	case quickbooks.InvoiceChangeCreated, quickbooks.InvoiceChangeUpdated:
		return nil
	default:
		return errors.New("change.changeKind must be created or updated")
	}
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
