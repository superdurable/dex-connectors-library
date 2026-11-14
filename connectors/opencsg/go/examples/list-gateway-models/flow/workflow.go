// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package modelcatalog runs OpenCSG listGatewayModels as one Dex Flow.
package modelcatalog

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/opencsg/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "OpenCSGModelCatalog"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "opencsg"
	operationStepType = "CallListGatewayModels"
)

var gatewayModelListOutcomeAttribute = dex.DefineAttribute[opencsg.ListGatewayModelsOutput]("opencsg-list-gateway-models-outcome")

// Flow runs ListGatewayModels once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection opencsg.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection opencsg.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{gatewayModelListOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(opencsg.NewListGatewayModelsStep(opencsg.ListGatewayModelsStepConfig[opencsg.ListGatewayModelsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "OpenCSG", Explanation: "List the models visible to this API Key."},
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

// dex:field attribute-key:opencsg-list-gateway-models-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readListGatewayModelsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"opencsg-list-gateway-models-outcome": outcome}}, nil
}

// dex:field attribute-key:opencsg-list-gateway-models-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readListGatewayModelsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"opencsg-list-gateway-models-outcome": outcome}}, nil
}

func readListGatewayModelsOutcome(ctx dex.Context) (opencsg.ListGatewayModelsOutput, error) {
	outcome, err := gatewayModelListOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return opencsg.ListGatewayModelsOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input opencsg.ListGatewayModelsInput) opencsg.ListGatewayModelsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListGatewayModels" }
func (startRequest) WaitFor(dex.Context, opencsg.ListGatewayModelsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input opencsg.ListGatewayModelsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[opencsg.ListGatewayModelsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[opencsg.ListGatewayModelsResult]
}

func (completeResult) GetStepType() string { return "CompleteListGatewayModels" }
func (completeResult) Execute(ctx dex.Context, result opencsg.ListGatewayModelsResult) (*dex.StepDecision, error) {
	if err := gatewayModelListOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
