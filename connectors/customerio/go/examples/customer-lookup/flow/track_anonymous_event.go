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
	// anonymousEventFlowType is the stable example Flow identity.
	anonymousEventFlowType = "CustomerioAnonymousEvent"
	anonymousEventStepType = "CallTrackAnonymousEvent"
)

var anonymousEventAttribute = dex.DefineAttribute[customerio.TrackEventOutput]("customerio-anonymousevent")

// AnonymousEventFlow runs TrackAnonymousEvent once and completes with its typed output.
type AnonymousEventFlow struct {
	dex.FlowDefaults
	connection customerio.Connection
}

// NewAnonymousEventFlow binds the provider connection at registration time.
func NewAnonymousEventFlow(connection customerio.Connection) *AnonymousEventFlow {
	return &AnonymousEventFlow{connection: connection}
}

func (*AnonymousEventFlow) GetFlowType() string { return anonymousEventFlowType }

func (*AnonymousEventFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{anonymousEventAttribute}}
}

func (flow *AnonymousEventFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startAnonymousEventFlow{}),
		dex.DefineStep(customerio.NewTrackAnonymousEventStep(customerio.TrackAnonymousEventStepConfig[customerio.TrackAnonymousEventInput]{
			StepType: anonymousEventStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Customer.io", Explanation: "Track a named event for a anonymous Customer.io visitor."},
			Connection:          flow.connection,
			MapToOperationInput: mapAnonymousEventFlowInput,
			Tracked:             sdkgo.GoTo(completeAnonymousEventFlow{}),
		})),
		dex.DefineStep(completeAnonymousEventFlow{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *AnonymousEventFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:customerio-anonymousevent value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*AnonymousEventFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readAnonymousEvent(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"customerio-anonymousevent": outcome}}, nil
}

// dex:field attribute-key:customerio-anonymousevent value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*AnonymousEventFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readAnonymousEvent(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"customerio-anonymousevent": outcome}}, nil
}

func readAnonymousEvent(ctx dex.Context) (customerio.TrackEventOutput, error) {
	outcome, err := anonymousEventAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return customerio.TrackEventOutput{}, nil
	}
	return outcome, err
}

// mapAnonymousEventFlowInput preserves the validated typed start input for the provider call.
func mapAnonymousEventFlowInput(input customerio.TrackAnonymousEventInput) customerio.TrackAnonymousEventInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startAnonymousEventFlow struct{ dex.StepDefaults }

func (startAnonymousEventFlow) GetStepType() string { return "StartTrackAnonymousEvent" }
func (startAnonymousEventFlow) WaitFor(dex.Context, customerio.TrackAnonymousEventInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startAnonymousEventFlow) Execute(_ dex.Context, input customerio.TrackAnonymousEventInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[customerio.TrackAnonymousEventInput](anonymousEventStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeAnonymousEventFlow struct {
	dex.StepDefaultsNoWaitFor[customerio.TrackAnonymousEventResult]
}

func (completeAnonymousEventFlow) GetStepType() string { return "CompleteTrackAnonymousEvent" }
func (completeAnonymousEventFlow) Execute(ctx dex.Context, result customerio.TrackAnonymousEventResult) (*dex.StepDecision, error) {
	if err := anonymousEventAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*AnonymousEventFlow)(nil)
