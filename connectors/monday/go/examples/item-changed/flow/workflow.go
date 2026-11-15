// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package itemchanged demonstrates the monday.com itemChanged Trigger: every item created or changed on the
// watched board starts one Flow, which reads the item back and records its current state.
package itemchanged

import (
	"errors"
	"strings"

	monday "github.com/superdurable/dex-connectors-library/connectors/monday/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity each item change starts.
	FlowType = "MondayItemChange"
	// ConnectionName is the static Dex Web connection for monday.com.
	ConnectionName = "monday-board-watcher"
	// ItemChangedTriggerBinding is the stable itemChanged binding that starts this Flow.
	ItemChangedTriggerBinding = "board-item-changes"

	recordItemChangeStepType   = "RecordItemChange"
	readChangedItemStepType    = "ReadChangedItem"
	completeItemChangeStepType = "CompleteItemChange"
)

var (
	itemChangeAttribute        = dex.DefineAttribute[Input]("monday-item-change")
	itemChangeOutcomeAttribute = dex.DefineAttribute[ChangeOutcome]("monday-item-change-outcome")
)

// Input is one itemChanged delivery.
type Input struct {
	// TriggerEventID is the stable Trigger event ID that also names the Flow.
	TriggerEventID string `json:"triggerEventId"`
	// Change is the delivered change.
	Change monday.ItemChangedEvent `json:"change"`
}

// ChangeOutcome is the Flow result and the value of its outcome Attribute.
type ChangeOutcome struct {
	// BoardID is the watched board.
	BoardID string `json:"boardId"`
	// ItemID is the changed item.
	ItemID string `json:"itemId"`
	// ChangeKind is the delivered change kind.
	ChangeKind monday.ItemChangeKind `json:"changeKind"`
	// Current is the item read back from monday.com.
	Current monday.Item `json:"current"`
}

// Flow records one monday.com item change.
type Flow struct {
	dex.FlowDefaults
	connection monday.Connection
}

// NewFlow binds the monday.com Connection at registration time.
func NewFlow(connection monday.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and monday.com connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordItemChange{}),
		dex.DefineStep(monday.NewGetItemStep(monday.GetItemStepConfig[Input]{
			StepType: readChangedItemStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "change", GroupLabel: "Change",
				Explanation: "Read the changed item back, so the record holds its current state rather than the poll's copy.",
			},
			Connection: flow.connection, MapToOperationInput: MapToGetItemInput,
			Found: sdkgo.GoTo(completeItemChange{}),
		})),
		dex.DefineStep(completeItemChange{}),
	}
}

// GetConnectorTriggerBindings declares the itemChanged binding and its board ID unit for Dex Web.
func (*Flow) GetConnectorTriggerBindings() []sdkgo.TriggerBindingDefinition {
	return []sdkgo.TriggerBindingDefinition{
		monday.DefineItemChangedTriggerBinding(monday.ItemChangedTriggerBindingConfig{
			ConnectionName: ConnectionName, BindingName: ItemChangedTriggerBinding,
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
				ID: "watchedBoard", UnitID: monday.UIUnitBoardIDInput, Label: "Watched board", Required: true,
				Description: "Enter the numeric ID of the board whose created and changed items start this Flow: open the board at https://monday.com/ and copy the digits after /boards/ in its address, such as 1234567890, or paste the whole address. The ID is not secret. The connection's user must be able to read the board. Blank saves nothing, and the Worker refuses to start without a board. The first start records the board's items as a baseline and starts no Flow for them. Restart the Worker after saving; a changed board records a new baseline.",
				Bindings:    []sdkgo.ConnectorUIBinding{{Port: monday.UIBoardIDInputPortBoardID, JSONPointer: "/boardId"}},
			}}},
		}),
	}
}

// GetRPCs returns the summary and display RPCs used by Dex Web.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the change and outcome Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{itemChangeAttribute, itemChangeOutcomeAttribute}}
}

