// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package regionsflow runs Alibaba Cloud describeRegions as one Dex Flow.
package regionsflow

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/alibaba/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "AlibabaCloudRegions"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "alibaba"
	operationStepType = "CallDescribeRegions"
)

var regionsOutcomeAttribute = dex.DefineAttribute[alibaba.DescribeRegionsOutput]("alibaba-regions-outcome")

// Flow runs DescribeRegions once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection alibaba.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection alibaba.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{regionsOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(alibaba.NewDescribeRegionsStep(alibaba.DescribeRegionsStepConfig[alibaba.DescribeRegionsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Alibaba Cloud", Explanation: "List ECS regions through the signed OpenAPI query."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Fetched:             sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:alibaba-regions-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readRegionsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"alibaba-regions-outcome": outcome}}, nil
}

// dex:field attribute-key:alibaba-regions-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readRegionsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"alibaba-regions-outcome": outcome}}, nil
}

func readRegionsOutcome(ctx dex.Context) (alibaba.DescribeRegionsOutput, error) {
	outcome, err := regionsOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return alibaba.DescribeRegionsOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input alibaba.DescribeRegionsInput) alibaba.DescribeRegionsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartDescribeRegions" }
func (startRequest) WaitFor(dex.Context, alibaba.DescribeRegionsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input alibaba.DescribeRegionsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[alibaba.DescribeRegionsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[alibaba.DescribeRegionsResult]
}

func (completeResult) GetStepType() string { return "CompleteDescribeRegions" }
func (completeResult) Execute(ctx dex.Context, result alibaba.DescribeRegionsResult) (*dex.StepDecision, error) {
	if err := regionsOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
