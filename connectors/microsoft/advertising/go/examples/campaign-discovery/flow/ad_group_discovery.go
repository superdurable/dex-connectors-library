// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package campaigndiscovery

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/microsoft/advertising/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const listAdGroupsStepType = "CallListAdGroups"

var adGroupsAttribute = dex.DefineAttribute[microsoftadvertising.AdGroupsPage]("microsoft-advertising-ad-groups")

// AdGroupDiscoveryFlow reads the ad groups in one campaign.
type AdGroupDiscoveryFlow struct {
	dex.FlowDefaults
	connection microsoftadvertising.Connection
}

// NewAdGroupDiscoveryFlow binds the static Microsoft Advertising connection.
func NewAdGroupDiscoveryFlow(connection microsoftadvertising.Connection) *AdGroupDiscoveryFlow {
	return &AdGroupDiscoveryFlow{connection: connection}
}

// GetPersistenceSchema registers the ad group page for Dex Web.
func (*AdGroupDiscoveryFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{adGroupsAttribute}}
}

// GetSteps registers the request, provider, and completion Steps.
func (flow *AdGroupDiscoveryFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startAdGroupDiscovery{}),
		dex.DefineStep(microsoftadvertising.NewListAdGroupsStep(microsoftadvertising.ListAdGroupsStepConfig[microsoftadvertising.ListAdGroupsInput]{
			StepType: listAdGroupsStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Microsoft Advertising", Explanation: "List ad groups in one campaign."},
			Connection:  flow.connection,
			MapToOperationInput: func(input microsoftadvertising.ListAdGroupsInput) microsoftadvertising.ListAdGroupsInput {
				return input
			},
			Listed: sdkgo.GoTo(completeAdGroupDiscovery{}),
		})),
		dex.DefineStep(completeAdGroupDiscovery{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *AdGroupDiscoveryFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:microsoft-advertising-ad-groups value-type:json editable:false description:"Ad groups"
// GetDexSummary returns the last ad group listing.
func (*AdGroupDiscoveryFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	groups, err := readAdGroups(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"microsoft-advertising-ad-groups": groups}}, nil
}

// dex:field attribute-key:microsoft-advertising-ad-groups value-type:json editable:false description:"Ad groups"
// GetDexDisplay returns the listing for Dex Web.
func (*AdGroupDiscoveryFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	groups, err := readAdGroups(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"microsoft-advertising-ad-groups": groups}}, nil
}

func readAdGroups(ctx dex.Context) (microsoftadvertising.AdGroupsPage, error) {
	groups, err := adGroupsAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return microsoftadvertising.AdGroupsPage{}, nil
	}
	return groups, err
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the account and campaign IDs to the provider Step."
type startAdGroupDiscovery struct{ dex.StepDefaults }

func (startAdGroupDiscovery) WaitFor(dex.Context, microsoftadvertising.ListAdGroupsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startAdGroupDiscovery) Execute(_ dex.Context, input microsoftadvertising.ListAdGroupsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[microsoftadvertising.ListAdGroupsInput](listAdGroupsStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the campaign's ad groups."
type completeAdGroupDiscovery struct {
	dex.StepDefaultsNoWaitFor[microsoftadvertising.ListAdGroupsResult]
}

func (completeAdGroupDiscovery) Execute(ctx dex.Context, result microsoftadvertising.ListAdGroupsResult) (*dex.StepDecision, error) {
	if err := adGroupsAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*AdGroupDiscoveryFlow)(nil)
