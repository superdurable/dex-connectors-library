// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package createautomation demonstrates Resend draft automation creation.
package createautomation

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/resend/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is this example's stable Flow identity.
	FlowType = "ResendCreateAutomation"
	// ConnectionName is the static Dex Web connection name.
	ConnectionName = "resend"
)

// Request supplies the new automation settings.
type Request resend.CreateAutomationInput

var outcomeAttribute = dex.DefineAttribute[resend.AutomationReference]("resend-automation-created")

// Flow creates one Resend draft automation and completes with its ID.
type Flow struct {
	dex.FlowDefaults
	connection resend.Connection
}

// NewFlow binds a Resend connection.
func NewFlow(connection resend.Connection) *Flow { return &Flow{connection: connection} }

// GetFlowType returns the stable Flow identity.
func (*Flow) GetFlowType() string { return FlowType }

// GetRPCs exposes Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetPersistenceSchema registers the verification result.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{outcomeAttribute}}
}

// GetDexSummary returns the created automation ID.
// dex:field attribute-key:resend-automation-created value-type:json editable:false description:"Created automation"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readAutomationOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"resend-automation-created": value}}, nil
}

// GetDexDisplay returns the created automation ID.
// dex:field attribute-key:resend-automation-created value-type:json editable:false description:"Created automation"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readAutomationOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"resend-automation-created": value}}, nil
}

func readAutomationOutcome(ctx dex.Context) (resend.AutomationReference, error) {
	value, err := outcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return resend.AutomationReference{}, nil
	}
	return value, err
}

// GetSteps registers the automation creation path.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(prepareAutomation{}),
		dex.DefineStep(resend.NewCreateAutomationStep(resend.CreateAutomationStepConfig[Request]{
			StepType: "CreateAutomation", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "resend-automation", GroupLabel: "Resend automation", Explanation: "Create the selected Resend draft automation."},
			MapToOperationInput: func(request Request) resend.CreateAutomationInput { return resend.CreateAutomationInput(request) },
			Created:             sdkgo.GoTo(completeAutomation{}),
		})),
		dex.DefineStep(completeAutomation{}),
	}
}

// dex:group group-id:resend-automation-prepare group-label:"Prepare automation"
// dex:explanation text:"Pass the typed automation request into the provider create."
type prepareAutomation struct{ dex.StepDefaults }

func (prepareAutomation) GetStepType() string { return "PrepareAutomation" }
func (prepareAutomation) WaitFor(dex.Context, Request) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (prepareAutomation) Execute(_ dex.Context, request Request) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[Request]("CreateAutomation"), request), nil
}

// dex:group group-id:resend-automation-complete group-label:"Complete automation creation"
// dex:explanation text:"Complete after Resend creates the automation."
type completeAutomation struct {
	dex.StepDefaultsNoWaitFor[resend.CreateAutomationResult]
}

func (completeAutomation) GetStepType() string { return "CompleteAutomation" }
func (completeAutomation) Execute(ctx dex.Context, result resend.CreateAutomationResult) (*dex.StepDecision, error) {
	if err := outcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
