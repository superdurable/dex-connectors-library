// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package candidateinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/ashby/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const createApplicationStepType = "CallCreateApplication"

var createdApplicationAttribute = dex.DefineAttribute[ashby.ApplicationSummary]("ashby-created-application")

// ApplicationCreateFlow creates an Ashby application.
type ApplicationCreateFlow struct {
	dex.FlowDefaults
	connection ashby.Connection
}

// NewApplicationCreateFlow binds the provider connection at registration time.
func NewApplicationCreateFlow(connection ashby.Connection) *ApplicationCreateFlow {
	return &ApplicationCreateFlow{connection: connection}
}

// GetFlowType returns the stable example identity.
func (*ApplicationCreateFlow) GetFlowType() string { return "AshbyApplicationCreate" }

// GetPersistenceSchema registers the retained application result.
func (*ApplicationCreateFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdApplicationAttribute}}
}

// GetSteps registers the start, create Mutation, and completion Steps.
func (flow *ApplicationCreateFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(createApplicationStart{}),
		dex.DefineStep(ashby.NewCreateApplicationStep(ashby.CreateApplicationStepConfig[ashby.CreateApplicationInput]{
			StepType: createApplicationStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Ashby", Explanation: "Create an application in Ashby."},
			Connection:  flow.connection, MapToOperationInput: mapToCreateApplicationInput,
			Created: sdkgo.GoTo(completeApplicationCreate{}),
		})),
		dex.DefineStep(completeApplicationCreate{}),
	}
}

// GetRPCs exposes read-only Dex Web views.
func (flow *ApplicationCreateFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:ashby-created-application value-type:json editable:false description:"Created application"
// GetDexSummary shows the created application in Dex Web.
func (*ApplicationCreateFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	application, err := readCreatedApplication(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"ashby-created-application": application}}, nil
}

// dex:field attribute-key:ashby-created-application value-type:json editable:false description:"Created application"
// GetDexDisplay shows the created application in Dex Web.
func (*ApplicationCreateFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	application, err := readCreatedApplication(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"ashby-created-application": application}}, nil
}

func readCreatedApplication(ctx dex.Context) (ashby.ApplicationSummary, error) {
	application, err := createdApplicationAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return ashby.ApplicationSummary{}, nil
	}
	return application, err
}

func mapToCreateApplicationInput(input ashby.CreateApplicationInput) ashby.CreateApplicationInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the candidate and job IDs to the create Step."
type createApplicationStart struct{ dex.StepDefaults }

func (createApplicationStart) GetStepType() string { return "StartCreateApplication" }
func (createApplicationStart) WaitFor(dex.Context, ashby.CreateApplicationInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (createApplicationStart) Execute(_ dex.Context, input ashby.CreateApplicationInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[ashby.CreateApplicationInput](createApplicationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Retain the created application and complete the Flow."
type completeApplicationCreate struct {
	dex.StepDefaultsNoWaitFor[ashby.CreateApplicationResult]
}

func (completeApplicationCreate) GetStepType() string { return "CompleteCreateApplication" }
func (completeApplicationCreate) Execute(ctx dex.Context, result ashby.CreateApplicationResult) (*dex.StepDecision, error) {
	if err := createdApplicationAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*ApplicationCreateFlow)(nil)
