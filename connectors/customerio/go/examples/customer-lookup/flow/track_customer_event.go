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
	// customerEventFlowType is the stable example Flow identity.
	customerEventFlowType = "CustomerioCustomerEvent"
	customerEventStepType = "CallTrackCustomerEvent"
)

var customerEventAttribute = dex.DefineAttribute[customerio.TrackEventOutput]("customerio-customerevent")

// CustomerEventFlow runs TrackCustomerEvent once and completes with its typed output.
type CustomerEventFlow struct {
	dex.FlowDefaults
	connection customerio.Connection
}

// NewCustomerEventFlow binds the provider connection at registration time.
func NewCustomerEventFlow(connection customerio.Connection) *CustomerEventFlow {
	return &CustomerEventFlow{connection: connection}
}

func (*CustomerEventFlow) GetFlowType() string { return customerEventFlowType }

func (*CustomerEventFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{customerEventAttribute}}
}

func (flow *CustomerEventFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startCustomerEventFlow{}),
		dex.DefineStep(customerio.NewTrackCustomerEventStep(customerio.TrackCustomerEventStepConfig[customerio.TrackCustomerEventInput]{
			StepType: customerEventStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Customer.io", Explanation: "Track a named event for a Customer.io customer."},
			Connection:          flow.connection,
			MapToOperationInput: mapCustomerEventFlowInput,
			Tracked:             sdkgo.GoTo(completeCustomerEventFlow{}),
		})),
		dex.DefineStep(completeCustomerEventFlow{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *CustomerEventFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:customerio-customerevent value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*CustomerEventFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readCustomerEvent(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"customerio-customerevent": outcome}}, nil
}

// dex:field attribute-key:customerio-customerevent value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*CustomerEventFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readCustomerEvent(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"customerio-customerevent": outcome}}, nil
}

func readCustomerEvent(ctx dex.Context) (customerio.TrackEventOutput, error) {
	outcome, err := customerEventAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return customerio.TrackEventOutput{}, nil
	}
	return outcome, err
}

// mapCustomerEventFlowInput preserves the validated typed start input for the provider call.
func mapCustomerEventFlowInput(input customerio.TrackCustomerEventInput) customerio.TrackCustomerEventInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startCustomerEventFlow struct{ dex.StepDefaults }

func (startCustomerEventFlow) GetStepType() string { return "StartTrackCustomerEvent" }
func (startCustomerEventFlow) WaitFor(dex.Context, customerio.TrackCustomerEventInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startCustomerEventFlow) Execute(_ dex.Context, input customerio.TrackCustomerEventInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[customerio.TrackCustomerEventInput](customerEventStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeCustomerEventFlow struct {
	dex.StepDefaultsNoWaitFor[customerio.TrackCustomerEventResult]
}

func (completeCustomerEventFlow) GetStepType() string { return "CompleteTrackCustomerEvent" }
func (completeCustomerEventFlow) Execute(ctx dex.Context, result customerio.TrackCustomerEventResult) (*dex.StepDecision, error) {
	if err := customerEventAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CustomerEventFlow)(nil)
