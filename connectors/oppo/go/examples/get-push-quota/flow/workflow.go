// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package pushquota runs Oppo getPushQuota as one Dex Flow.
package pushquota

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/oppo/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "OppoPushQuota"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "oppo"
	operationStepType = "CallGetPushQuota"
)

var pushQuotaOutcomeAttribute = dex.DefineAttribute[oppo.GetPushQuotaOutput]("oppo-get-push-quota-outcome")

// Flow runs GetPushQuota once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection oppo.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection oppo.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{pushQuotaOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(oppo.NewGetPushQuotaStep(oppo.GetPushQuotaStepConfig[oppo.GetPushQuotaInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Oppo", Explanation: "Read the OPPO Push daily sending quota."},
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

// dex:field attribute-key:oppo-get-push-quota-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readGetPushQuotaOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"oppo-get-push-quota-outcome": outcome}}, nil
}

// dex:field attribute-key:oppo-get-push-quota-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readGetPushQuotaOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"oppo-get-push-quota-outcome": outcome}}, nil
}

func readGetPushQuotaOutcome(ctx dex.Context) (oppo.GetPushQuotaOutput, error) {
	outcome, err := pushQuotaOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return oppo.GetPushQuotaOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input oppo.GetPushQuotaInput) oppo.GetPushQuotaInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetPushQuota" }
func (startRequest) WaitFor(dex.Context, oppo.GetPushQuotaInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input oppo.GetPushQuotaInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[oppo.GetPushQuotaInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[oppo.GetPushQuotaResult]
}

func (completeResult) GetStepType() string { return "CompleteGetPushQuota" }
func (completeResult) Execute(ctx dex.Context, result oppo.GetPushQuotaResult) (*dex.StepDecision, error) {
	if err := pushQuotaOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
