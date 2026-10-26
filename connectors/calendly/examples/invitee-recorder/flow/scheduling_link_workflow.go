// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package inviteerecorder

import (
	"github.com/superdurable/dex-connectors-library/connectors/calendly"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// SchedulingLinkFlowType is the example Flow that creates one single-use link.
const SchedulingLinkFlowType = "CalendlyCreateSchedulingLink"

// SchedulingLinkFlow creates a single-use link with a fenced Calendly Mutation.
type SchedulingLinkFlow struct {
	dex.FlowDefaults
	connection calendly.Connection
}

// NewSchedulingLinkFlow binds the configured Calendly connection.
func NewSchedulingLinkFlow(connection calendly.Connection) *SchedulingLinkFlow {
	return &SchedulingLinkFlow{connection: connection}
}

// GetFlowType returns the stable example Flow type.
func (*SchedulingLinkFlow) GetFlowType() string { return SchedulingLinkFlowType }

// GetSteps creates the link and completes with its booking URL.
func (flow *SchedulingLinkFlow) GetSteps() []dex.StepDef {
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

// GetPersistenceSchema declares no Flow Attributes.
func (*SchedulingLinkFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{}
}

// GetRPCs declares no application RPCs.
func (*SchedulingLinkFlow) GetRPCs() []dex.RPCDef { return nil }

func useSchedulingLinkInput(input calendly.CreateSchedulingLinkInput) calendly.CreateSchedulingLinkInput {
	return input
}

// dex:group group-id:calendly group-label:"Calendly"
// dex:explanation text:"Complete the Flow with the accepted single-use booking link."
type completeSchedulingLink struct {
	dex.StepDefaultsNoWaitFor[calendly.CreateSchedulingLinkResult]
}

func (completeSchedulingLink) GetStepType() string { return "CompleteSchedulingLink" }

func (completeSchedulingLink) Execute(_ dex.Context, result calendly.CreateSchedulingLinkResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*SchedulingLinkFlow)(nil)
