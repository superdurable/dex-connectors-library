// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package campaigndiscovery

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/microsoft/advertising/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const listKeywordsStepType = "CallListKeywords"

var keywordsAttribute = dex.DefineAttribute[microsoftadvertising.KeywordsPage]("microsoft-advertising-keywords")

// KeywordDiscoveryFlow reads the keywords in one ad group.
type KeywordDiscoveryFlow struct {
	dex.FlowDefaults
	connection microsoftadvertising.Connection
}

// NewKeywordDiscoveryFlow binds the static Microsoft Advertising connection.
func NewKeywordDiscoveryFlow(connection microsoftadvertising.Connection) *KeywordDiscoveryFlow {
	return &KeywordDiscoveryFlow{connection: connection}
}

// GetPersistenceSchema registers the keyword page for Dex Web.
func (*KeywordDiscoveryFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{keywordsAttribute}}
}

// GetSteps registers the request, provider, and completion Steps.
func (flow *KeywordDiscoveryFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startKeywordDiscovery{}),
		dex.DefineStep(microsoftadvertising.NewListKeywordsStep(microsoftadvertising.ListKeywordsStepConfig[microsoftadvertising.ListKeywordsInput]{
			StepType: listKeywordsStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Microsoft Advertising", Explanation: "List keywords in one ad group."},
			Connection:  flow.connection,
			MapToOperationInput: func(input microsoftadvertising.ListKeywordsInput) microsoftadvertising.ListKeywordsInput {
				return input
			},
			Listed: sdkgo.GoTo(completeKeywordDiscovery{}),
		})),
		dex.DefineStep(completeKeywordDiscovery{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *KeywordDiscoveryFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:microsoft-advertising-keywords value-type:json editable:false description:"Keywords"
// GetDexSummary returns the last keyword listing.
func (*KeywordDiscoveryFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	keywords, err := readKeywords(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"microsoft-advertising-keywords": keywords}}, nil
}

// dex:field attribute-key:microsoft-advertising-keywords value-type:json editable:false description:"Keywords"
// GetDexDisplay returns the listing for Dex Web.
func (*KeywordDiscoveryFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	keywords, err := readKeywords(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"microsoft-advertising-keywords": keywords}}, nil
}

func readKeywords(ctx dex.Context) (microsoftadvertising.KeywordsPage, error) {
	keywords, err := keywordsAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return microsoftadvertising.KeywordsPage{}, nil
	}
	return keywords, err
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the account and ad group IDs to the provider Step."
type startKeywordDiscovery struct{ dex.StepDefaults }

func (startKeywordDiscovery) WaitFor(dex.Context, microsoftadvertising.ListKeywordsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startKeywordDiscovery) Execute(_ dex.Context, input microsoftadvertising.ListKeywordsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[microsoftadvertising.ListKeywordsInput](listKeywordsStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the ad group's keywords."
type completeKeywordDiscovery struct {
	dex.StepDefaultsNoWaitFor[microsoftadvertising.ListKeywordsResult]
}

func (completeKeywordDiscovery) Execute(ctx dex.Context, result microsoftadvertising.ListKeywordsResult) (*dex.StepDecision, error) {
	if err := keywordsAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*KeywordDiscoveryFlow)(nil)
