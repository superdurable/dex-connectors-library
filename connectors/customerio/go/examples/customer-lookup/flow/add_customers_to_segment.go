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
	// segmentAdditionFlowType is the stable example Flow identity.
	segmentAdditionFlowType = "CustomerioSegmentAddition"
	segmentAdditionStepType = "CallAddCustomersToSegment"
)

var segmentAdditionAttribute = dex.DefineAttribute[customerio.SegmentMembershipOutput]("customerio-segmentaddition")

// SegmentAdditionFlow runs AddCustomersToSegment once and completes with its typed output.
type SegmentAdditionFlow struct {
	dex.FlowDefaults
	connection customerio.Connection
}

// NewSegmentAdditionFlow binds the provider connection at registration time.
func NewSegmentAdditionFlow(connection customerio.Connection) *SegmentAdditionFlow {
	return &SegmentAdditionFlow{connection: connection}
}

func (*SegmentAdditionFlow) GetFlowType() string { return segmentAdditionFlowType }

func (*SegmentAdditionFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{segmentAdditionAttribute}}
}

func (flow *SegmentAdditionFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startSegmentAdditionFlow{}),
		dex.DefineStep(customerio.NewAddCustomersToSegmentStep(customerio.AddCustomersToSegmentStepConfig[customerio.AddCustomersToSegmentInput]{
			StepType: segmentAdditionStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Customer.io", Explanation: "Add people to a Customer.io manual segment."},
			Connection:          flow.connection,
			MapToOperationInput: mapSegmentAdditionFlowInput,
			Added:               sdkgo.GoTo(completeSegmentAdditionFlow{}),
		})),
		dex.DefineStep(completeSegmentAdditionFlow{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *SegmentAdditionFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:customerio-segmentaddition value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*SegmentAdditionFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readSegmentAddition(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"customerio-segmentaddition": outcome}}, nil
}

// dex:field attribute-key:customerio-segmentaddition value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*SegmentAdditionFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readSegmentAddition(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"customerio-segmentaddition": outcome}}, nil
}

func readSegmentAddition(ctx dex.Context) (customerio.SegmentMembershipOutput, error) {
	outcome, err := segmentAdditionAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return customerio.SegmentMembershipOutput{}, nil
	}
	return outcome, err
}

// mapSegmentAdditionFlowInput preserves the validated typed start input for the provider call.
func mapSegmentAdditionFlowInput(input customerio.AddCustomersToSegmentInput) customerio.AddCustomersToSegmentInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startSegmentAdditionFlow struct{ dex.StepDefaults }

func (startSegmentAdditionFlow) GetStepType() string { return "StartAddCustomersToSegment" }
func (startSegmentAdditionFlow) WaitFor(dex.Context, customerio.AddCustomersToSegmentInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startSegmentAdditionFlow) Execute(_ dex.Context, input customerio.AddCustomersToSegmentInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[customerio.AddCustomersToSegmentInput](segmentAdditionStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeSegmentAdditionFlow struct {
	dex.StepDefaultsNoWaitFor[customerio.AddCustomersToSegmentResult]
}

func (completeSegmentAdditionFlow) GetStepType() string { return "CompleteAddCustomersToSegment" }
func (completeSegmentAdditionFlow) Execute(ctx dex.Context, result customerio.AddCustomersToSegmentResult) (*dex.StepDecision, error) {
	if err := segmentAdditionAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*SegmentAdditionFlow)(nil)
