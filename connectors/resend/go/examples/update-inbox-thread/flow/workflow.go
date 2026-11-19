// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package updateinboxthread demonstrates a Resend inbox thread update.
package updateinboxthread

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/resend/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is this example's stable Flow identity.
	FlowType = "ResendUpdateInboxThread"
	// ConnectionName is the static Dex Web connection name.
	ConnectionName = "resend"
)

// Request supplies the new inbox thread settings.
type Request resend.UpdateInboxThreadInput

var outcomeAttribute = dex.DefineAttribute[resend.InboxThreadReference]("resend-inbox-thread-updated")

// Flow updates a Resend inbox thread and completes with its ID.
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

// GetDexSummary returns the updated thread ID.
// dex:field attribute-key:resend-inbox-thread-updated value-type:json editable:false description:"Updated inbox thread"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readInboxOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"resend-inbox-thread-updated": value}}, nil
}

// GetDexDisplay returns the updated thread ID.
// dex:field attribute-key:resend-inbox-thread-updated value-type:json editable:false description:"Updated inbox thread"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readInboxOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"resend-inbox-thread-updated": value}}, nil
}

func readInboxOutcome(ctx dex.Context) (resend.InboxThreadReference, error) {
	value, err := outcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return resend.InboxThreadReference{}, nil
	}
	return value, err
}

// GetSteps registers the inbox thread update path.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(prepareInbox{}),
		dex.DefineStep(resend.NewUpdateInboxThreadStep(resend.UpdateInboxThreadStepConfig[Request]{
			StepType: "UpdateInboxThread", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "resend-inbox-thread", GroupLabel: "Resend inbox thread", Explanation: "Update the selected Resend inbox thread."},
			MapToOperationInput: func(request Request) resend.UpdateInboxThreadInput { return resend.UpdateInboxThreadInput(request) },
			Updated:             sdkgo.GoTo(completeInbox{}),
		})),
		dex.DefineStep(completeInbox{}),
	}
}

// dex:group group-id:resend-inbox-thread-prepare group-label:"Prepare inbox thread"
// dex:explanation text:"Pass the typed thread request into the provider update."
type prepareInbox struct{ dex.StepDefaults }

func (prepareInbox) GetStepType() string { return "PrepareInbox" }
func (prepareInbox) WaitFor(dex.Context, Request) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (prepareInbox) Execute(_ dex.Context, request Request) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[Request]("UpdateInboxThread"), request), nil
}

// dex:group group-id:resend-inbox-thread-complete group-label:"Complete inbox thread update"
// dex:explanation text:"Complete after Resend updates the thread."
type completeInbox struct {
	dex.StepDefaultsNoWaitFor[resend.UpdateInboxThreadResult]
}

func (completeInbox) GetStepType() string { return "CompleteInbox" }
func (completeInbox) Execute(ctx dex.Context, result resend.UpdateInboxThreadResult) (*dex.StepDecision, error) {
	if err := outcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
