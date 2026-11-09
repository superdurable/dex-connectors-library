// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package postscheduling runs Hootsuite schedulePost as one Dex Flow.
package postscheduling

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/hootsuite/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "HootsuitePostScheduling"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "hootsuite"
	operationStepType = "CallSchedulePost"
)

var scheduledPostReceiptAttribute = dex.DefineAttribute[hootsuite.ScheduledPostReceipt]("hootsuite-scheduled-post-receipt")

// Flow runs SchedulePost once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection hootsuite.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection hootsuite.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{scheduledPostReceiptAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(hootsuite.NewSchedulePostStep(hootsuite.SchedulePostStepConfig[hootsuite.SchedulePostInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Hootsuite", Explanation: "Call the provider's schedulePost operation."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Scheduled:           sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:hootsuite-scheduled-post-receipt value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readScheduledPostReceipt(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"hootsuite-scheduled-post-receipt": outcome}}, nil
}

// dex:field attribute-key:hootsuite-scheduled-post-receipt value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readScheduledPostReceipt(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"hootsuite-scheduled-post-receipt": outcome}}, nil
}

func readScheduledPostReceipt(ctx dex.Context) (hootsuite.ScheduledPostReceipt, error) {
	outcome, err := scheduledPostReceiptAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return hootsuite.ScheduledPostReceipt{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input hootsuite.SchedulePostInput) hootsuite.SchedulePostInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartSchedulePost" }
func (startRequest) WaitFor(dex.Context, hootsuite.SchedulePostInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input hootsuite.SchedulePostInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[hootsuite.SchedulePostInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[hootsuite.SchedulePostResult]
}

func (completeResult) GetStepType() string { return "CompleteSchedulePost" }
func (completeResult) Execute(ctx dex.Context, result hootsuite.SchedulePostResult) (*dex.StepDecision, error) {
	if err := scheduledPostReceiptAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
