// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package demo

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/elevenlabs/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// HistoryFlowType is the stable history page example identity.
const HistoryFlowType = "ElevenLabsHistory"

var historyAttribute = dex.DefineAttribute[elevenlabs.HistoryItemsPage]("elevenlabs-history")

// HistoryFlow lists generated item IDs without storing text or audio.
type HistoryFlow struct {
	dex.FlowDefaults
	connection elevenlabs.Connection
}

// NewHistoryFlow binds the provider connection at registration time.
func NewHistoryFlow(connection elevenlabs.Connection) *HistoryFlow {
	return &HistoryFlow{connection: connection}
}

// GetFlowType preserves the history Flow identity.
func (*HistoryFlow) GetFlowType() string { return HistoryFlowType }

// GetPersistenceSchema registers the history metadata Attribute.
func (*HistoryFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{historyAttribute}}
}

// GetSteps registers the operation-specific history page Step.
func (flow *HistoryFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startHistory{}),
		dex.DefineStep(elevenlabs.NewListHistoryItemsStep(elevenlabs.ListHistoryItemsStepConfig[elevenlabs.ListHistoryItemsInput]{
			StepType: "CallListHistoryItems", ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "ElevenLabs", Explanation: "Read generated item metadata without text or audio."},
			Connection:          flow.connection,
			MapToOperationInput: mapHistoryInput,
			Listed:              sdkgo.GoTo(completeHistory{}),
		})),
		dex.DefineStep(completeHistory{}),
	}
}

// GetRPCs registers Dex Web summary and display views.
func (flow *HistoryFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:elevenlabs-history value-type:json editable:false description:"Generated item metadata"
// GetDexSummary shows the last history page in Dex Web.
func (*HistoryFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readHistory(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"elevenlabs-history": page}}, nil
}

// dex:field attribute-key:elevenlabs-history value-type:json editable:false description:"Generated item metadata"
// GetDexDisplay shows the last history page in Dex Web.
func (*HistoryFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readHistory(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"elevenlabs-history": page}}, nil
}

func readHistory(ctx dex.Context) (elevenlabs.HistoryItemsPage, error) {
	page, err := historyAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return elevenlabs.HistoryItemsPage{}, nil
	}
	return page, err
}

func mapHistoryInput(input elevenlabs.ListHistoryItemsInput) elevenlabs.ListHistoryItemsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the history filters to ElevenLabs."
type startHistory struct{ dex.StepDefaults }

func (startHistory) GetStepType() string { return "StartHistory" }
func (startHistory) WaitFor(dex.Context, elevenlabs.ListHistoryItemsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startHistory) Execute(_ dex.Context, input elevenlabs.ListHistoryItemsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[elevenlabs.ListHistoryItemsInput]("CallListHistoryItems"), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with generated item IDs and metadata."
type completeHistory struct {
	dex.StepDefaultsNoWaitFor[elevenlabs.ListHistoryItemsResult]
}

func (completeHistory) GetStepType() string { return "CompleteHistory" }
func (completeHistory) Execute(ctx dex.Context, result elevenlabs.ListHistoryItemsResult) (*dex.StepDecision, error) {
	if err := historyAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*HistoryFlow)(nil)
