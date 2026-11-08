// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package projectdiscovery runs Azure DevOps listProjects as one Dex Flow.
package projectdiscovery

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/microsoft/azure-devops/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "AzureDevOpsProjectDiscovery"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "azure-devops"
	operationStepType = "CallListProjects"
)

var projectsAttribute = dex.DefineAttribute[azuredevops.Projects]("azure-devops-projects")

// Flow runs ListProjects once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection azuredevops.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection azuredevops.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{projectsAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(azuredevops.NewListProjectsStep(azuredevops.ListProjectsStepConfig[azuredevops.ListProjectsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Azure DevOps", Explanation: "Call the provider's listProjects operation."},
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

// dex:field attribute-key:azure-devops-projects value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readProjects(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"azure-devops-projects": outcome}}, nil
}

// dex:field attribute-key:azure-devops-projects value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readProjects(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"azure-devops-projects": outcome}}, nil
}

func readProjects(ctx dex.Context) (azuredevops.Projects, error) {
	outcome, err := projectsAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return azuredevops.Projects{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input azuredevops.ListProjectsInput) azuredevops.ListProjectsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListProjects" }
func (startRequest) WaitFor(dex.Context, azuredevops.ListProjectsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input azuredevops.ListProjectsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[azuredevops.ListProjectsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[azuredevops.ListProjectsResult]
}

func (completeResult) GetStepType() string { return "CompleteListProjects" }
func (completeResult) Execute(ctx dex.Context, result azuredevops.ListProjectsResult) (*dex.StepDecision, error) {
	if err := projectsAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
