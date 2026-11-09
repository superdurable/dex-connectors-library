// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package campaigndiscovery runs Microsoft Advertising listCampaigns as one Dex Flow.
package campaigndiscovery

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/microsoft/advertising/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "MicrosoftAdvertisingCampaignDiscovery"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "microsoft-advertising"
	operationStepType = "CallListCampaigns"
)

var campaignAttribute = dex.DefineAttribute[microsoftadvertising.CampaignsPage]("microsoftadvertising-campaign")

// Flow runs ListCampaigns once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection microsoftadvertising.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection microsoftadvertising.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{campaignAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(microsoftadvertising.NewListCampaignsStep(microsoftadvertising.ListCampaignsStepConfig[microsoftadvertising.ListCampaignsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Microsoft Advertising", Explanation: "List campaigns for the selected Microsoft Advertising account."},
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

// dex:field attribute-key:microsoftadvertising-campaign value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readCampaign(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"microsoftadvertising-campaign": outcome}}, nil
}

// dex:field attribute-key:microsoftadvertising-campaign value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readCampaign(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"microsoftadvertising-campaign": outcome}}, nil
}

func readCampaign(ctx dex.Context) (microsoftadvertising.CampaignsPage, error) {
	outcome, err := campaignAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return microsoftadvertising.CampaignsPage{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input microsoftadvertising.ListCampaignsInput) microsoftadvertising.ListCampaignsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListCampaigns" }
func (startRequest) WaitFor(dex.Context, microsoftadvertising.ListCampaignsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input microsoftadvertising.ListCampaignsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[microsoftadvertising.ListCampaignsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[microsoftadvertising.ListCampaignsResult]
}

func (completeResult) GetStepType() string { return "CompleteListCampaigns" }
func (completeResult) Execute(ctx dex.Context, result microsoftadvertising.ListCampaignsResult) (*dex.StepDecision, error) {
	if err := campaignAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
