// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package personinspection runs Personio getPerson as one Dex Flow.
package personinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/personio/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "PersonioPersonInspection"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "personio"
	operationStepType = "CallGetPerson"
)

var personAttribute = dex.DefineAttribute[personio.PersonProfile]("personio-person")

// Flow runs GetPerson once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection personio.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection personio.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{personAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(personio.NewGetPersonStep(personio.GetPersonStepConfig[personio.GetPersonInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Personio", Explanation: "Read a person's name from Personio."},
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

// dex:field attribute-key:personio-person value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readPerson(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"personio-person": outcome}}, nil
}

// dex:field attribute-key:personio-person value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readPerson(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"personio-person": outcome}}, nil
}

func readPerson(ctx dex.Context) (personio.PersonProfile, error) {
	outcome, err := personAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return personio.PersonProfile{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input personio.GetPersonInput) personio.GetPersonInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetPerson" }
func (startRequest) WaitFor(dex.Context, personio.GetPersonInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input personio.GetPersonInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[personio.GetPersonInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[personio.GetPersonResult]
}

func (completeResult) GetStepType() string { return "CompleteGetPerson" }
func (completeResult) Execute(ctx dex.Context, result personio.GetPersonResult) (*dex.StepDecision, error) {
	if err := personAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
