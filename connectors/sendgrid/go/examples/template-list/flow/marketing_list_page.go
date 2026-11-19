// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package templatelist

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/sendgrid/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// MarketingListFlowType identifies the static-list page example.
	MarketingListFlowType = "SendGridMarketingListPageDemo"
	marketingListStepType = "CallListMarketingLists"
)

var marketingListOutcomeAttribute = dex.DefineAttribute[sendgrid.ListListsOutput]("sendgrid-marketing-list-page-outcome")

// MarketingListFlow retrieves one page of static contact lists.
type MarketingListFlow struct {
	dex.FlowDefaults
	connection sendgrid.Connection
}

// NewMarketingListFlow binds the static SendGrid connection.
func NewMarketingListFlow(connection sendgrid.Connection) *MarketingListFlow {
	return &MarketingListFlow{connection: connection}
}

// GetFlowType returns the stable list page identity.
func (*MarketingListFlow) GetFlowType() string { return MarketingListFlowType }

// GetPersistenceSchema registers the saved list page.
func (*MarketingListFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{marketingListOutcomeAttribute}}
}

// GetSteps registers request, list query, and completion Steps.
func (flow *MarketingListFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startMarketingListPage{}),
		dex.DefineStep(sendgrid.NewListListsStep(sendgrid.ListListsStepConfig[sendgrid.ListListsInput]{
			StepType: marketingListStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "SendGrid", Explanation: "List one bounded page of static marketing contact lists."},
			Connection:  flow.connection, MapToOperationInput: func(input sendgrid.ListListsInput) sendgrid.ListListsInput { return input },
			Listed: sdkgo.GoTo(completeMarketingListPage{}),
		})),
		dex.DefineStep(completeMarketingListPage{}),
	}
}

// GetRPCs exposes Dex Web summary and display views.
func (flow *MarketingListFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:sendgrid-marketing-list-page-outcome value-type:json editable:false description:"Marketing list page"
// GetDexSummary returns the saved list page.
func (*MarketingListFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readMarketingListOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"sendgrid-marketing-list-page-outcome": page}}, nil
}

// dex:field attribute-key:sendgrid-marketing-list-page-outcome value-type:json editable:false description:"Marketing list page"
// GetDexDisplay returns the saved list page.
func (*MarketingListFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readMarketingListOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"sendgrid-marketing-list-page-outcome": page}}, nil
}

func readMarketingListOutcome(ctx dex.Context) (sendgrid.ListListsOutput, error) {
	page, err := marketingListOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return sendgrid.ListListsOutput{}, nil
	}
	return page, err
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the list page selection to SendGrid."
type startMarketingListPage struct{ dex.StepDefaults }

func (startMarketingListPage) GetStepType() string { return "StartMarketingListPage" }
func (startMarketingListPage) WaitFor(dex.Context, sendgrid.ListListsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startMarketingListPage) Execute(_ dex.Context, input sendgrid.ListListsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[sendgrid.ListListsInput](marketingListStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Save the list page and complete the Flow."
type completeMarketingListPage struct {
	dex.StepDefaultsNoWaitFor[sendgrid.ListListsResult]
}

func (completeMarketingListPage) GetStepType() string { return "CompleteMarketingListPage" }
func (completeMarketingListPage) Execute(ctx dex.Context, result sendgrid.ListListsResult) (*dex.StepDecision, error) {
	if err := marketingListOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*MarketingListFlow)(nil)
