// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package classobjectcount runs Bmob countClassObjects as one Dex Flow.
package classobjectcount

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/bmob/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "BmobClassObjectCount"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "bmob"
	operationStepType = "CallCountClassObjects"
)

var classObjectCountOutcomeAttribute = dex.DefineAttribute[bmob.CountClassObjectsOutput]("bmob-class-object-count-outcome")

// Flow runs CountClassObjects once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection bmob.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection bmob.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{classObjectCountOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(bmob.NewCountClassObjectsStep(bmob.CountClassObjectsStepConfig[bmob.CountClassObjectsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Bmob", Explanation: "Count objects in the requested Bmob class."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Counted:             sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:bmob-class-object-count-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readClassObjectCountOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"bmob-class-object-count-outcome": outcome}}, nil
}

// dex:field attribute-key:bmob-class-object-count-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readClassObjectCountOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"bmob-class-object-count-outcome": outcome}}, nil
}

func readClassObjectCountOutcome(ctx dex.Context) (bmob.CountClassObjectsOutput, error) {
	outcome, err := classObjectCountOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return bmob.CountClassObjectsOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input bmob.CountClassObjectsInput) bmob.CountClassObjectsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartCountClassObjects" }
func (startRequest) WaitFor(dex.Context, bmob.CountClassObjectsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input bmob.CountClassObjectsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[bmob.CountClassObjectsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[bmob.CountClassObjectsResult]
}

func (completeResult) GetStepType() string { return "CompleteCountClassObjects" }
func (completeResult) Execute(ctx dex.Context, result bmob.CountClassObjectsResult) (*dex.StepDecision, error) {
	if err := classObjectCountOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
