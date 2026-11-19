// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/creatio/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const createActivityStepType = "CreateCreatioActivity"

var createdActivityAttribute = dex.DefineAttribute[creatio.ActivitySummary]("creatio-created-activity")

// ActivityCreationFlow creates one Creatio activity with bounded sends.
type ActivityCreationFlow struct {
	dex.FlowDefaults
	connection creatio.Connection
}

// NewActivityCreationFlow binds the static Creatio connection.
func NewActivityCreationFlow(connection creatio.Connection) *ActivityCreationFlow {
	return &ActivityCreationFlow{connection: connection}
}

// GetFlowType gives this example a stable Flow identity.
func (*ActivityCreationFlow) GetFlowType() string { return "CreatioActivityCreation" }

// GetPersistenceSchema retains the created activity for Dex Web.
func (*ActivityCreationFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdActivityAttribute}}
}

// GetSteps wires the operation-specific activity Mutation.
func (flow *ActivityCreationFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startActivityCreation{}),
		dex.DefineStep(creatio.NewCreateActivityStep(creatio.CreateActivityStepConfig[creatio.CreateActivityInput]{
			StepType: createActivityStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Creatio", Explanation: "Create a Creatio activity."},
			Connection:  flow.connection, MapToOperationInput: mapActivityCreationInput,
			Created: sdkgo.GoTo(completeActivityCreation{}),
		})),
		dex.DefineStep(completeActivityCreation{}),
	}
}

// GetRPCs exposes read-only Dex Web result views.
func (flow *ActivityCreationFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:creatio-created-activity value-type:json editable:false description:"Created activity"
// GetDexSummary shows the created activity in Dex Web.
func (*ActivityCreationFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readActivityMetadata(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"creatio-created-activity": page}}, nil
}

// dex:field attribute-key:creatio-created-activity value-type:json editable:false description:"Created activity"
// GetDexDisplay shows the created activity in Dex Web.
func (*ActivityCreationFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readActivityMetadata(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"creatio-created-activity": page}}, nil
}

func readActivityMetadata(ctx dex.Context) (creatio.ActivitySummary, error) {
	page, err := createdActivityAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return creatio.ActivitySummary{}, nil
	}
	return page, err
}

func mapActivityCreationInput(input creatio.CreateActivityInput) creatio.CreateActivityInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the requested activity title to Creatio."
type startActivityCreation struct{ dex.StepDefaults }

func (startActivityCreation) WaitFor(dex.Context, creatio.CreateActivityInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startActivityCreation) Execute(_ dex.Context, input creatio.CreateActivityInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[creatio.CreateActivityInput](createActivityStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Retain the created activity and complete the Flow."
type completeActivityCreation struct {
	dex.StepDefaultsNoWaitFor[creatio.CreateActivityResult]
}

func (completeActivityCreation) Execute(ctx dex.Context, result creatio.CreateActivityResult) (*dex.StepDecision, error) {
	if err := createdActivityAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*ActivityCreationFlow)(nil)
