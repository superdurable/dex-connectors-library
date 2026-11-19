// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package campaigndiscovery

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/microsoft/advertising/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const inspectCampaignStepType = "CallGetCampaign"

var inspectedCampaignAttribute = dex.DefineAttribute[microsoftadvertising.CampaignSummary]("microsoftadvertising-inspected-campaign")

// CampaignInspectionFlow reads one campaign by ID and stores its public summary.
type CampaignInspectionFlow struct {
	dex.FlowDefaults
	connection microsoftadvertising.Connection
}

// NewCampaignInspectionFlow binds the existing named connection.
func NewCampaignInspectionFlow(connection microsoftadvertising.Connection) *CampaignInspectionFlow {
	return &CampaignInspectionFlow{connection: connection}
}

// GetPersistenceSchema registers the campaign summary consumed by the Flow.
func (*CampaignInspectionFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{inspectedCampaignAttribute}}
}

// GetSteps registers the request, provider, and completion Steps.
func (flow *CampaignInspectionFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startCampaignInspection{}),
		dex.DefineStep(microsoftadvertising.NewGetCampaignStep(microsoftadvertising.GetCampaignStepConfig[microsoftadvertising.GetCampaignInput]{
			StepType: inspectCampaignStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Microsoft Advertising", Explanation: "Read one campaign from the account."},
			Connection:          flow.connection,
			MapToOperationInput: func(input microsoftadvertising.GetCampaignInput) microsoftadvertising.GetCampaignInput { return input },
			Found:               sdkgo.GoTo(completeCampaignInspection{}),
		})),
		dex.DefineStep(completeCampaignInspection{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *CampaignInspectionFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:microsoftadvertising-inspected-campaign value-type:json editable:false description:"Campaign summary"
// GetDexSummary shows the inspected campaign in Dex Web.
func (*CampaignInspectionFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	campaign, err := readInspectedCampaign(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"microsoftadvertising-inspected-campaign": campaign}}, nil
}

// dex:field attribute-key:microsoftadvertising-inspected-campaign value-type:json editable:false description:"Campaign summary"
// GetDexDisplay shows the inspected campaign in Dex Web.
func (*CampaignInspectionFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	campaign, err := readInspectedCampaign(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"microsoftadvertising-inspected-campaign": campaign}}, nil
}

func readInspectedCampaign(ctx dex.Context) (microsoftadvertising.CampaignSummary, error) {
	campaign, err := inspectedCampaignAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return microsoftadvertising.CampaignSummary{}, nil
	}
	return campaign, err
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the campaign identity to the provider Step."
type startCampaignInspection struct{ dex.StepDefaults }

func (startCampaignInspection) WaitFor(dex.Context, microsoftadvertising.GetCampaignInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startCampaignInspection) Execute(_ dex.Context, input microsoftadvertising.GetCampaignInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[microsoftadvertising.GetCampaignInput](inspectCampaignStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Store the campaign summary and complete the Flow."
type completeCampaignInspection struct {
	dex.StepDefaultsNoWaitFor[microsoftadvertising.GetCampaignResult]
}

func (completeCampaignInspection) Execute(ctx dex.Context, result microsoftadvertising.GetCampaignResult) (*dex.StepDecision, error) {
	if err := inspectedCampaignAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CampaignInspectionFlow)(nil)
