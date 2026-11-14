// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package quotalist runs Zhihu listQuotas as one Dex Flow.
package quotalist

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/zhihu/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "ZhihuQuotas"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "zhihu"
	operationStepType = "CallListQuotas"
)

var quotaListOutcomeAttribute = dex.DefineAttribute[zhihu.QuotaPage]("zhihu-quota-list-outcome")

// Flow runs ListQuotas once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection zhihu.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection zhihu.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{quotaListOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(zhihu.NewListQuotasStep(zhihu.ListQuotasStepConfig[zhihu.ListQuotasInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Zhihu", Explanation: "List Zhihu API quota balances."},
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

// dex:field attribute-key:zhihu-quota-list-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readQuotasOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"zhihu-quota-list-outcome": outcome}}, nil
}

// dex:field attribute-key:zhihu-quota-list-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readQuotasOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"zhihu-quota-list-outcome": outcome}}, nil
}

func readQuotasOutcome(ctx dex.Context) (zhihu.QuotaPage, error) {
	outcome, err := quotaListOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return zhihu.QuotaPage{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input zhihu.ListQuotasInput) zhihu.ListQuotasInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListQuotas" }
func (startRequest) WaitFor(dex.Context, zhihu.ListQuotasInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input zhihu.ListQuotasInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[zhihu.ListQuotasInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[zhihu.ListQuotasResult]
}

func (completeResult) GetStepType() string { return "CompleteListQuotas" }
func (completeResult) Execute(ctx dex.Context, result zhihu.ListQuotasResult) (*dex.StepDecision, error) {
	if err := quotaListOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
