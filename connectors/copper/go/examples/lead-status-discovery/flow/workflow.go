// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package leadstatusdiscovery runs Copper listLeadStatuses as one Dex Flow.
package leadstatusdiscovery

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/copper/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "CopperLeadStatusDiscovery"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "copper"
	operationStepType = "CallListLeadStatuses"
)

var leadStatusesAttribute = dex.DefineAttribute[copper.LeadStatusesList]("copper-lead-statuses")

// Flow runs ListLeadStatuses once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection copper.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection copper.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{leadStatusesAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(copper.NewListLeadStatusesStep(copper.ListLeadStatusesStepConfig[copper.ListLeadStatusesInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Copper", Explanation: "List lead statuses for the connected Copper user."},
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

// dex:field attribute-key:copper-lead-statuses value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readLeadStatusesList(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"copper-lead-statuses": outcome}}, nil
}

// dex:field attribute-key:copper-lead-statuses value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readLeadStatusesList(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"copper-lead-statuses": outcome}}, nil
}

func readLeadStatusesList(ctx dex.Context) (copper.LeadStatusesList, error) {
	outcome, err := leadStatusesAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return copper.LeadStatusesList{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input copper.ListLeadStatusesInput) copper.ListLeadStatusesInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListLeadStatuses" }
func (startRequest) WaitFor(dex.Context, copper.ListLeadStatusesInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input copper.ListLeadStatusesInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[copper.ListLeadStatusesInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[copper.ListLeadStatusesResult]
}

func (completeResult) GetStepType() string { return "CompleteListLeadStatuses" }
func (completeResult) Execute(ctx dex.Context, result copper.ListLeadStatusesResult) (*dex.StepDecision, error) {
	if err := leadStatusesAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
