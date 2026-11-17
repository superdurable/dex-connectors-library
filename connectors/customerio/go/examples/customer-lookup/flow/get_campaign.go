// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package customerlookup

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/customerio/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// campaignFlowType is the stable example Flow identity.
	campaignFlowType = "CustomerioCampaignInspection"
	campaignStepType = "CallGetCampaign"
)

var campaignAttribute = dex.DefineAttribute[customerio.CampaignSummary]("customerio-campaign")

// CampaignInspectionFlow runs GetCampaign once and completes with its typed output.
type CampaignInspectionFlow struct {
	dex.FlowDefaults
	connection customerio.Connection
}

// NewCampaignInspectionFlow binds the provider connection at registration time.
func NewCampaignInspectionFlow(connection customerio.Connection) *CampaignInspectionFlow {
	return &CampaignInspectionFlow{connection: connection}
}

func (*CampaignInspectionFlow) GetFlowType() string { return campaignFlowType }

func (*CampaignInspectionFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{campaignAttribute}}
}

func (flow *CampaignInspectionFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startCampaignInspectionFlow{}),
		dex.DefineStep(customerio.NewGetCampaignStep(customerio.GetCampaignStepConfig[customerio.GetCampaignInput]{
			StepType: campaignStepType, ConnectionName: ConnectionName,
			Annotations:                    sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Customer.io", Explanation: "Find campaign in Customer.io."},
			Connection:                     flow.connection,
			MapToOperationInput: mapCampaignInspectionFlowInput,
			Found:                          sdkgo.GoTo(completeCampaignInspectionFlow{}),
		})),
		dex.DefineStep(completeCampaignInspectionFlow{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *CampaignInspectionFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:customerio-campaign value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*CampaignInspectionFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readCampaign(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"customerio-campaign": outcome}}, nil
}

// dex:field attribute-key:customerio-campaign value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*CampaignInspectionFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readCampaign(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"customerio-campaign": outcome}}, nil
}

func readCampaign(ctx dex.Context) (customerio.CampaignSummary, error) {
	outcome, err := campaignAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return customerio.CampaignSummary{}, nil
	}
	return outcome, err
}

// mapCampaignInspectionFlowInput preserves the validated typed start input for the provider call.
func mapCampaignInspectionFlowInput(input customerio.GetCampaignInput) customerio.GetCampaignInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startCampaignInspectionFlow struct{ dex.StepDefaults }

func (startCampaignInspectionFlow) GetStepType() string { return "StartGetCampaign" }
func (startCampaignInspectionFlow) WaitFor(dex.Context, customerio.GetCampaignInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startCampaignInspectionFlow) Execute(_ dex.Context, input customerio.GetCampaignInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[customerio.GetCampaignInput](campaignStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeCampaignInspectionFlow struct {
	dex.StepDefaultsNoWaitFor[customerio.GetCampaignResult]
}

func (completeCampaignInspectionFlow) GetStepType() string { return "CompleteGetCampaign" }
func (completeCampaignInspectionFlow) Execute(ctx dex.Context, result customerio.GetCampaignResult) (*dex.StepDecision, error) {
	if err := campaignAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CampaignInspectionFlow)(nil)
