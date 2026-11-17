// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package customerlookup

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/customerio/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// segmentRemovalFlowType is the stable example Flow identity.
	segmentRemovalFlowType = "CustomerioSegmentRemoval"
	segmentRemovalStepType = "CallRemoveCustomersFromSegment"
)

var segmentRemovalAttribute = dex.DefineAttribute[customerio.SegmentMembershipOutput]("customerio-segmentremoval")

// SegmentRemovalFlow runs RemoveCustomersFromSegment once and completes with its typed output.
type SegmentRemovalFlow struct {
	dex.FlowDefaults
	connection customerio.Connection
}

// NewSegmentRemovalFlow binds the provider connection at registration time.
func NewSegmentRemovalFlow(connection customerio.Connection) *SegmentRemovalFlow {
	return &SegmentRemovalFlow{connection: connection}
}

func (*SegmentRemovalFlow) GetFlowType() string { return segmentRemovalFlowType }

func (*SegmentRemovalFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{segmentRemovalAttribute}}
}

func (flow *SegmentRemovalFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startSegmentRemovalFlow{}),
		dex.DefineStep(customerio.NewRemoveCustomersFromSegmentStep(customerio.RemoveCustomersFromSegmentStepConfig[customerio.RemoveCustomersFromSegmentInput]{
			StepType: segmentRemovalStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Customer.io", Explanation: "Remove people from a Customer.io manual segment."},
			Connection:          flow.connection,
			MapToOperationInput: mapSegmentRemovalFlowInput,
			Removed:             sdkgo.GoTo(completeSegmentRemovalFlow{}),
		})),
		dex.DefineStep(completeSegmentRemovalFlow{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *SegmentRemovalFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:customerio-segmentremoval value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*SegmentRemovalFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readSegmentRemoval(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"customerio-segmentremoval": outcome}}, nil
}

// dex:field attribute-key:customerio-segmentremoval value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*SegmentRemovalFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readSegmentRemoval(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"customerio-segmentremoval": outcome}}, nil
}

func readSegmentRemoval(ctx dex.Context) (customerio.SegmentMembershipOutput, error) {
	outcome, err := segmentRemovalAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return customerio.SegmentMembershipOutput{}, nil
	}
	return outcome, err
}

// mapSegmentRemovalFlowInput preserves the validated typed start input for the provider call.
func mapSegmentRemovalFlowInput(input customerio.RemoveCustomersFromSegmentInput) customerio.RemoveCustomersFromSegmentInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startSegmentRemovalFlow struct{ dex.StepDefaults }

func (startSegmentRemovalFlow) GetStepType() string { return "StartRemoveCustomersFromSegment" }
func (startSegmentRemovalFlow) WaitFor(dex.Context, customerio.RemoveCustomersFromSegmentInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startSegmentRemovalFlow) Execute(_ dex.Context, input customerio.RemoveCustomersFromSegmentInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[customerio.RemoveCustomersFromSegmentInput](segmentRemovalStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeSegmentRemovalFlow struct {
	dex.StepDefaultsNoWaitFor[customerio.RemoveCustomersFromSegmentResult]
}

func (completeSegmentRemovalFlow) GetStepType() string { return "CompleteRemoveCustomersFromSegment" }
func (completeSegmentRemovalFlow) Execute(ctx dex.Context, result customerio.RemoveCustomersFromSegmentResult) (*dex.StepDecision, error) {
	if err := segmentRemovalAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*SegmentRemovalFlow)(nil)
