// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package appspage runs Jiandaoyun listAppsPage as one Dex Flow.
package appspage

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/fanruan/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "JiandaoyunAppsPage"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "fanruan"
	operationStepType = "CallListAppsPage"
)

var appOutcomeAttribute = dex.DefineAttribute[fanruan.ListAppsPageOutput]("fanruan-list-apps-page-outcome")

// Flow runs ListAppsPage once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection fanruan.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection fanruan.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{appOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(fanruan.NewListAppsPageStep(fanruan.ListAppsPageStepConfig[fanruan.ListAppsPageInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Jiandaoyun", Explanation: "Call the provider's listAppsPage operation."},
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

// dex:field attribute-key:fanruan-list-apps-page-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readListAppsPageOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"fanruan-list-apps-page-outcome": outcome}}, nil
}

// dex:field attribute-key:fanruan-list-apps-page-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readListAppsPageOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"fanruan-list-apps-page-outcome": outcome}}, nil
}

func readListAppsPageOutcome(ctx dex.Context) (fanruan.ListAppsPageOutput, error) {
	outcome, err := appOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return fanruan.ListAppsPageOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input fanruan.ListAppsPageInput) fanruan.ListAppsPageInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListAppsPage" }
func (startRequest) WaitFor(dex.Context, fanruan.ListAppsPageInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input fanruan.ListAppsPageInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[fanruan.ListAppsPageInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[fanruan.ListAppsPageResult]
}

func (completeResult) GetStepType() string { return "CompleteListAppsPage" }
func (completeResult) Execute(ctx dex.Context, result fanruan.ListAppsPageResult) (*dex.StepDecision, error) {
	if err := appOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
