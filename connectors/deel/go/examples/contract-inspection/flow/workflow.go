// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package contractinspection runs Deel getContract as one Dex Flow.
package contractinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/deel/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "DeelContractInspection"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "deel"
	operationStepType = "CallGetContract"
)

var contractAttribute = dex.DefineAttribute[deel.ContractStatus]("deel-contract")

// Flow runs GetContract once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection deel.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection deel.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{contractAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(deel.NewGetContractStep(deel.GetContractStepConfig[deel.GetContractInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Deel", Explanation: "Read a contract from Deel."},
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

// dex:field attribute-key:deel-contract value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readContract(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"deel-contract": outcome}}, nil
}

// dex:field attribute-key:deel-contract value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readContract(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"deel-contract": outcome}}, nil
}

func readContract(ctx dex.Context) (deel.ContractStatus, error) {
	outcome, err := contractAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return deel.ContractStatus{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input deel.GetContractInput) deel.GetContractInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetContract" }
func (startRequest) WaitFor(dex.Context, deel.GetContractInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input deel.GetContractInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[deel.GetContractInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[deel.GetContractResult]
}

func (completeResult) GetStepType() string { return "CompleteGetContract" }
func (completeResult) Execute(ctx dex.Context, result deel.GetContractResult) (*dex.StepDecision, error) {
	if err := contractAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