// GetDexSummary returns the change kind and outcome.
//
// dex:field attribute-key:monday-item-change value-type:json editable:false description:"Delivered item change"
// dex:field attribute-key:monday-item-change-outcome value-type:json editable:false description:"Recorded change"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	change, outcome, err := itemChangeInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"monday-item-change":         change,
		"monday-item-change-outcome": outcome,
	}}, nil
}

// GetDexDisplay returns the delivered change and the item read back from monday.com.
//
// dex:field attribute-key:monday-item-change value-type:json editable:false description:"Board, change kind, and item from the poll"
// dex:field attribute-key:monday-item-change-outcome value-type:json editable:false description:"Current item read back from monday.com"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	change, outcome, err := itemChangeInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"monday-item-change":         change,
		"monday-item-change-outcome": outcome,
	}}, nil
}

func itemChangeInspection(ctx dex.Context) (Input, ChangeOutcome, error) {
	change, err := optionalAttribute(ctx, itemChangeAttribute)
	if err != nil {
		return Input{}, ChangeOutcome{}, err
	}
	outcome, err := optionalAttribute(ctx, itemChangeOutcomeAttribute)
	return change, outcome, err
}

// optionalAttribute returns the zero value while a running Flow has not set the Attribute yet.
func optionalAttribute[T any](ctx dex.Context, attribute dex.Attribute[T]) (T, error) {
	value, err := attribute.Get(ctx)
	var missingAttribute *dex.AttributeNotFoundError
	if errors.As(err, &missingAttribute) {
		var zero T
		return zero, nil
	}
	return value, err
}

// ResolveFlowID names one Flow per change; a redelivered change finds its existing Flow.
func ResolveFlowID(event sdkgo.TriggerEvent[monday.ItemChangedEvent]) string {
	return "monday-item-change-" + event.ID
}

// MapToFlowInput keeps the Trigger event ID beside the delivered change.
func MapToFlowInput(event sdkgo.TriggerEvent[monday.ItemChangedEvent]) Input {
	return Input{TriggerEventID: event.ID, Change: event.Payload}
}

// AcceptEveryItemChange admits every change; an application narrows it, for example to a Done status.
func AcceptEveryItemChange(sdkgo.TriggerEvent[monday.ItemChangedEvent]) bool { return true }

// MapToGetItemInput reads the changed item with every column.
func MapToGetItemInput(input Input) monday.GetItemInput {
	return monday.GetItemInput{ItemID: input.Change.Item.ID}
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the delivered item change, then read the item back from monday.com."
type recordItemChange struct {
	dex.StepDefaults
}

func (recordItemChange) GetStepType() string { return recordItemChangeStepType }

// WaitFor skips immediately because the Trigger starts the Flow at this Step.
func (recordItemChange) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordItemChange) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	if err := validateInput(input); err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	if err := itemChangeAttribute.Set(ctx, input); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[Input](readChangedItemStepType), input), nil
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the item read back from monday.com as the outcome and complete the Flow."
type completeItemChange struct {
	dex.StepDefaultsNoWaitFor[monday.GetItemResult]
}

func (completeItemChange) GetStepType() string { return completeItemChangeStepType }

func (completeItemChange) Execute(ctx dex.Context, result monday.GetItemResult) (*dex.StepDecision, error) {
	change, err := itemChangeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome := ChangeOutcome{
		BoardID: change.Change.BoardID, ItemID: change.Change.Item.ID, ChangeKind: change.Change.ChangeKind, Current: result.Value,
	}
	if err := itemChangeOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

func validateInput(input Input) error {
	switch {
	case strings.TrimSpace(input.TriggerEventID) == "":
		return errors.New("triggerEventId is required")
	case strings.TrimSpace(input.Change.BoardID) == "" || strings.TrimSpace(input.Change.Item.ID) == "":
		return errors.New("change.boardId and change.item.id are required")
	}
	switch input.Change.ChangeKind {
	case monday.ItemChangeCreated, monday.ItemChangeUpdated:
		return nil
	default:
		return errors.New("change.changeKind must be created or updated")
	}
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
