// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package clientdiscovery

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/campaignmonitor/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// SegmentCreateFlowType is the stable example segment creation identity.
const SegmentCreateFlowType = "CampaignMonitorSegmentCreate"

const createSegmentStepType = "CallCreateSegment"

var createdSegmentAttribute = dex.DefineAttribute[campaignmonitor.SegmentIdentity]("campaignmonitor-created-segment")

// SegmentCreateFlow creates a segment from typed start input.
type SegmentCreateFlow struct {
	dex.FlowDefaults
	connection campaignmonitor.Connection
}

// NewSegmentCreateFlow binds the provider connection at registration time.
func NewSegmentCreateFlow(connection campaignmonitor.Connection) *SegmentCreateFlow {
	return &SegmentCreateFlow{connection: connection}
}

func (*SegmentCreateFlow) GetFlowType() string { return SegmentCreateFlowType }

func (*SegmentCreateFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdSegmentAttribute}}
}

func (flow *SegmentCreateFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startSegmentCreate{}),
		dex.DefineStep(campaignmonitor.NewCreateSegmentStep(campaignmonitor.CreateSegmentStepConfig[campaignmonitor.CreateSegmentInput]{
			StepType: createSegmentStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Campaign Monitor", Explanation: "Create a segment for the selected subscriber list."},
			Connection:  flow.connection, MapToOperationInput: mapCreateSegmentInput,
			Created: sdkgo.GoTo(completeSegmentCreate{}),
		})),
		dex.DefineStep(completeSegmentCreate{}),
	}
}

func (flow *SegmentCreateFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:campaignmonitor-created-segment value-type:json editable:false description:"Created segment"
// GetDexSummary shows the created segment in Dex Web.
func (*SegmentCreateFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	segment, err := readCreatedSegment(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"campaignmonitor-created-segment": segment}}, nil
}

// dex:field attribute-key:campaignmonitor-created-segment value-type:json editable:false description:"Created segment"
// GetDexDisplay shows the created segment in Dex Web.
func (*SegmentCreateFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	segment, err := readCreatedSegment(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"campaignmonitor-created-segment": segment}}, nil
}

func readCreatedSegment(ctx dex.Context) (campaignmonitor.SegmentIdentity, error) {
	segment, err := createdSegmentAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return campaignmonitor.SegmentIdentity{}, nil
	}
	return segment, err
}

func mapCreateSegmentInput(input campaignmonitor.CreateSegmentInput) campaignmonitor.CreateSegmentInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed segment settings to the provider Step."
type startSegmentCreate struct{ dex.StepDefaults }

func (startSegmentCreate) GetStepType() string { return "StartCreateSegment" }
func (startSegmentCreate) WaitFor(dex.Context, campaignmonitor.CreateSegmentInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startSegmentCreate) Execute(_ dex.Context, input campaignmonitor.CreateSegmentInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[campaignmonitor.CreateSegmentInput](createSegmentStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the created segment and complete the Flow."
type completeSegmentCreate struct {
	dex.StepDefaultsNoWaitFor[campaignmonitor.CreateSegmentResult]
}

func (completeSegmentCreate) GetStepType() string { return "CompleteCreateSegment" }
func (completeSegmentCreate) Execute(ctx dex.Context, result campaignmonitor.CreateSegmentResult) (*dex.StepDecision, error) {
	if err := createdSegmentAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*SegmentCreateFlow)(nil)
