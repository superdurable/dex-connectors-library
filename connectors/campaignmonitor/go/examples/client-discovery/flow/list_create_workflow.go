// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package clientdiscovery

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/campaignmonitor/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// ListCreateFlowType is the stable example list creation identity.
const ListCreateFlowType = "CampaignMonitorListCreate"

const createListStepType = "CallCreateList"

var createdListAttribute = dex.DefineAttribute[campaignmonitor.ListIdentity]("campaignmonitor-created-list")

// ListCreateFlow creates a subscriber list from typed start input.
type ListCreateFlow struct {
	dex.FlowDefaults
	connection campaignmonitor.Connection
}

// NewListCreateFlow binds the provider connection at registration time.
func NewListCreateFlow(connection campaignmonitor.Connection) *ListCreateFlow {
	return &ListCreateFlow{connection: connection}
}

func (*ListCreateFlow) GetFlowType() string { return ListCreateFlowType }

func (*ListCreateFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdListAttribute}}
}

func (flow *ListCreateFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startListCreate{}),
		dex.DefineStep(campaignmonitor.NewCreateListStep(campaignmonitor.CreateListStepConfig[campaignmonitor.CreateListInput]{
			StepType: createListStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Campaign Monitor", Explanation: "Create a subscriber list for the selected client."},
			Connection:  flow.connection, MapToOperationInput: mapCreateListInput,
			Created: sdkgo.GoTo(completeListCreate{}),
		})),
		dex.DefineStep(completeListCreate{}),
	}
}

func (flow *ListCreateFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:campaignmonitor-created-list value-type:json editable:false description:"Created list"
// GetDexSummary shows the created list in Dex Web.
func (*ListCreateFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	list, err := readCreatedList(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"campaignmonitor-created-list": list}}, nil
}

// dex:field attribute-key:campaignmonitor-created-list value-type:json editable:false description:"Created list"
// GetDexDisplay shows the created list in Dex Web.
func (*ListCreateFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	list, err := readCreatedList(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"campaignmonitor-created-list": list}}, nil
}

func readCreatedList(ctx dex.Context) (campaignmonitor.ListIdentity, error) {
	list, err := createdListAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return campaignmonitor.ListIdentity{}, nil
	}
	return list, err
}

func mapCreateListInput(input campaignmonitor.CreateListInput) campaignmonitor.CreateListInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed list settings to the provider Step."
type startListCreate struct{ dex.StepDefaults }

func (startListCreate) GetStepType() string { return "StartCreateList" }
func (startListCreate) WaitFor(dex.Context, campaignmonitor.CreateListInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startListCreate) Execute(_ dex.Context, input campaignmonitor.CreateListInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[campaignmonitor.CreateListInput](createListStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the created list and complete the Flow."
type completeListCreate struct {
	dex.StepDefaultsNoWaitFor[campaignmonitor.CreateListResult]
}

func (completeListCreate) GetStepType() string { return "CompleteCreateList" }
func (completeListCreate) Execute(ctx dex.Context, result campaignmonitor.CreateListResult) (*dex.StepDecision, error) {
	if err := createdListAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*ListCreateFlow)(nil)
