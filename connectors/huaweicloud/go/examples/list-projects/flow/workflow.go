// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package listprojects runs Huawei Cloud listAccessibleProjects as one Dex Flow.
package listprojects

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/huaweicloud/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "HuaweiCloudProjectList"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "huaweicloud"
	operationStepType = "CallListAccessibleProjects"
)

var projectsOutcomeAttribute = dex.DefineAttribute[huaweicloud.ListAccessibleProjectsOutput]("huaweicloud-list-projects-outcome")

// Flow runs ListAccessibleProjects once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection huaweicloud.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection huaweicloud.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{projectsOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(huaweicloud.NewListAccessibleProjectsStep(huaweicloud.ListAccessibleProjectsStepConfig[huaweicloud.ListAccessibleProjectsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Huawei Cloud", Explanation: "Call the provider's listAccessibleProjects operation."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Found:               sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:huaweicloud-list-projects-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readListProjectsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"huaweicloud-list-projects-outcome": outcome}}, nil
}

// dex:field attribute-key:huaweicloud-list-projects-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readListProjectsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"huaweicloud-list-projects-outcome": outcome}}, nil
}

func readListProjectsOutcome(ctx dex.Context) (huaweicloud.ListAccessibleProjectsOutput, error) {
	outcome, err := projectsOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return huaweicloud.ListAccessibleProjectsOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input huaweicloud.ListAccessibleProjectsInput) huaweicloud.ListAccessibleProjectsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListAccessibleProjects" }
func (startRequest) WaitFor(dex.Context, huaweicloud.ListAccessibleProjectsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input huaweicloud.ListAccessibleProjectsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[huaweicloud.ListAccessibleProjectsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[huaweicloud.ListAccessibleProjectsResult]
}

func (completeResult) GetStepType() string { return "CompleteListAccessibleProjects" }
func (completeResult) Execute(ctx dex.Context, result huaweicloud.ListAccessibleProjectsResult) (*dex.StepDecision, error) {
	if err := projectsOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
