// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package accountquota runs ZoomEye getAccountQuota as one Dex Flow.
package accountquota

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/knownsec/zoomeye/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "ZoomEyeAccountQuota"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "zoomeye"
	operationStepType = "CallGetAccountQuota"
)

var accountQuotaOutcomeAttribute = dex.DefineAttribute[zoomeye.GetAccountQuotaOutput]("zoomeye-account-quota-outcome")

// Flow runs GetAccountQuota once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection zoomeye.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection zoomeye.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{accountQuotaOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(zoomeye.NewGetAccountQuotaStep(zoomeye.GetAccountQuotaStepConfig[zoomeye.GetAccountQuotaInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "ZoomEye", Explanation: "Read subscription and remaining points for the connected ZoomEye account."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Fetched:             sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:zoomeye-account-quota-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readAccountQuotaOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"zoomeye-account-quota-outcome": outcome}}, nil
}

// dex:field attribute-key:zoomeye-account-quota-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readAccountQuotaOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"zoomeye-account-quota-outcome": outcome}}, nil
}

func readAccountQuotaOutcome(ctx dex.Context) (zoomeye.GetAccountQuotaOutput, error) {
	outcome, err := accountQuotaOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return zoomeye.GetAccountQuotaOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input zoomeye.GetAccountQuotaInput) zoomeye.GetAccountQuotaInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetAccountQuota" }
func (startRequest) WaitFor(dex.Context, zoomeye.GetAccountQuotaInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input zoomeye.GetAccountQuotaInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[zoomeye.GetAccountQuotaInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[zoomeye.GetAccountQuotaResult]
}

func (completeResult) GetStepType() string { return "CompleteGetAccountQuota" }
func (completeResult) Execute(ctx dex.Context, result zoomeye.GetAccountQuotaResult) (*dex.StepDecision, error) {
	if err := accountQuotaOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
