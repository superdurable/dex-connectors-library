// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactinspection

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/constantcontact/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const activitiesStepType = "ListConstantContactActivities"

// ActivitiesFlow reads recent bulk activities and completes with the typed result.
type ActivitiesFlow struct {
	dex.FlowDefaults
	connection constantcontact.Connection
}

// NewActivitiesFlow binds the provider connection for activity inspection.
func NewActivitiesFlow(connection constantcontact.Connection) *ActivitiesFlow {
	return &ActivitiesFlow{connection: connection}
}

// GetPersistenceSchema declares the list result retained for Dex Web.
func (*ActivitiesFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{activitiesAttribute}}
}

// GetSteps wires the operation-specific list Query to a completion Step.
func (flow *ActivitiesFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startActivities{}),
		dex.DefineStep(constantcontact.NewListActivitiesStep(constantcontact.ListActivitiesStepConfig[constantcontact.ListActivitiesInput]{
			StepType: activitiesStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Constant Contact", Explanation: "Read a page of activities from Constant Contact."},
			Connection:          flow.connection,
			MapToOperationInput: mapActivitiesInput,
			Listed:              sdkgo.GoTo(completeActivities{}),
		})),
		dex.DefineStep(completeActivities{}),
	}
}

// GetRPCs exposes read-only Dex Web views of the last list page.
func (flow *ActivitiesFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:constantcontact-activities value-type:json editable:false description:"Provider list page"
// GetDexSummary shows the last list page in Dex Web.
func (*ActivitiesFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readActivities(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"constantcontact-activities": page}}, nil
}

// dex:field attribute-key:constantcontact-activities value-type:json editable:false description:"Recent bulk activities"
// GetDexDisplay shows the last list page in Dex Web.
func (*ActivitiesFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readActivities(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"constantcontact-activities": page}}, nil
}

func readActivities(ctx dex.Context) (constantcontact.ActivitiesPage, error) {
	page, err := activitiesAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return constantcontact.ActivitiesPage{}, nil
	}
	return page, err
}

var activitiesAttribute = dex.DefineAttribute[constantcontact.ActivitiesPage]("constantcontact-activities")

func mapActivitiesInput(input constantcontact.ListActivitiesInput) constantcontact.ListActivitiesInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the requested activity page to the provider Step."
type startActivities struct{ dex.StepDefaults }

func (startActivities) WaitFor(dex.Context, constantcontact.ListActivitiesInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startActivities) Execute(_ dex.Context, input constantcontact.ListActivitiesInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[constantcontact.ListActivitiesInput](activitiesStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Save the returned activity page and complete the Flow."
type completeActivities struct {
	dex.StepDefaultsNoWaitFor[constantcontact.ListActivitiesResult]
}

func (completeActivities) Execute(ctx dex.Context, result constantcontact.ListActivitiesResult) (*dex.StepDecision, error) {
	if err := activitiesAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*ActivitiesFlow)(nil)
