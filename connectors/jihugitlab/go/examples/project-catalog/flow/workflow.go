// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package projectcatalog runs Jihugitlab listMemberProjects as one Dex Flow.
package projectcatalog

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/jihugitlab/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "JihugitlabProjectCatalog"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "jihugitlab"
	operationStepType = "CallListMemberProjects"
)

var projectCatalogOutcomeAttribute = dex.DefineAttribute[jihugitlab.ListMemberProjectsOutput]("jihugitlab-project-catalog-outcome")

// Flow runs ListMemberProjects once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection jihugitlab.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection jihugitlab.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{projectCatalogOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(jihugitlab.NewListMemberProjectsStep(jihugitlab.ListMemberProjectsStepConfig[jihugitlab.ListMemberProjectsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Jihugitlab", Explanation: "Read one member-project page with IDs, names, paths, and next-page number."},
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

// dex:field attribute-key:jihugitlab-project-catalog-outcome value-type:json editable:false description:"Project catalog"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readProjectCatalogOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"jihugitlab-project-catalog-outcome": outcome}}, nil
}

// dex:field attribute-key:jihugitlab-project-catalog-outcome value-type:json editable:false description:"Project catalog"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readProjectCatalogOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"jihugitlab-project-catalog-outcome": outcome}}, nil
}

func readProjectCatalogOutcome(ctx dex.Context) (jihugitlab.ListMemberProjectsOutput, error) {
	outcome, err := projectCatalogOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return jihugitlab.ListMemberProjectsOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input jihugitlab.ListMemberProjectsInput) jihugitlab.ListMemberProjectsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListMemberProjects" }
func (startRequest) WaitFor(dex.Context, jihugitlab.ListMemberProjectsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input jihugitlab.ListMemberProjectsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[jihugitlab.ListMemberProjectsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[jihugitlab.ListMemberProjectsResult]
}

func (completeResult) GetStepType() string { return "CompleteListMemberProjects" }
func (completeResult) Execute(ctx dex.Context, result jihugitlab.ListMemberProjectsResult) (*dex.StepDecision, error) {
	if err := projectCatalogOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
