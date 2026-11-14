// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package accountstatus runs Istero getAccountStatus as one Dex Flow.
package accountstatus

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/istero/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "IsteroAccountStatus"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "istero"
	operationStepType = "CallGetAccountStatus"
)

var accountStatusOutcomeAttribute = dex.DefineAttribute[istero.GetAccountStatusOutput]("istero-account-status-outcome")

// Flow runs GetAccountStatus once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection istero.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection istero.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{accountStatusOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(istero.NewGetAccountStatusStep(istero.GetAccountStatusStepConfig[istero.GetAccountStatusInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "ISAS Data", Explanation: "Read the free account endpoint for daily API call counters."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Retrieved:           sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:istero-account-status-outcome value-type:json editable:false description:"Account catalog"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readAccountStatusOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"istero-account-status-outcome": outcome}}, nil
}

// dex:field attribute-key:istero-account-status-outcome value-type:json editable:false description:"Account catalog"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readAccountStatusOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"istero-account-status-outcome": outcome}}, nil
}

func readAccountStatusOutcome(ctx dex.Context) (istero.GetAccountStatusOutput, error) {
	outcome, err := accountStatusOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return istero.GetAccountStatusOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input istero.GetAccountStatusInput) istero.GetAccountStatusInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetAccountStatus" }
func (startRequest) WaitFor(dex.Context, istero.GetAccountStatusInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input istero.GetAccountStatusInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[istero.GetAccountStatusInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[istero.GetAccountStatusResult]
}

func (completeResult) GetStepType() string { return "CompleteGetAccountStatus" }
func (completeResult) Execute(ctx dex.Context, result istero.GetAccountStatusResult) (*dex.StepDecision, error) {
	if err := accountStatusOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
