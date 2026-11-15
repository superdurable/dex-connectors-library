// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package crmobjectchanged demonstrates the HubSpot crmObjectChanged Trigger: every contact, company, or
// deal of the picked object type that is created or changed starts one Flow, which reads the record back
// and records its current state.
package crmobjectchanged

import (
	"errors"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/hubspot/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity each record change starts.
	FlowType = "HubSpotCRMObjectChange"
	// ConnectionName is the static Dex Web connection for HubSpot.
	ConnectionName = "hubspot-crm-watcher"
	// CRMObjectChangedTriggerBinding is the stable crmObjectChanged binding that starts this Flow.
	CRMObjectChangedTriggerBinding = "crm-object-changes"

	recordCRMObjectChangeStepType   = "RecordCRMObjectChange"
	readChangedObjectStepType       = "ReadChangedObject"
	completeCRMObjectChangeStepType = "CompleteCRMObjectChange"
)

// DeliveredProperties are the properties each delivery and read-back returns; HubSpot ignores a name the
// object type does not define, so one list serves contacts, companies, and deals.
var DeliveredProperties = []string{"email", "firstname", "lastname", "name", "domain", "dealname", "dealstage", "pipeline", "hubspot_owner_id"}

var (
	crmObjectChangeAttribute        = dex.DefineAttribute[Input]("hubspot-crm-object-change")
	crmObjectChangeOutcomeAttribute = dex.DefineAttribute[ChangeOutcome]("hubspot-crm-object-change-outcome")
)

// Input is one crmObjectChanged delivery.
type Input struct {
	// TriggerEventID is the stable Trigger event ID that also names the Flow.
	TriggerEventID string `json:"triggerEventId"`
	// Change is the delivered change.
	Change hubspot.CRMObjectChangedEvent `json:"change"`
}

// ChangeOutcome is the Flow result and the value of its outcome Attribute.
type ChangeOutcome struct {
	// ObjectType is the watched object type.
	ObjectType hubspot.ObjectType `json:"objectType"`
	// ObjectID is the changed record.
	ObjectID string `json:"objectId"`
	// ChangeKind is the delivered change kind.
	ChangeKind hubspot.CRMObjectChangeKind `json:"changeKind"`
	// Current is the record read back from HubSpot.
	Current hubspot.CRMObject `json:"current"`
}

// Flow records one HubSpot CRM record change.
type Flow struct {
	dex.FlowDefaults
	connection hubspot.Connection
}

// NewFlow binds the HubSpot Connection at registration time.
func NewFlow(connection hubspot.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and HubSpot connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordCRMObjectChange{}),
		dex.DefineStep(hubspot.NewGetObjectStep(hubspot.GetObjectStepConfig[Input]{
			StepType: readChangedObjectStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "change", GroupLabel: "Change",
				Explanation: "Read the changed record back, so the outcome holds its current values rather than the search snapshot.",
			},
			Connection: flow.connection, MapToOperationInput: MapToGetObjectInput,
			Found: sdkgo.GoTo(completeCRMObjectChange{}),
		})),
		dex.DefineStep(completeCRMObjectChange{}),
	}
}

// GetConnectorTriggerBindings declares the crmObjectChanged binding and its object type picker for Dex Web.
func (*Flow) GetConnectorTriggerBindings() []sdkgo.TriggerBindingDefinition {
	return []sdkgo.TriggerBindingDefinition{
		hubspot.DefineCRMObjectChangedTriggerBinding(hubspot.CRMObjectChangedTriggerBindingConfig{
			ConnectionName: ConnectionName, BindingName: CRMObjectChangedTriggerBinding,
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
				ID: "watchedObjectType", UnitID: hubspot.UIUnitObjectTypePicker, Label: "Watched object type", Required: true,
				Description: "Choose whether created and changed contacts, companies, or deals start this Flow; the unit saves HubSpot's object name. The connection's private app or OAuth app needs the matching crm.objects.<type>.read scope, which the HubSpot authorization guide already lists. It cannot be blank. The first start records existing records as a baseline and starts no Flow for them. Restart the Worker after saving; a changed object type records a new baseline.",
				Bindings:    []sdkgo.ConnectorUIBinding{{Port: hubspot.UIObjectTypePickerPortObjectType, JSONPointer: "/objectType"}},
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
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{crmObjectChangeAttribute, crmObjectChangeOutcomeAttribute}}
}

// GetDexSummary returns the change and its outcome.
//
// dex:field attribute-key:hubspot-crm-object-change value-type:json editable:false description:"Delivered CRM record change"
// dex:field attribute-key:hubspot-crm-object-change-outcome value-type:json editable:false description:"Recorded change"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	change, outcome, err := crmObjectChangeInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"hubspot-crm-object-change":         change,
		"hubspot-crm-object-change-outcome": outcome,
	}}, nil
}

