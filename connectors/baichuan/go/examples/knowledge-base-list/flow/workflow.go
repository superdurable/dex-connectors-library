// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package knowledgebaselist runs Baichuan AI listKnowledgeBases as one Dex Flow.
package knowledgebaselist

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/baichuan/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "BaichuanKnowledgeBaseList"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "baichuan"
	operationStepType = "CallListKnowledgeBases"
)

var knowledgeBaseListOutcomeAttribute = dex.DefineAttribute[baichuan.ListKnowledgeBasesOutput]("baichuan-knowledge-base-list-outcome")

// Flow runs ListKnowledgeBases once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection baichuan.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection baichuan.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{knowledgeBaseListOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(baichuan.NewListKnowledgeBasesStep(baichuan.ListKnowledgeBasesStepConfig[baichuan.ListKnowledgeBasesInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Baichuan AI", Explanation: "Call the provider's listKnowledgeBases operation."},
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

// dex:field attribute-key:baichuan-knowledge-base-list-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readKnowledgeBaseListOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"baichuan-knowledge-base-list-outcome": outcome}}, nil
}

// dex:field attribute-key:baichuan-knowledge-base-list-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readKnowledgeBaseListOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"baichuan-knowledge-base-list-outcome": outcome}}, nil
}

func readKnowledgeBaseListOutcome(ctx dex.Context) (baichuan.ListKnowledgeBasesOutput, error) {
	outcome, err := knowledgeBaseListOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return baichuan.ListKnowledgeBasesOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput passes the empty start input to the provider call.
func MapToOperationInput(input baichuan.ListKnowledgeBasesInput) baichuan.ListKnowledgeBasesInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListKnowledgeBases" }
func (startRequest) WaitFor(dex.Context, baichuan.ListKnowledgeBasesInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input baichuan.ListKnowledgeBasesInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[baichuan.ListKnowledgeBasesInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[baichuan.ListKnowledgeBasesResult]
}

func (completeResult) GetStepType() string { return "CompleteListKnowledgeBases" }
func (completeResult) Execute(ctx dex.Context, result baichuan.ListKnowledgeBasesResult) (*dex.StepDecision, error) {
	if err := knowledgeBaseListOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
