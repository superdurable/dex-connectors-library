// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package leaddiscovery runs Zendesk Sell listLeads as one Dex Flow.
package leaddiscovery

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/zendesk/sell/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "ZendeskSellLeadDiscovery"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "zendesk-sell"
	operationStepType = "CallListLeads"
)

var leadsPageAttribute = dex.DefineAttribute[sell.LeadsPage]("zendesk-sell-leads-page")

// Flow runs ListLeads once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection sell.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection sell.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{leadsPageAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(sell.NewListLeadsStep(sell.ListLeadsStepConfig[sell.ListLeadsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Zendesk Sell", Explanation: "List a bounded page of leads for the connected Zendesk Sell user."},
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

// dex:field attribute-key:zendesk-sell-leads-page value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readLeadsPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"zendesk-sell-leads-page": outcome}}, nil
}

// dex:field attribute-key:zendesk-sell-leads-page value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readLeadsPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"zendesk-sell-leads-page": outcome}}, nil
}

func readLeadsPage(ctx dex.Context) (sell.LeadsPage, error) {
	outcome, err := leadsPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return sell.LeadsPage{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input sell.ListLeadsInput) sell.ListLeadsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListLeads" }
func (startRequest) WaitFor(dex.Context, sell.ListLeadsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input sell.ListLeadsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[sell.ListLeadsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[sell.ListLeadsResult]
}

func (completeResult) GetStepType() string { return "CompleteListLeads" }
func (completeResult) Execute(ctx dex.Context, result sell.ListLeadsResult) (*dex.StepDecision, error) {
	if err := leadsPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
