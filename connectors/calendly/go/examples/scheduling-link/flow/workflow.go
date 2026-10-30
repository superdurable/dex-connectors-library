// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package schedulinglink creates and records one single-use Calendly booking link.
package schedulinglink

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/calendly"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// FlowType is the stable identity of this example Flow.
const FlowType = "CalendlyCreateSchedulingLink"

// ConnectionName is the static Dex Web connection used by the scheduling-link Worker.
const ConnectionName = "calendly-scheduling"

var schedulingLinkAttribute = dex.DefineAttribute[calendly.SchedulingLink]("calendly-scheduling-link")

// Flow creates and records one single-use scheduling link.
type Flow struct {
	dex.FlowDefaults
	connection calendly.Connection
}

// NewFlow binds the configured Calendly connection.
func NewFlow(connection calendly.Connection) *Flow { return &Flow{connection: connection} }

// GetFlowType returns the stable example Flow type.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps creates the link, records it, and completes the Flow.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(calendly.NewCreateSchedulingLinkStep(calendly.CreateSchedulingLinkStepConfig[calendly.CreateSchedulingLinkInput]{
			StepType: "CreateSchedulingLink", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "calendly", GroupLabel: "Calendly", Explanation: "Create one single-use booking link with a durable dispatch fence."},
			MapToOperationInput: useSchedulingLinkInput,
			Created:             sdkgo.GoTo(completeSchedulingLink{}),
		})),
		dex.DefineStep(completeSchedulingLink{}),
	}
}

// GetRPCs registers the Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetPersistenceSchema registers the created link Attribute.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{schedulingLinkAttribute}}
}

// GetDexSummary shows the created link in the Dex Web run list.
//
// dex:field attribute-key:calendly-scheduling-link value-type:json editable:false description:"Created single-use booking link"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	link, err := inspectSchedulingLink(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"calendly-scheduling-link": link}}, nil
}

// GetDexDisplay shows the accepted link in the Dex Web run detail.
//
// dex:field attribute-key:calendly-scheduling-link value-type:json editable:false description:"Booking URL and selected Calendly event type"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	link, err := inspectSchedulingLink(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"calendly-scheduling-link": link}}, nil
}

func useSchedulingLinkInput(input calendly.CreateSchedulingLinkInput) calendly.CreateSchedulingLinkInput {
	return input
}

func inspectSchedulingLink(ctx dex.Context) (calendly.SchedulingLink, error) {
	link, err := schedulingLinkAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return calendly.SchedulingLink{}, nil
	}
	return link, err
}

// dex:group group-id:calendly group-label:"Calendly"
// dex:explanation text:"Record the accepted single-use booking link and complete the Flow."
type completeSchedulingLink struct {
	dex.StepDefaultsNoWaitFor[calendly.CreateSchedulingLinkResult]
}

func (completeSchedulingLink) GetStepType() string { return "CompleteSchedulingLink" }

func (completeSchedulingLink) Execute(ctx dex.Context, result calendly.CreateSchedulingLinkResult) (*dex.StepDecision, error) {
	if err := schedulingLinkAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
