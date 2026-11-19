// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package createcontact demonstrates bounded Resend contact creation.
package createcontact

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/resend/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is this example's stable Flow identity.
	FlowType = "ResendCreateContact"
	// ConnectionName is the static Dex Web connection name.
	ConnectionName = "resend"
)

// Request supplies the contact fields to create.
type Request resend.CreateContactInput

var outcomeAttribute = dex.DefineAttribute[resend.ContactReference]("resend-created-contact")

// Flow creates one Resend contact and completes with its ID.
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

// GetPersistenceSchema registers the created contact result.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{outcomeAttribute}}
}

// GetDexSummary returns the created contact ID.
// dex:field attribute-key:resend-created-contact value-type:json editable:false description:"Created contact ID"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readContactOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"resend-created-contact": value}}, nil
}

// GetDexDisplay returns the created contact ID.
// dex:field attribute-key:resend-created-contact value-type:json editable:false description:"Created contact ID"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readContactOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"resend-created-contact": value}}, nil
}

func readContactOutcome(ctx dex.Context) (resend.ContactReference, error) {
	value, err := outcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return resend.ContactReference{}, nil
	}
	return value, err
}

// GetSteps registers the contact create path.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(prepareContact{}),
		dex.DefineStep(resend.NewCreateContactStep(resend.CreateContactStepConfig[Request]{
			StepType: "CreateContact", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "resend-contact", GroupLabel: "Resend contact", Explanation: "Create the contact in Resend under its email address."},
			MapToOperationInput: func(request Request) resend.CreateContactInput { return resend.CreateContactInput(request) },
			Created:             sdkgo.GoTo(completeContact{}),
		})),
		dex.DefineStep(completeContact{}),
	}
}

// dex:group group-id:resend-contact-prepare group-label:"Prepare contact"
// dex:explanation text:"Pass the typed contact request into the provider create."
type prepareContact struct{ dex.StepDefaults }

func (prepareContact) GetStepType() string { return "PrepareContact" }
func (prepareContact) WaitFor(dex.Context, Request) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (prepareContact) Execute(_ dex.Context, request Request) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[Request]("CreateContact"), request), nil
}

// dex:group group-id:resend-contact-complete group-label:"Complete contact create"
// dex:explanation text:"Complete with Resend's created contact identifier."
type completeContact struct {
	dex.StepDefaultsNoWaitFor[resend.CreateContactResult]
}

func (completeContact) GetStepType() string { return "CompleteContact" }
func (completeContact) Execute(ctx dex.Context, result resend.CreateContactResult) (*dex.StepDecision, error) {
	if err := outcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
