// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package addcontacttosegment demonstrates Resend contact segment membership.
package addcontacttosegment

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/resend/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is this example's stable Flow identity.
	FlowType = "ResendAddContactToSegment"
	// ConnectionName is the static Dex Web connection name.
	ConnectionName = "resend"
)

// Request selects the contact and segment.
type Request resend.ContactSegmentInput

var outcomeAttribute = dex.DefineAttribute[resend.ContactSegmentReference]("resend-contact-segment-added")

// Flow adds one contact to a Resend segment.
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

// GetDexSummary returns the contact and segment IDs.
// dex:field attribute-key:resend-contact-segment-added value-type:json editable:false description:"Added contact segment"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readContactToSegmentOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"resend-contact-segment-added": value}}, nil
}

// GetDexDisplay returns the contact and segment IDs.
// dex:field attribute-key:resend-contact-segment-added value-type:json editable:false description:"Added contact segment"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readContactToSegmentOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"resend-contact-segment-added": value}}, nil
}

func readContactToSegmentOutcome(ctx dex.Context) (resend.ContactSegmentReference, error) {
	value, err := outcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return resend.ContactSegmentReference{}, nil
	}
	return value, err
}

// GetSteps registers the membership addition path.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(prepareContactToSegment{}),
		dex.DefineStep(resend.NewAddContactToSegmentStep(resend.AddContactToSegmentStepConfig[Request]{
			StepType: "AddContactToSegment", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "resend-contact-segment", GroupLabel: "Resend contact segment", Explanation: "Add the selected contact to the selected Resend segment."},
			MapToOperationInput: func(request Request) resend.ContactSegmentInput { return resend.ContactSegmentInput(request) },
			Added:             sdkgo.GoTo(completeContactToSegment{}),
		})),
		dex.DefineStep(completeContactToSegment{}),
	}
}

// dex:group group-id:resend-contact-segment-prepare group-label:"Prepare membership"
// dex:explanation text:"Pass the contact and segment IDs into the provider write."
type prepareContactToSegment struct{ dex.StepDefaults }

func (prepareContactToSegment) GetStepType() string { return "PrepareContactToSegment" }
func (prepareContactToSegment) WaitFor(dex.Context, Request) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (prepareContactToSegment) Execute(_ dex.Context, request Request) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[Request]("AddContactToSegment"), request), nil
}

// dex:group group-id:resend-contact-segment-complete group-label:"Complete membership"
// dex:explanation text:"Complete after Resend confirms the segment membership."
type completeContactToSegment struct {
	dex.StepDefaultsNoWaitFor[resend.AddContactToSegmentResult]
}

func (completeContactToSegment) GetStepType() string { return "CompleteContactToSegment" }
func (completeContactToSegment) Execute(ctx dex.Context, result resend.AddContactToSegmentResult) (*dex.StepDecision, error) {
	if err := outcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
