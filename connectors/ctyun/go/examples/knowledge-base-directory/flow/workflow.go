// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package knowledgebasecatalog runs CTYun listKnowledgeBases as one Dex Flow.
package knowledgebasecatalog

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/ctyun/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "CTYunKnowledgeBaseCatalog"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "ctyun"
	operationStepType = "CallListKnowledgeBases"
)

var knowledgeBaseCatalogOutcomeAttribute = dex.DefineAttribute[ctyun.ListKnowledgeBasesOutput]("ctyun-knowledge-base-directory-outcome")

// Flow runs ListKnowledgeBases once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection ctyun.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection ctyun.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{knowledgeBaseCatalogOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(ctyun.NewListKnowledgeBasesStep(ctyun.ListKnowledgeBasesStepConfig[ctyun.ListKnowledgeBasesInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "CTYun", Explanation: "Read accessible knowledge bases with IDs, names, and file counts."},
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

// dex:field attribute-key:ctyun-knowledge-base-directory-outcome value-type:json editable:false description:"KnowledgeBase catalog"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readKnowledgeBaseCatalogOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"ctyun-knowledge-base-directory-outcome": outcome}}, nil
}

// dex:field attribute-key:ctyun-knowledge-base-directory-outcome value-type:json editable:false description:"KnowledgeBase catalog"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readKnowledgeBaseCatalogOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"ctyun-knowledge-base-directory-outcome": outcome}}, nil
}

func readKnowledgeBaseCatalogOutcome(ctx dex.Context) (ctyun.ListKnowledgeBasesOutput, error) {
	outcome, err := knowledgeBaseCatalogOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return ctyun.ListKnowledgeBasesOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input ctyun.ListKnowledgeBasesInput) ctyun.ListKnowledgeBasesInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListKnowledgeBases" }
func (startRequest) WaitFor(dex.Context, ctyun.ListKnowledgeBasesInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input ctyun.ListKnowledgeBasesInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[ctyun.ListKnowledgeBasesInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[ctyun.ListKnowledgeBasesResult]
}

func (completeResult) GetStepType() string { return "CompleteListKnowledgeBases" }
func (completeResult) Execute(ctx dex.Context, result ctyun.ListKnowledgeBasesResult) (*dex.StepDecision, error) {
	if err := knowledgeBaseCatalogOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
