// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package accessclientflow runs Gowu getAccessClient as one Dex Flow.
package accessclientflow

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/gowu/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "GowuAccessClient"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "gowu"
	operationStepType = "CallGetAccessClient"
)

var accessClientOutcomeAttribute = dex.DefineAttribute[gowu.AccessClientResult]("gowu-access-client-outcome")

// Flow runs GetAccessClient once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection gowu.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection gowu.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{accessClientOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(gowu.NewGetAccessClientStep(gowu.GetAccessClientStepConfig[gowu.GetAccessClientInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Gowu", Explanation: "Query the status of a authorized Gowu application."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Found:               sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:gowu-access-client-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readAccessClientResultOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"gowu-access-client-outcome": outcome}}, nil
}

// dex:field attribute-key:gowu-access-client-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readAccessClientResultOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"gowu-access-client-outcome": outcome}}, nil
}

func readAccessClientResultOutcome(ctx dex.Context) (gowu.AccessClientResult, error) {
	outcome, err := accessClientOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return gowu.AccessClientResult{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input gowu.GetAccessClientInput) gowu.GetAccessClientInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetAccessClient" }
func (startRequest) WaitFor(dex.Context, gowu.GetAccessClientInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input gowu.GetAccessClientInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[gowu.GetAccessClientInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[gowu.GetAccessClientResult]
}

func (completeResult) GetStepType() string { return "CompleteGetAccessClient" }
func (completeResult) Execute(ctx dex.Context, result gowu.GetAccessClientResult) (*dex.StepDecision, error) {
	if err := accessClientOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
