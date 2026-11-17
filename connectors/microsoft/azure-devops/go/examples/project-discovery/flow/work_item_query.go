// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package projectdiscovery

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/microsoft/azure-devops/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const queryWorkItemsStepType = "CallQueryWorkItems"

var workItemIDsAttribute = dex.DefineAttribute[azuredevops.WorkItemIDs]("azure-devops-work-item-ids")

// QueryWorkItemsFlow runs one bounded WIQL query using the named connection.
type QueryWorkItemsFlow struct {
	dex.FlowDefaults
	connection azuredevops.Connection
}

// NewQueryWorkItemsFlow binds the Azure DevOps connection at registration time.
func NewQueryWorkItemsFlow(connection azuredevops.Connection) *QueryWorkItemsFlow {
	return &QueryWorkItemsFlow{connection: connection}
}

// GetPersistenceSchema registers the IDs displayed by this Flow.
func (*QueryWorkItemsFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{workItemIDsAttribute}}
}

// GetSteps registers the request, provider, and completion Steps.
func (flow *QueryWorkItemsFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startWorkItemQuery{}),
		dex.DefineStep(azuredevops.NewQueryWorkItemsStep(azuredevops.QueryWorkItemsStepConfig[azuredevops.QueryWorkItemsInput]{
			StepType: queryWorkItemsStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Azure DevOps", Explanation: "Query work item IDs in one project."},
			Connection:          flow.connection,
			MapToOperationInput: func(input azuredevops.QueryWorkItemsInput) azuredevops.QueryWorkItemsInput { return input },
			Listed:              sdkgo.GoTo(completeWorkItemQuery{}),
		})),
		dex.DefineStep(completeWorkItemQuery{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *QueryWorkItemsFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:azure-devops-work-item-ids value-type:json editable:false description:"Matching work item IDs"
// GetDexSummary returns the last matched IDs.
func (*QueryWorkItemsFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	ids, err := readWorkItemIDs(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"azure-devops-work-item-ids": ids}}, nil
}

// dex:field attribute-key:azure-devops-work-item-ids value-type:json editable:false description:"Matching work item IDs"
// GetDexDisplay returns the IDs for the Dex Web detail view.
func (*QueryWorkItemsFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	ids, err := readWorkItemIDs(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"azure-devops-work-item-ids": ids}}, nil
}

func readWorkItemIDs(ctx dex.Context) (azuredevops.WorkItemIDs, error) {
	ids, err := workItemIDsAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return azuredevops.WorkItemIDs{}, nil
	}
	return ids, err
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Send the project and WIQL to the provider Step."
type startWorkItemQuery struct{ dex.StepDefaults }

func (startWorkItemQuery) WaitFor(dex.Context, azuredevops.QueryWorkItemsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startWorkItemQuery) Execute(_ dex.Context, input azuredevops.QueryWorkItemsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[azuredevops.QueryWorkItemsInput](queryWorkItemsStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the IDs returned by Azure DevOps."
type completeWorkItemQuery struct {
	dex.StepDefaultsNoWaitFor[azuredevops.QueryWorkItemsResult]
}

func (completeWorkItemQuery) Execute(ctx dex.Context, result azuredevops.QueryWorkItemsResult) (*dex.StepDecision, error) {
	if err := workItemIDsAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*QueryWorkItemsFlow)(nil)
