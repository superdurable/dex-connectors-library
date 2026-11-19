// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package createcontactproperty demonstrates Resend contact property creation.
package createcontactproperty

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/resend/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is this example's stable Flow identity.
	FlowType = "ResendCreateContactProperty"
	// ConnectionName is the static Dex Web connection name.
	ConnectionName = "resend"
)

// Request supplies the new property settings.
type Request resend.CreateContactPropertyInput

var outcomeAttribute = dex.DefineAttribute[resend.ContactPropertyReference]("resend-property-created")

// Flow creates one Resend contact property and completes with its ID.
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

// GetDexSummary returns the created property ID.
// dex:field attribute-key:resend-property-created value-type:json editable:false description:"Created property"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readPropertyOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"resend-property-created": value}}, nil
}

// GetDexDisplay returns the created property ID.
// dex:field attribute-key:resend-property-created value-type:json editable:false description:"Created property"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readPropertyOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"resend-property-created": value}}, nil
}

func readPropertyOutcome(ctx dex.Context) (resend.ContactPropertyReference, error) {
	value, err := outcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return resend.ContactPropertyReference{}, nil
	}
	return value, err
}

// GetSteps registers the property creation path.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(prepareProperty{}),
		dex.DefineStep(resend.NewCreateContactPropertyStep(resend.CreateContactPropertyStepConfig[Request]{
			StepType: "CreateContactProperty", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "resend-property", GroupLabel: "Resend property", Explanation: "Create the selected Resend contact property."},
			MapToOperationInput: func(request Request) resend.CreateContactPropertyInput { return resend.CreateContactPropertyInput(request) },
			Created:             sdkgo.GoTo(completeProperty{}),
		})),
		dex.DefineStep(completeProperty{}),
	}
}

// dex:group group-id:resend-property-prepare group-label:"Prepare property"
// dex:explanation text:"Pass the typed property request into the provider create."
type prepareProperty struct{ dex.StepDefaults }

func (prepareProperty) GetStepType() string { return "PrepareProperty" }
func (prepareProperty) WaitFor(dex.Context, Request) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (prepareProperty) Execute(_ dex.Context, request Request) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[Request]("CreateContactProperty"), request), nil
}

// dex:group group-id:resend-property-complete group-label:"Complete property creation"
// dex:explanation text:"Complete after Resend creates the property."
type completeProperty struct {
	dex.StepDefaultsNoWaitFor[resend.CreateContactPropertyResult]
}

func (completeProperty) GetStepType() string { return "CompleteProperty" }
func (completeProperty) Execute(ctx dex.Context, result resend.CreateContactPropertyResult) (*dex.StepDecision, error) {
	if err := outcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
