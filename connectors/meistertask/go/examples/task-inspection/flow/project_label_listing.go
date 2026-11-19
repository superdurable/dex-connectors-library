// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package taskinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/meistertask/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const projectLabelListingStepType = "CallListProjectLabels"

var projectLabelListingAttribute = dex.DefineAttribute[meistertask.ProjectLabels]("meistertask-project-label-list")

// ProjectLabelListingFlow reads one page of labels for a Dex application.
type ProjectLabelListingFlow struct {
	dex.FlowDefaults
	connection meistertask.Connection
}

// NewProjectLabelListingFlow binds the same static MeisterTask connection used by project label inspection.
func NewProjectLabelListingFlow(connection meistertask.Connection) *ProjectLabelListingFlow {
	return &ProjectLabelListingFlow{connection: connection}
}

// GetFlowType identifies the project label listing Flow.
func (*ProjectLabelListingFlow) GetFlowType() string { return "MeisterTaskProjectLabelListing" }

// GetPersistenceSchema registers its durable provider result.
func (*ProjectLabelListingFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{projectLabelListingAttribute}}
}

// GetSteps wires the project label listing Query through an operation-specific factory.
func (flow *ProjectLabelListingFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startProjectLabelListing{}),
		dex.DefineStep(meistertask.NewListProjectLabelsStep(meistertask.ListProjectLabelsStepConfig[meistertask.ListProjectLabelsInput]{
			StepType: projectLabelListingStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "MeisterTask", Explanation: "List labels visible to the connection."},
			Connection:  flow.connection, MapToOperationInput: listProjectLabelsInput,
			Listed: sdkgo.GoTo(completeProjectLabelListing{}),
		})),
		dex.DefineStep(completeProjectLabelListing{}),
	}
}

// GetRPCs exposes the project label page in Dex Web.
func (flow *ProjectLabelListingFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:meistertask-project-label-list value-type:json editable:false description:"Project labels"
// GetDexSummary returns the last project label page.
func (*ProjectLabelListingFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	labels, err := readProjectLabelListing(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"meistertask-project-label-list": labels}}, nil
}

// dex:field attribute-key:meistertask-project-label-list value-type:json editable:false description:"Project labels"
// GetDexDisplay returns the last project label page.
func (*ProjectLabelListingFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	labels, err := readProjectLabelListing(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"meistertask-project-label-list": labels}}, nil
}

func readProjectLabelListing(ctx dex.Context) (meistertask.ProjectLabels, error) {
	labels, err := projectLabelListingAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return meistertask.ProjectLabels{}, nil
	}
	return labels, err
}

func listProjectLabelsInput(input meistertask.ListProjectLabelsInput) meistertask.ListProjectLabelsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass page filters to the MeisterTask list operation."
type startProjectLabelListing struct{ dex.StepDefaults }

func (startProjectLabelListing) GetStepType() string { return "StartProjectLabelListing" }
func (startProjectLabelListing) WaitFor(dex.Context, meistertask.ListProjectLabelsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startProjectLabelListing) Execute(_ dex.Context, input meistertask.ListProjectLabelsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[meistertask.ListProjectLabelsInput](projectLabelListingStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Save the returned project label page and complete the Flow."
type completeProjectLabelListing struct {
	dex.StepDefaultsNoWaitFor[meistertask.ListProjectLabelsResult]
}

func (completeProjectLabelListing) GetStepType() string { return "CompleteProjectLabelListing" }
func (completeProjectLabelListing) Execute(ctx dex.Context, result meistertask.ListProjectLabelsResult) (*dex.StepDecision, error) {
	if err := projectLabelListingAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*ProjectLabelListingFlow)(nil)
