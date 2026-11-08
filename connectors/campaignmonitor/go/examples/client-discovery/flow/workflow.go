// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package clientdiscovery runs Campaign Monitor listClients as one Dex Flow.
package clientdiscovery

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/campaignmonitor/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "CampaignMonitorClientDiscovery"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "campaignmonitor"
	operationStepType = "CallListClients"
)

var clientsListAttribute = dex.DefineAttribute[campaignmonitor.ClientsList]("campaignmonitor-clients-list")

// Flow runs ListClients once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection campaignmonitor.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection campaignmonitor.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{clientsListAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(campaignmonitor.NewListClientsStep(campaignmonitor.ListClientsStepConfig[campaignmonitor.ListClientsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Campaign Monitor", Explanation: "Call the provider's listClients operation."},
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

// dex:field attribute-key:campaignmonitor-clients-list value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readClientsList(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"campaignmonitor-clients-list": outcome}}, nil
}

// dex:field attribute-key:campaignmonitor-clients-list value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readClientsList(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"campaignmonitor-clients-list": outcome}}, nil
}

func readClientsList(ctx dex.Context) (campaignmonitor.ClientsList, error) {
	outcome, err := clientsListAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return campaignmonitor.ClientsList{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input campaignmonitor.ListClientsInput) campaignmonitor.ListClientsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListClients" }
func (startRequest) WaitFor(dex.Context, campaignmonitor.ListClientsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input campaignmonitor.ListClientsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[campaignmonitor.ListClientsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[campaignmonitor.ListClientsResult]
}

func (completeResult) GetStepType() string { return "CompleteListClients" }
func (completeResult) Execute(ctx dex.Context, result campaignmonitor.ListClientsResult) (*dex.StepDecision, error) {
	if err := clientsListAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
