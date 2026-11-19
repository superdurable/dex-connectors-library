// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package accounts

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/linkedin/ads/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// AdAccountLookupFlow reads one LinkedIn Advertising account by ID.
type AdAccountLookupFlow struct {
	dex.FlowDefaults
	connection ads.Connection
}

// NewAdAccountLookupFlow binds the provider connection at registration time.
func NewAdAccountLookupFlow(connection ads.Connection) *AdAccountLookupFlow {
	return &AdAccountLookupFlow{connection: connection}
}

func (*AdAccountLookupFlow) GetFlowType() string { return "LinkedInAdsAdAccountLookup" }

var adAccountAttribute = dex.DefineAttribute[ads.AdAccount]("linkedin-ad-account")

func (*AdAccountLookupFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{adAccountAttribute}}
}

func (flow *AdAccountLookupFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startAdAccount{}),
		dex.DefineStep(ads.NewGetAdAccountStep(ads.GetAdAccountStepConfig[ads.GetAdAccountInput]{
			StepType: "CallGetAdAccount", ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "LinkedIn Ads", Explanation: "Read one ad account matching the selected account ID."},
			Connection:  flow.connection, MapToOperationInput: mapAdAccountInput,
			Found: sdkgo.GoTo(completeAdAccount{}),
		})),
		dex.DefineStep(completeAdAccount{}),
	}
}

func (flow *AdAccountLookupFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:linkedin-ad-account value-type:json editable:false description:"Ad account"
// GetDexSummary shows the last ad account result in Dex Web.
func (*AdAccountLookupFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readAdAccount(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"linkedin-ad-account": value}}, nil
}

// dex:field attribute-key:linkedin-ad-account value-type:json editable:false description:"Ad account"
// GetDexDisplay shows the last ad account result in Dex Web.
func (*AdAccountLookupFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readAdAccount(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"linkedin-ad-account": value}}, nil
}

func readAdAccount(ctx dex.Context) (ads.AdAccount, error) {
	value, err := adAccountAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return ads.AdAccount{}, nil
	}
	return value, err
}

func mapAdAccountInput(input ads.GetAdAccountInput) ads.GetAdAccountInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the account ID to LinkedIn Ads."
type startAdAccount struct{ dex.StepDefaults }

func (startAdAccount) GetStepType() string { return "StartGetAdAccount" }
func (startAdAccount) WaitFor(dex.Context, ads.GetAdAccountInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startAdAccount) Execute(_ dex.Context, input ads.GetAdAccountInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[ads.GetAdAccountInput]("CallGetAdAccount"), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Store the ad account and complete the Flow."
type completeAdAccount struct {
	dex.StepDefaultsNoWaitFor[ads.GetAdAccountResult]
}

func (completeAdAccount) GetStepType() string { return "CompleteGetAdAccount" }
func (completeAdAccount) Execute(ctx dex.Context, result ads.GetAdAccountResult) (*dex.StepDecision, error) {
	if err := adAccountAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*AdAccountLookupFlow)(nil)
