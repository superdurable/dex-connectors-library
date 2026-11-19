// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package taskinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/meistertask/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const checklistItemListingStepType = "CallListChecklistItems"

var checklistItemListingAttribute = dex.DefineAttribute[meistertask.ChecklistItemsPage]("meistertask-checklist-item-list")

// ChecklistItemListingFlow reads one page of items for a Dex application.
type ChecklistItemListingFlow struct {
	dex.FlowDefaults
	connection meistertask.Connection
}

// NewChecklistItemListingFlow binds the same static MeisterTask connection used by checklist inspection.
func NewChecklistItemListingFlow(connection meistertask.Connection) *ChecklistItemListingFlow {
	return &ChecklistItemListingFlow{connection: connection}
}

// GetFlowType identifies the checklist listing Flow.
func (*ChecklistItemListingFlow) GetFlowType() string { return "MeisterTaskChecklistItemListing" }

// GetPersistenceSchema registers its durable provider result.
func (*ChecklistItemListingFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{checklistItemListingAttribute}}
}

// GetSteps wires the checklist listing Query through an operation-specific factory.
func (flow *ChecklistItemListingFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startChecklistItemListing{}),
		dex.DefineStep(meistertask.NewListChecklistItemsStep(meistertask.ListChecklistItemsStepConfig[meistertask.ListChecklistItemsInput]{
			StepType: checklistItemListingStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "MeisterTask", Explanation: "List items visible to the connection."},
			Connection:  flow.connection, MapToOperationInput: listChecklistItemsInput,
			Listed: sdkgo.GoTo(completeChecklistItemListing{}),
		})),
		dex.DefineStep(completeChecklistItemListing{}),
	}
}

// GetRPCs exposes the checklist page in Dex Web.
func (flow *ChecklistItemListingFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:meistertask-checklist-item-list value-type:json editable:false description:"Checklist page"
// GetDexSummary returns the last checklist page.
func (*ChecklistItemListingFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	items, err := readChecklistItemListing(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"meistertask-checklist-item-list": items}}, nil
}

// dex:field attribute-key:meistertask-checklist-item-list value-type:json editable:false description:"Checklist page"
// GetDexDisplay returns the last checklist page.
func (*ChecklistItemListingFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	items, err := readChecklistItemListing(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"meistertask-checklist-item-list": items}}, nil
}

func readChecklistItemListing(ctx dex.Context) (meistertask.ChecklistItemsPage, error) {
	items, err := checklistItemListingAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return meistertask.ChecklistItemsPage{}, nil
	}
	return items, err
}

func listChecklistItemsInput(input meistertask.ListChecklistItemsInput) meistertask.ListChecklistItemsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass page filters to the MeisterTask list operation."
type startChecklistItemListing struct{ dex.StepDefaults }

func (startChecklistItemListing) GetStepType() string { return "StartChecklistItemListing" }
func (startChecklistItemListing) WaitFor(dex.Context, meistertask.ListChecklistItemsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startChecklistItemListing) Execute(_ dex.Context, input meistertask.ListChecklistItemsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[meistertask.ListChecklistItemsInput](checklistItemListingStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Save the returned checklist page and complete the Flow."
type completeChecklistItemListing struct {
	dex.StepDefaultsNoWaitFor[meistertask.ListChecklistItemsResult]
}

func (completeChecklistItemListing) GetStepType() string { return "CompleteChecklistItemListing" }
func (completeChecklistItemListing) Execute(ctx dex.Context, result meistertask.ListChecklistItemsResult) (*dex.StepDecision, error) {
	if err := checklistItemListingAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*ChecklistItemListingFlow)(nil)