// GetDexDisplay returns the delivered change and the record read back from HubSpot.
//
// dex:field attribute-key:hubspot-crm-object-change value-type:json editable:false description:"Object type, change kind, and record from the search page"
// dex:field attribute-key:hubspot-crm-object-change-outcome value-type:json editable:false description:"Current record read back from HubSpot"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	change, outcome, err := crmObjectChangeInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"hubspot-crm-object-change":         change,
		"hubspot-crm-object-change-outcome": outcome,
	}}, nil
}

func crmObjectChangeInspection(ctx dex.Context) (Input, ChangeOutcome, error) {
	change, err := optionalAttribute(ctx, crmObjectChangeAttribute)
	if err != nil {
		return Input{}, ChangeOutcome{}, err
	}
	outcome, err := optionalAttribute(ctx, crmObjectChangeOutcomeAttribute)
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

// ResolveFlowID names one Flow per record version; a redelivered change finds its existing Flow.
func ResolveFlowID(event sdkgo.TriggerEvent[hubspot.CRMObjectChangedEvent]) string {
	return "hubspot-crm-object-change-" + event.ID
}

// MapToFlowInput keeps the Trigger event ID beside the delivered change.
func MapToFlowInput(event sdkgo.TriggerEvent[hubspot.CRMObjectChangedEvent]) Input {
	return Input{TriggerEventID: event.ID, Change: event.Payload}
}

// AcceptEveryCRMObjectChange admits every change; an application narrows it, for example to created records.
func AcceptEveryCRMObjectChange(sdkgo.TriggerEvent[hubspot.CRMObjectChangedEvent]) bool { return true }

// MapToGetObjectInput reads the changed record with the delivered properties.
func MapToGetObjectInput(input Input) hubspot.GetObjectInput {
	return hubspot.GetObjectInput{ObjectType: input.Change.ObjectType, ObjectID: input.Change.Object.ID, Properties: DeliveredProperties}
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Validate and record the delivered record change, then read the record back."
type recordCRMObjectChange struct {
	dex.StepDefaults
}

func (recordCRMObjectChange) GetStepType() string { return recordCRMObjectChangeStepType }

// WaitFor skips immediately because the Trigger starts the Flow at this Step.
func (recordCRMObjectChange) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordCRMObjectChange) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	if err := validateInput(input); err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	if err := crmObjectChangeAttribute.Set(ctx, input); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[Input](readChangedObjectStepType), input), nil
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the record read back from HubSpot as the outcome and complete the Flow."
type completeCRMObjectChange struct {
	dex.StepDefaultsNoWaitFor[hubspot.GetObjectResult]
}

func (completeCRMObjectChange) GetStepType() string { return completeCRMObjectChangeStepType }

func (completeCRMObjectChange) Execute(ctx dex.Context, result hubspot.GetObjectResult) (*dex.StepDecision, error) {
	change, err := crmObjectChangeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome := ChangeOutcome{
		ObjectType: change.Change.ObjectType, ObjectID: change.Change.Object.ID, ChangeKind: change.Change.ChangeKind, Current: result.Value,
	}
	if err := crmObjectChangeOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

func validateInput(input Input) error {
	switch {
	case strings.TrimSpace(input.TriggerEventID) == "":
		return errors.New("triggerEventId is required")
	case input.Change.ObjectType == "" || strings.TrimSpace(input.Change.Object.ID) == "":
		return errors.New("change.objectType and change.object.id are required")
	}
	switch input.Change.ChangeKind {
	case hubspot.CRMObjectChangeCreated, hubspot.CRMObjectChangeUpdated:
		return nil
	default:
		return errors.New("change.changeKind must be created or updated")
	}
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
