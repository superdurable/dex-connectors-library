// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package managecontact demonstrates an absolute Resend contact update.
package managecontact

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/resend/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is this example's stable Flow identity.
	FlowType = "ResendManageContact"
	// ConnectionName is the static Dex Web connection name.
	ConnectionName = "resend"
)

// Request supplies the contact fields to replace.
type Request resend.UpdateContactInput

var outcomeAttribute = dex.DefineAttribute[resend.ContactReference]("resend-contact-outcome")

// Flow updates one Resend contact and completes with its ID.
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

// GetPersistenceSchema registers the updated contact result.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{outcomeAttribute}}
}

// GetDexSummary returns the updated contact ID.
// dex:field attribute-key:resend-contact-outcome value-type:json editable:false description:"Updated contact ID"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readContactOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"resend-contact-outcome": value}}, nil
}

// GetDexDisplay returns the updated contact ID.
// dex:field attribute-key:resend-contact-outcome value-type:json editable:false description:"Updated contact ID"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readContactOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"resend-contact-outcome": value}}, nil
}

func readContactOutcome(ctx dex.Context) (resend.ContactReference, error) {
	value, err := outcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return resend.ContactReference{}, nil
	}
	return value, err
}

// GetSteps registers the contact update path.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(prepareContact{}),
		dex.DefineStep(resend.NewUpdateContactStep(resend.UpdateContactStepConfig[Request]{
			StepType: "UpdateContact", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "resend-contact", GroupLabel: "Resend contact", Explanation: "Replace the selected contact fields in Resend."},
			MapToOperationInput: func(request Request) resend.UpdateContactInput { return resend.UpdateContactInput(request) },
			Updated:             sdkgo.GoTo(completeContact{}),
		})),
		dex.DefineStep(completeContact{}),
	}
}

// dex:group group-id:resend-contact-prepare group-label:"Prepare contact"
// dex:explanation text:"Pass the typed contact request into the provider update."
type prepareContact struct{ dex.StepDefaults }

func (prepareContact) GetStepType() string { return "PrepareContact" }
func (prepareContact) WaitFor(dex.Context, Request) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (prepareContact) Execute(_ dex.Context, request Request) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[Request]("UpdateContact"), request), nil
}

// dex:group group-id:resend-contact-complete group-label:"Complete contact update"
// dex:explanation text:"Complete with Resend's updated contact identifier."
type completeContact struct {
	dex.StepDefaultsNoWaitFor[resend.UpdateContactResult]
}

func (completeContact) GetStepType() string { return "CompleteContact" }
func (completeContact) Execute(ctx dex.Context, result resend.UpdateContactResult) (*dex.StepDecision, error) {
	if err := outcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
