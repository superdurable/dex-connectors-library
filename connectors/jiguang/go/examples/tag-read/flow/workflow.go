// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package tagread runs Jiguang listApplicationTags as one Dex Flow.
package tagread

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/jiguang/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "JiguangTagRead"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "jiguang"
	operationStepType = "CallListApplicationTags"
)

var tagsAttribute = dex.DefineAttribute[jiguang.ApplicationTags]("jiguang-tag-read")

// Flow runs ListApplicationTags once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection jiguang.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection jiguang.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{tagsAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(jiguang.NewListApplicationTagsStep(jiguang.ListApplicationTagsStepConfig[jiguang.ListApplicationTagsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Jiguang", Explanation: "List JPush application tags."},
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

// dex:field attribute-key:jiguang-tag-read value-type:json editable:false description:"JPush application tags"
// GetDexSummary shows the last JPush application tags in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readTags(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"jiguang-tag-read": outcome}}, nil
}

// dex:field attribute-key:jiguang-tag-read value-type:json editable:false description:"JPush application tags"
// GetDexDisplay shows the last JPush application tags in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readTags(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"jiguang-tag-read": outcome}}, nil
}

func readTags(ctx dex.Context) (jiguang.ApplicationTags, error) {
	outcome, err := tagsAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return jiguang.ApplicationTags{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input jiguang.ListApplicationTagsInput) jiguang.ListApplicationTagsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListApplicationTags" }
func (startRequest) WaitFor(dex.Context, jiguang.ListApplicationTagsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input jiguang.ListApplicationTagsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[jiguang.ListApplicationTagsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[jiguang.ListApplicationTagsResult]
}

func (completeResult) GetStepType() string { return "CompleteListApplicationTags" }
func (completeResult) Execute(ctx dex.Context, result jiguang.ListApplicationTagsResult) (*dex.StepDecision, error) {
	if err := tagsAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
