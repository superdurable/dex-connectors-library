// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package taskinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/meistertask/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const taskChecklistListingStepType = "CallListTaskChecklists"

var taskChecklistListingAttribute = dex.DefineAttribute[meistertask.TaskChecklistsPage]("meistertask-checklist-list")

// TaskChecklistListingFlow reads one page of checklists for a Dex application.
type TaskChecklistListingFlow struct {
	dex.FlowDefaults
	connection meistertask.Connection
}

// NewTaskChecklistListingFlow binds the same static MeisterTask connection used by checklist inspection.
func NewTaskChecklistListingFlow(connection meistertask.Connection) *TaskChecklistListingFlow {
	return &TaskChecklistListingFlow{connection: connection}
}

// GetFlowType identifies the checklist listing Flow.
func (*TaskChecklistListingFlow) GetFlowType() string { return "MeisterTaskTaskChecklistListing" }

// GetPersistenceSchema registers its durable provider result.
func (*TaskChecklistListingFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{taskChecklistListingAttribute}}
}

// GetSteps wires the checklist listing Query through an operation-specific factory.
func (flow *TaskChecklistListingFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startTaskChecklistListing{}),
		dex.DefineStep(meistertask.NewListTaskChecklistsStep(meistertask.ListTaskChecklistsStepConfig[meistertask.ListTaskChecklistsInput]{
			StepType: taskChecklistListingStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "MeisterTask", Explanation: "List checklists visible to the connection."},
			Connection:  flow.connection, MapToOperationInput: listTaskChecklistsInput,
			Listed: sdkgo.GoTo(completeTaskChecklistListing{}),
		})),
		dex.DefineStep(completeTaskChecklistListing{}),
	}
}

// GetRPCs exposes the checklist page in Dex Web.
func (flow *TaskChecklistListingFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:meistertask-checklist-list value-type:json editable:false description:"Checklist page"
// GetDexSummary returns the last checklist page.
func (*TaskChecklistListingFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	checklists, err := readTaskChecklistListing(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"meistertask-checklist-list": checklists}}, nil
}

// dex:field attribute-key:meistertask-checklist-list value-type:json editable:false description:"Checklist page"
// GetDexDisplay returns the last checklist page.
func (*TaskChecklistListingFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	checklists, err := readTaskChecklistListing(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"meistertask-checklist-list": checklists}}, nil
}

func readTaskChecklistListing(ctx dex.Context) (meistertask.TaskChecklistsPage, error) {
	checklists, err := taskChecklistListingAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return meistertask.TaskChecklistsPage{}, nil
	}
	return checklists, err
}

func listTaskChecklistsInput(input meistertask.ListTaskChecklistsInput) meistertask.ListTaskChecklistsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass page filters to the MeisterTask list operation."
type startTaskChecklistListing struct{ dex.StepDefaults }

func (startTaskChecklistListing) GetStepType() string { return "StartTaskChecklistListing" }
func (startTaskChecklistListing) WaitFor(dex.Context, meistertask.ListTaskChecklistsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startTaskChecklistListing) Execute(_ dex.Context, input meistertask.ListTaskChecklistsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[meistertask.ListTaskChecklistsInput](taskChecklistListingStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Save the returned checklist page and complete the Flow."
type completeTaskChecklistListing struct {
	dex.StepDefaultsNoWaitFor[meistertask.ListTaskChecklistsResult]
}

func (completeTaskChecklistListing) GetStepType() string { return "CompleteTaskChecklistListing" }
func (completeTaskChecklistListing) Execute(ctx dex.Context, result meistertask.ListTaskChecklistsResult) (*dex.StepDecision, error) {
	if err := taskChecklistListingAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*TaskChecklistListingFlow)(nil)
