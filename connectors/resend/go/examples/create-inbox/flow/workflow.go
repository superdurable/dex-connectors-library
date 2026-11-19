// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package createinbox demonstrates Resend draft inbox creation.
package createinbox

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/resend/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is this example's stable Flow identity.
	FlowType = "ResendCreateInbox"
	// ConnectionName is the static Dex Web connection name.
	ConnectionName = "resend"
)

// Request supplies the new inbox settings.
type Request resend.CreateInboxInput

var outcomeAttribute = dex.DefineAttribute[resend.InboxReference]("resend-inbox-created")

// Flow creates one Resend draft inbox and completes with its ID.
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

// GetDexSummary returns the created inbox ID.
// dex:field attribute-key:resend-inbox-created value-type:json editable:false description:"Created inbox"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readInboxOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"resend-inbox-created": value}}, nil
}

// GetDexDisplay returns the created inbox ID.
// dex:field attribute-key:resend-inbox-created value-type:json editable:false description:"Created inbox"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readInboxOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"resend-inbox-created": value}}, nil
}

func readInboxOutcome(ctx dex.Context) (resend.InboxReference, error) {
	value, err := outcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return resend.InboxReference{}, nil
	}
	return value, err
}

// GetSteps registers the inbox creation path.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(prepareInbox{}),
		dex.DefineStep(resend.NewCreateInboxStep(resend.CreateInboxStepConfig[Request]{
			StepType: "CreateInbox", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "resend-inbox", GroupLabel: "Resend inbox", Explanation: "Create the selected Resend draft inbox."},
			MapToOperationInput: func(request Request) resend.CreateInboxInput { return resend.CreateInboxInput(request) },
			Created:             sdkgo.GoTo(completeInbox{}),
		})),
		dex.DefineStep(completeInbox{}),
	}
}

// dex:group group-id:resend-inbox-prepare group-label:"Prepare inbox"
// dex:explanation text:"Pass the typed inbox request into the provider create."
type prepareInbox struct{ dex.StepDefaults }

func (prepareInbox) GetStepType() string { return "PrepareInbox" }
func (prepareInbox) WaitFor(dex.Context, Request) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (prepareInbox) Execute(_ dex.Context, request Request) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[Request]("CreateInbox"), request), nil
}

// dex:group group-id:resend-inbox-complete group-label:"Complete inbox creation"
// dex:explanation text:"Complete after Resend creates the inbox."
type completeInbox struct {
	dex.StepDefaultsNoWaitFor[resend.CreateInboxResult]
}

func (completeInbox) GetStepType() string { return "CompleteInbox" }
func (completeInbox) Execute(ctx dex.Context, result resend.CreateInboxResult) (*dex.StepDecision, error) {
	if err := outcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
