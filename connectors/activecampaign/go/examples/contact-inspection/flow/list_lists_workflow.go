// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/activecampaign/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// ListListsFlowType is the stable example Flow identity for list inspection.
const ListListsFlowType = "ActiveCampaignListInspection"

const listListsStepType = "CallListLists"

var listsPageAttribute = dex.DefineAttribute[activecampaign.ListsPage]("activecampaign-lists-page")

// ListListsFlow reads one email-list page and completes with typed metadata.
type ListListsFlow struct {
	dex.FlowDefaults
	connection activecampaign.Connection
}

// NewListListsFlow binds the provider connection at registration time.
func NewListListsFlow(connection activecampaign.Connection) *ListListsFlow {
	return &ListListsFlow{connection: connection}
}

// GetFlowType returns the stable Flow identity.
func (*ListListsFlow) GetFlowType() string { return ListListsFlowType }

// GetPersistenceSchema registers the list page displayed in Dex Web.
func (*ListListsFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{listsPageAttribute}}
}

// GetSteps registers the list Query and completion path.
func (flow *ListListsFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startListLists{}),
		dex.DefineStep(activecampaign.NewListListsStep(activecampaign.ListListsStepConfig[activecampaign.ListListsInput]{
			StepType: listListsStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "ActiveCampaign", Explanation: "Read one page of email lists and subscriber counts."},
			Connection:          flow.connection,
			MapToOperationInput: func(input activecampaign.ListListsInput) activecampaign.ListListsInput { return input },
			Listed:              sdkgo.GoTo(completeListLists{}),
		})),
		dex.DefineStep(completeListLists{}),
	}
}

// GetRPCs exposes read-only list summaries in Dex Web.
func (flow *ListListsFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:activecampaign-lists-page value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last list page in Dex Web.
func (*ListListsFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readListsPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"activecampaign-lists-page": page}}, nil
}

// dex:field attribute-key:activecampaign-lists-page value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last list page in Dex Web.
func (*ListListsFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readListsPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"activecampaign-lists-page": page}}, nil
}

func readListsPage(ctx dex.Context) (activecampaign.ListsPage, error) {
	page, err := listsPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return activecampaign.ListsPage{}, nil
	}
	return page, err
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass pagination to the list Query."
type startListLists struct{ dex.StepDefaults }

func (startListLists) GetStepType() string { return "StartListLists" }
func (startListLists) WaitFor(dex.Context, activecampaign.ListListsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startListLists) Execute(_ dex.Context, input activecampaign.ListListsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[activecampaign.ListListsInput](listListsStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with sanitized list metadata."
type completeListLists struct {
	dex.StepDefaultsNoWaitFor[activecampaign.ListListsResult]
}

func (completeListLists) GetStepType() string { return "CompleteListLists" }
func (completeListLists) Execute(ctx dex.Context, result activecampaign.ListListsResult) (*dex.StepDecision, error) {
	if err := listsPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*ListListsFlow)(nil)
