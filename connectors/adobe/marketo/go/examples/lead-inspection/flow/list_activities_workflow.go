// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package leadinspection

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/adobe/marketo/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// ListActivitiesFlowType is the stable activity list Flow identity.
const ListActivitiesFlowType = "MarketoListActivities"
const listActivitiesStepType = "CallListActivities"

var activityPageAttribute = dex.DefineAttribute[marketo.ActivityPage]("marketo-activity-page")

// ListActivitiesFlow reads one activity page by cursor and stores its metadata.
type ListActivitiesFlow struct {
	dex.FlowDefaults
	connection marketo.Connection
}

// NewListActivitiesFlow binds the provider connection at registration.
func NewListActivitiesFlow(connection marketo.Connection) *ListActivitiesFlow {
	return &ListActivitiesFlow{connection: connection}
}
func (*ListActivitiesFlow) GetFlowType() string { return ListActivitiesFlowType }
func (*ListActivitiesFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{activityPageAttribute}}
}
func (flow *ListActivitiesFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startListActivities{}),
		dex.DefineStep(marketo.NewListActivitiesStep(marketo.ListActivitiesStepConfig[marketo.ListActivitiesInput]{
			StepType: listActivitiesStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Marketo", Explanation: "Find one page of activity metadata by cursor."},
			Connection:  flow.connection, MapToOperationInput: mapListActivitiesInput,
			Found: sdkgo.GoTo(completeListActivities{}),
		})),
		dex.DefineStep(completeListActivities{}),
	}
}
func (flow *ListActivitiesFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:marketo-activity-page value-type:json editable:false description:"Matching activity metadata"
// GetDexSummary shows the matching page.
func (*ListActivitiesFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readActivityPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"marketo-activity-page": page}}, nil
}

// dex:field attribute-key:marketo-activity-page value-type:json editable:false description:"Matching activity metadata"
// GetDexDisplay shows the matching page.
func (*ListActivitiesFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readActivityPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"marketo-activity-page": page}}, nil
}
func readActivityPage(ctx dex.Context) (marketo.ActivityPage, error) {
	page, err := activityPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return marketo.ActivityPage{}, nil
	}
	return page, err
}
func mapListActivitiesInput(input marketo.ListActivitiesInput) marketo.ListActivitiesInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the email search input to the provider Step."
type startListActivities struct{ dex.StepDefaults }

func (startListActivities) GetStepType() string { return "StartListActivities" }
func (startListActivities) WaitFor(dex.Context, marketo.ListActivitiesInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startListActivities) Execute(_ dex.Context, input marketo.ListActivitiesInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[marketo.ListActivitiesInput](listActivitiesStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Store the activity page and complete the Flow."
type completeListActivities struct {
	dex.StepDefaultsNoWaitFor[marketo.ListActivitiesResult]
}

func (completeListActivities) GetStepType() string { return "CompleteListActivities" }
func (completeListActivities) Execute(ctx dex.Context, result marketo.ListActivitiesResult) (*dex.StepDecision, error) {
	if err := activityPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*ListActivitiesFlow)(nil)
