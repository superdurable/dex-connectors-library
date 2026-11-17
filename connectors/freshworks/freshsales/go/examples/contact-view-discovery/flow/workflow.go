// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package contactviewdiscovery runs Freshsales listContactViews as one Dex Flow.
package contactviewdiscovery

import (
	"errors"
	"strconv"

	"github.com/superdurable/dex-connectors-library/connectors/freshworks/freshsales/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "FreshsalesContactViewDiscovery"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName       = "freshsales"
	operationStepType    = "CallListContactViews"
	contactsStepType     = "CallListContacts"
	accountViewsStepType = "CallListAccountViews"
	dealViewsStepType    = "CallListDealViews"
	appointmentsStepType = "CallListAppointments"
)

var contactViewsAttribute = dex.DefineAttribute[freshsales.ContactViews]("freshsales-contact-views")

// Flow runs ListContactViews once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection freshsales.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection freshsales.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{contactViewsAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(freshsales.NewListContactViewsStep(freshsales.ListContactViewsStepConfig[freshsales.ListContactViewsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Freshsales", Explanation: "Call the provider's listContactViews operation."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Listed:              sdkgo.GoTo(afterViews{}),
		})),
		dex.DefineStep(afterViews{}),
		dex.DefineStep(freshsales.NewListContactsStep(freshsales.ListContactsStepConfig[freshsales.ListContactsInput]{
			StepType: contactsStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Freshsales", Explanation: "Read the first page of the selected contact view."},
			Connection:  flow.connection, MapToOperationInput: MapToContactsInput,
			Listed: sdkgo.GoTo(afterContacts{}),
		})),
		dex.DefineStep(afterContacts{}),
		dex.DefineStep(freshsales.NewListAccountViewsStep(freshsales.ListAccountViewsStepConfig[freshsales.ListAccountViewsInput]{
			StepType: accountViewsStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Freshsales", Explanation: "Discover available sales account views."},
			Connection:  flow.connection, MapToOperationInput: MapToAccountViewsInput,
			Listed: sdkgo.GoTo(afterAccountViews{}),
		})),
		dex.DefineStep(afterAccountViews{}),
		dex.DefineStep(freshsales.NewListDealViewsStep(freshsales.ListDealViewsStepConfig[freshsales.ListDealViewsInput]{
			StepType: dealViewsStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Freshsales", Explanation: "Discover available deal views."},
			Connection:  flow.connection, MapToOperationInput: MapToDealViewsInput,
			Listed: sdkgo.GoTo(afterDealViews{}),
		})),
		dex.DefineStep(afterDealViews{}),
		dex.DefineStep(freshsales.NewListAppointmentsStep(freshsales.ListAppointmentsStepConfig[freshsales.ListAppointmentsInput]{
			StepType: appointmentsStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Freshsales", Explanation: "List upcoming appointments using the required open filter."},
			Connection:  flow.connection, MapToOperationInput: MapToAppointmentsInput,
			Listed: sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:freshsales-contact-views value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readContactViews(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"freshsales-contact-views": outcome}}, nil
}

// dex:field attribute-key:freshsales-contact-views value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readContactViews(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"freshsales-contact-views": outcome}}, nil
}

func readContactViews(ctx dex.Context) (freshsales.ContactViews, error) {
	outcome, err := contactViewsAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return freshsales.ContactViews{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input freshsales.ListContactViewsInput) freshsales.ListContactViewsInput {
	return input
}

// MapToContactsInput preserves the view and page selected by the Flow.
func MapToContactsInput(input freshsales.ListContactsInput) freshsales.ListContactsInput {
	return input
}

// MapToAccountViewsInput preserves the empty account view query input.
func MapToAccountViewsInput(input freshsales.ListAccountViewsInput) freshsales.ListAccountViewsInput {
	return input
}

// MapToDealViewsInput preserves the empty deal view query input.
func MapToDealViewsInput(input freshsales.ListDealViewsInput) freshsales.ListDealViewsInput {
	return input
}

// MapToAppointmentsInput preserves the required appointment filter and page.
func MapToAppointmentsInput(input freshsales.ListAppointmentsInput) freshsales.ListAppointmentsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListContactViews" }
func (startRequest) WaitFor(dex.Context, freshsales.ListContactViewsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input freshsales.ListContactViewsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[freshsales.ListContactViewsInput](operationStepType), input), nil
}

// dex:group group-id:provider group-label:"Freshsales"
// dex:explanation text:"Use the first available contact view to list contacts."
type afterViews struct {
	dex.StepDefaultsNoWaitFor[freshsales.ListContactViewsResult]
}

func (afterViews) GetStepType() string { return "AfterContactViews" }
func (afterViews) Execute(ctx dex.Context, result freshsales.ListContactViewsResult) (*dex.StepDecision, error) {
	if err := contactViewsAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	if len(result.Value.Views) == 0 {
		return dex.GracefulComplete(result.Value), nil
	}
	return dex.GoTo(sdkgo.StepRef[freshsales.ListContactsInput](contactsStepType), freshsales.ListContactsInput{ViewID: strconv.FormatInt(result.Value.Views[0].ID, 10), Page: 1}), nil
}

// dex:group group-id:provider group-label:"Freshsales"
// dex:explanation text:"Discover sales account views after listing contacts."
type afterContacts struct {
	dex.StepDefaultsNoWaitFor[freshsales.ListContactsResult]
}

func (afterContacts) GetStepType() string { return "AfterContacts" }
func (afterContacts) Execute(_ dex.Context, _ freshsales.ListContactsResult) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[freshsales.ListAccountViewsInput](accountViewsStepType), freshsales.ListAccountViewsInput{}), nil
}

// dex:group group-id:provider group-label:"Freshsales"
// dex:explanation text:"Discover deal views after listing account views."
type afterAccountViews struct {
	dex.StepDefaultsNoWaitFor[freshsales.ListAccountViewsResult]
}

func (afterAccountViews) GetStepType() string { return "AfterAccountViews" }
func (afterAccountViews) Execute(_ dex.Context, _ freshsales.ListAccountViewsResult) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[freshsales.ListDealViewsInput](dealViewsStepType), freshsales.ListDealViewsInput{}), nil
}

// dex:group group-id:provider group-label:"Freshsales"
// dex:explanation text:"List upcoming appointments after discovering deal views."
type afterDealViews struct {
	dex.StepDefaultsNoWaitFor[freshsales.ListDealViewsResult]
}

func (afterDealViews) GetStepType() string { return "AfterDealViews" }
func (afterDealViews) Execute(_ dex.Context, _ freshsales.ListDealViewsResult) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[freshsales.ListAppointmentsInput](appointmentsStepType), freshsales.ListAppointmentsInput{Filter: "open", Page: 1}), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[freshsales.ListAppointmentsResult]
}

func (completeResult) GetStepType() string { return "CompleteListContactViews" }
func (completeResult) Execute(ctx dex.Context, _ freshsales.ListAppointmentsResult) (*dex.StepDecision, error) {
	views, err := contactViewsAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return dex.GracefulComplete(views), nil
}

var _ dex.Flow = (*Flow)(nil)
