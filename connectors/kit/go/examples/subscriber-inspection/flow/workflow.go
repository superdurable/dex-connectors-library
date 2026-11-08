// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package subscriberinspection runs Kit listSubscribers as one Dex Flow.
package subscriberinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/kit/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "KitSubscriberInspection"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "kit"
	operationStepType = "CallListSubscribers"
)

var subscribersPageAttribute = dex.DefineAttribute[kit.SubscribersPage]("kit-subscribers-page")

// Flow runs ListSubscribers once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection kit.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection kit.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{subscribersPageAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(kit.NewListSubscribersStep(kit.ListSubscribersStepConfig[kit.ListSubscribersInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Kit", Explanation: "Call the provider's listSubscribers operation."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Listed:              sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:kit-subscribers-page value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readSubscriberPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"kit-subscribers-page": outcome}}, nil
}

// dex:field attribute-key:kit-subscribers-page value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readSubscriberPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"kit-subscribers-page": outcome}}, nil
}

func readSubscriberPage(ctx dex.Context) (kit.SubscribersPage, error) {
	outcome, err := subscribersPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return kit.SubscribersPage{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input kit.ListSubscribersInput) kit.ListSubscribersInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListSubscribers" }
func (startRequest) WaitFor(dex.Context, kit.ListSubscribersInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input kit.ListSubscribersInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[kit.ListSubscribersInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[kit.ListSubscribersResult]
}

func (completeResult) GetStepType() string { return "CompleteListSubscribers" }
func (completeResult) Execute(ctx dex.Context, result kit.ListSubscribersResult) (*dex.StepDecision, error) {
	if err := subscribersPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
