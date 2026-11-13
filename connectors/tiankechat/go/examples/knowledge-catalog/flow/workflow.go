// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package knowledgecatalog runs TiankeChat listKnowledgeEntries as one Dex Flow.
package knowledgecatalog

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/tiankechat/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "TiankeChatKnowledgeCatalog"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "tiankechat"
	operationStepType = "CallListKnowledgeEntries"
)

var knowledgeCatalogOutcomeAttribute = dex.DefineAttribute[tiankechat.ListKnowledgeEntriesOutput]("tiankechat-knowledge-catalog-outcome")

// Flow runs ListKnowledgeEntries once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection tiankechat.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection tiankechat.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{knowledgeCatalogOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(tiankechat.NewListKnowledgeEntriesStep(tiankechat.ListKnowledgeEntriesStepConfig[tiankechat.ListKnowledgeEntriesInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "TiankeChat", Explanation: "Read knowledge entry IDs, categories, enabled flags, and hit counts without question or answer text."},
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

// dex:field attribute-key:tiankechat-knowledge-catalog-outcome value-type:json editable:false description:"KnowledgeEntry catalog"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readKnowledgeCatalogOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"tiankechat-knowledge-catalog-outcome": outcome}}, nil
}

// dex:field attribute-key:tiankechat-knowledge-catalog-outcome value-type:json editable:false description:"KnowledgeEntry catalog"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readKnowledgeCatalogOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"tiankechat-knowledge-catalog-outcome": outcome}}, nil
}

func readKnowledgeCatalogOutcome(ctx dex.Context) (tiankechat.ListKnowledgeEntriesOutput, error) {
	outcome, err := knowledgeCatalogOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return tiankechat.ListKnowledgeEntriesOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input tiankechat.ListKnowledgeEntriesInput) tiankechat.ListKnowledgeEntriesInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListKnowledgeEntries" }
func (startRequest) WaitFor(dex.Context, tiankechat.ListKnowledgeEntriesInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input tiankechat.ListKnowledgeEntriesInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[tiankechat.ListKnowledgeEntriesInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[tiankechat.ListKnowledgeEntriesResult]
}

func (completeResult) GetStepType() string { return "CompleteListKnowledgeEntries" }
func (completeResult) Execute(ctx dex.Context, result tiankechat.ListKnowledgeEntriesResult) (*dex.StepDecision, error) {
	if err := knowledgeCatalogOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
