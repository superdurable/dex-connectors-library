// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package clientdiscovery

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/campaignmonitor/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// DraftCampaignCreateFlowType is the stable example draft campaign creation identity.
const DraftCampaignCreateFlowType = "CampaignMonitorDraftCampaignCreate"

const createDraftCampaignStepType = "CallCreateDraftCampaign"

var createdCampaignAttribute = dex.DefineAttribute[campaignmonitor.CampaignIdentity]("campaignmonitor-created-campaign")

// DraftCampaignCreateFlow creates a draft campaign from typed start input.
type DraftCampaignCreateFlow struct {
	dex.FlowDefaults
	connection campaignmonitor.Connection
}

// NewDraftCampaignCreateFlow binds the provider connection at registration time.
func NewDraftCampaignCreateFlow(connection campaignmonitor.Connection) *DraftCampaignCreateFlow {
	return &DraftCampaignCreateFlow{connection: connection}
}

func (*DraftCampaignCreateFlow) GetFlowType() string { return DraftCampaignCreateFlowType }

func (*DraftCampaignCreateFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdCampaignAttribute}}
}

func (flow *DraftCampaignCreateFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startDraftCampaignCreate{}),
		dex.DefineStep(campaignmonitor.NewCreateDraftCampaignStep(campaignmonitor.CreateDraftCampaignStepConfig[campaignmonitor.CreateDraftCampaignInput]{
			StepType: createDraftCampaignStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Campaign Monitor", Explanation: "Create a draft campaign for the selected client."},
			Connection:  flow.connection, MapToOperationInput: mapCreateDraftCampaignInput,
			Created: sdkgo.GoTo(completeDraftCampaignCreate{}),
		})),
		dex.DefineStep(completeDraftCampaignCreate{}),
	}
}

func (flow *DraftCampaignCreateFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:campaignmonitor-created-campaign value-type:json editable:false description:"Created campaign"
// GetDexSummary shows the created campaign in Dex Web.
func (*DraftCampaignCreateFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	campaign, err := readCreatedCampaign(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"campaignmonitor-created-campaign": campaign}}, nil
}

// dex:field attribute-key:campaignmonitor-created-campaign value-type:json editable:false description:"Created campaign"
// GetDexDisplay shows the created campaign in Dex Web.
func (*DraftCampaignCreateFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	campaign, err := readCreatedCampaign(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"campaignmonitor-created-campaign": campaign}}, nil
}

func readCreatedCampaign(ctx dex.Context) (campaignmonitor.CampaignIdentity, error) {
	campaign, err := createdCampaignAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return campaignmonitor.CampaignIdentity{}, nil
	}
	return campaign, err
}

func mapCreateDraftCampaignInput(input campaignmonitor.CreateDraftCampaignInput) campaignmonitor.CreateDraftCampaignInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed campaign settings to the provider Step."
type startDraftCampaignCreate struct{ dex.StepDefaults }

func (startDraftCampaignCreate) GetStepType() string { return "StartCreateDraftCampaign" }
func (startDraftCampaignCreate) WaitFor(dex.Context, campaignmonitor.CreateDraftCampaignInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startDraftCampaignCreate) Execute(_ dex.Context, input campaignmonitor.CreateDraftCampaignInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[campaignmonitor.CreateDraftCampaignInput](createDraftCampaignStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the created campaign and complete the Flow."
type completeDraftCampaignCreate struct {
	dex.StepDefaultsNoWaitFor[campaignmonitor.CreateDraftCampaignResult]
}

func (completeDraftCampaignCreate) GetStepType() string { return "CompleteCreateDraftCampaign" }
func (completeDraftCampaignCreate) Execute(ctx dex.Context, result campaignmonitor.CreateDraftCampaignResult) (*dex.StepDecision, error) {
	if err := createdCampaignAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*DraftCampaignCreateFlow)(nil)
