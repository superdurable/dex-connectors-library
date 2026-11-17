// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/creatio/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const listActivitiesStepType = "ListCreatioActivities"

var activitiesPageAttribute = dex.DefineAttribute[creatio.ActivitiesPage]("creatio-activities-page")

// ActivitiesSearchFlow reads one bounded page of Creatio activities.
type ActivitiesSearchFlow struct {
	dex.FlowDefaults
	connection creatio.Connection
}

// NewActivitiesSearchFlow binds the static Creatio connection.
func NewActivitiesSearchFlow(connection creatio.Connection) *ActivitiesSearchFlow {
	return &ActivitiesSearchFlow{connection: connection}
}

// GetFlowType gives this example a stable Flow identity.
func (*ActivitiesSearchFlow) GetFlowType() string { return "CreatioActivitySearch" }

// GetPersistenceSchema retains the search result for Dex Web.
func (*ActivitiesSearchFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{activitiesPageAttribute}}
}

// GetSteps wires the operation-specific activities Query.
func (flow *ActivitiesSearchFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startActivitiesSearch{}),
		dex.DefineStep(creatio.NewListActivitiesStep(creatio.ListActivitiesStepConfig[creatio.ListActivitiesInput]{
			StepType: listActivitiesStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Creatio", Explanation: "Read one page of Creatio activities."},
			Connection:  flow.connection, MapToOperationInput: mapActivitiesSearchInput,
			Listed: sdkgo.GoTo(completeActivitiesSearch{}),
		})),
		dex.DefineStep(completeActivitiesSearch{}),
	}
}

// GetRPCs exposes read-only Dex Web search views.
func (flow *ActivitiesSearchFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:creatio-activities-page value-type:json editable:false description:"Activities search page"
// GetDexSummary shows the returned page in Dex Web.
func (*ActivitiesSearchFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readActivitiesPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"creatio-activities-page": page}}, nil
}

// dex:field attribute-key:creatio-activities-page value-type:json editable:false description:"Activities search page"
// GetDexDisplay shows the returned page in Dex Web.
func (*ActivitiesSearchFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readActivitiesPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"creatio-activities-page": page}}, nil
}

func readActivitiesPage(ctx dex.Context) (creatio.ActivitiesPage, error) {
	page, err := activitiesPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return creatio.ActivitiesPage{}, nil
	}
	return page, err
}

func mapActivitiesSearchInput(input creatio.ListActivitiesInput) creatio.ListActivitiesInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the requested activities page and filters to Creatio."
type startActivitiesSearch struct{ dex.StepDefaults }

func (startActivitiesSearch) WaitFor(dex.Context, creatio.ListActivitiesInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startActivitiesSearch) Execute(_ dex.Context, input creatio.ListActivitiesInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[creatio.ListActivitiesInput](listActivitiesStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Retain the activities page and complete the Flow."
type completeActivitiesSearch struct {
	dex.StepDefaultsNoWaitFor[creatio.ListActivitiesResult]
}

func (completeActivitiesSearch) Execute(ctx dex.Context, result creatio.ListActivitiesResult) (*dex.StepDecision, error) {
	if err := activitiesPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*ActivitiesSearchFlow)(nil)
