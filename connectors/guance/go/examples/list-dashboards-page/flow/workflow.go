// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package dashboardspage runs Guance listDashboardsPage as one Dex Flow.
package dashboardspage

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/guance/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "GuanceDashboardsPage"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "guance"
	operationStepType = "CallListDashboardsPage"
)

var dashboardsPageOutcomeAttribute = dex.DefineAttribute[guance.ListDashboardsPageOutput]("guance-list-dashboards-page-outcome")

// Flow runs ListDashboardsPage once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection guance.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection guance.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{dashboardsPageOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(guance.NewListDashboardsPageStep(guance.ListDashboardsPageStepConfig[guance.ListDashboardsPageInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Guance", Explanation: "Call the provider's listDashboardsPage operation."},
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

// dex:field attribute-key:guance-list-dashboards-page-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readListDashboardsPageOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"guance-list-dashboards-page-outcome": outcome}}, nil
}

// dex:field attribute-key:guance-list-dashboards-page-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readListDashboardsPageOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"guance-list-dashboards-page-outcome": outcome}}, nil
}

func readListDashboardsPageOutcome(ctx dex.Context) (guance.ListDashboardsPageOutput, error) {
	outcome, err := dashboardsPageOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return guance.ListDashboardsPageOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input guance.ListDashboardsPageInput) guance.ListDashboardsPageInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListDashboardsPage" }
func (startRequest) WaitFor(dex.Context, guance.ListDashboardsPageInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input guance.ListDashboardsPageInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[guance.ListDashboardsPageInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[guance.ListDashboardsPageResult]
}

func (completeResult) GetStepType() string { return "CompleteListDashboardsPage" }
func (completeResult) Execute(ctx dex.Context, result guance.ListDashboardsPageResult) (*dex.StepDecision, error) {
	if err := dashboardsPageOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
