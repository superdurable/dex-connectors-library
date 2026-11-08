// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package organizationinspection runs Eventbrite listOrganizations as one Dex Flow.
package organizationinspection

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/eventbrite/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "EventbriteOrganizationInspection"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "eventbrite"
	operationStepType = "CallListOrganizations"
)

var organizationsPageAttribute = dex.DefineAttribute[eventbrite.OrganizationsPage]("eventbrite-organizations-page")

// Flow runs ListOrganizations once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection eventbrite.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection eventbrite.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{organizationsPageAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(eventbrite.NewListOrganizationsStep(eventbrite.ListOrganizationsStepConfig[eventbrite.ListOrganizationsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Eventbrite", Explanation: "Call the provider's listOrganizations operation."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Listed:              sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:eventbrite-organizations-page value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readOrganizationPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"eventbrite-organizations-page": outcome}}, nil
}

// dex:field attribute-key:eventbrite-organizations-page value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readOrganizationPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"eventbrite-organizations-page": outcome}}, nil
}

func readOrganizationPage(ctx dex.Context) (eventbrite.OrganizationsPage, error) {
	outcome, err := organizationsPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return eventbrite.OrganizationsPage{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input eventbrite.ListOrganizationsInput) eventbrite.ListOrganizationsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListOrganizations" }
func (startRequest) WaitFor(dex.Context, eventbrite.ListOrganizationsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input eventbrite.ListOrganizationsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[eventbrite.ListOrganizationsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[eventbrite.ListOrganizationsResult]
}

func (completeResult) GetStepType() string { return "CompleteListOrganizations" }
func (completeResult) Execute(ctx dex.Context, result eventbrite.ListOrganizationsResult) (*dex.StepDecision, error) {
	if err := organizationsPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
