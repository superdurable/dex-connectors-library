// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package cardchanged demonstrates the Trello cardChanged Trigger: every card created, updated,
// commented on, moved in, or deleted on the watched board starts one Flow, which reads the card back and
// records its current state.
package cardchanged

import (
	"errors"
	"slices"
	"strings"

	trello "github.com/superdurable/dex-connectors-library/connectors/atlassian/trello/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity each card action starts.
	FlowType = "TrelloCardChange"
	// ConnectionName is the static Dex Web connection for Trello.
	ConnectionName = "trello-card-watcher"
	// CardChangedTriggerBinding is the stable cardChanged binding that starts this Flow.
	CardChangedTriggerBinding = "board-card-changes"

	recordCardChangeStepType       = "RecordCardChange"
	readChangedCardStepType        = "ReadChangedCard"
	completeCardChangeStepType     = "CompleteCardChange"
	recordCardRemovedSinceStepType = "RecordCardRemovedSince"
)

var (
	cardChangeAttribute        = dex.DefineAttribute[Input]("trello-card-change")
	cardChangeOutcomeAttribute = dex.DefineAttribute[ChangeOutcome]("trello-card-change-outcome")
)

// Input is one cardChanged delivery.
type Input struct {
	// TriggerEventID is the stable Trigger event ID that also names the Flow.
	TriggerEventID string `json:"triggerEventId"`
	// Change is the delivered card action.
	Change trello.CardChangedEvent `json:"change"`
}

// ChangeOutcome is the Flow result and the value of its outcome Attribute.
type ChangeOutcome struct {
	// BoardID is the board the action belongs to.
	BoardID string `json:"boardId"`
	// CardID is the changed card.
	CardID string `json:"cardId"`
	// ActionType is the delivered Trello action type.
	ActionType trello.CardActionType `json:"actionType"`
	// Current is the card read back from Trello; nil for a deleted card, which is not read, and for a card
	// deleted after the action.
	Current *trello.Card `json:"current,omitempty"`
}

// Flow records one Trello card action.
type Flow struct {
	dex.FlowDefaults
	connection trello.Connection
}

// NewFlow binds the Trello Connection at registration time.
func NewFlow(connection trello.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and Trello connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordCardChange{}),
		dex.DefineStep(trello.NewGetCardStep(trello.GetCardStepConfig[Input]{
			StepType: readChangedCardStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "change", GroupLabel: "Change",
				Explanation: "Read the changed card back, so the record holds its current state rather than the action snapshot.",
			},
			Connection: flow.connection, MapToOperationInput: MapToGetCardInput,
			Found: sdkgo.GoTo(completeCardChange{}),
			// A card deleted after its action still completes, recorded without a current state.
			NotFound: sdkgo.GoTo(recordCardRemovedSince{}),
		})),
		dex.DefineStep(completeCardChange{}),
		dex.DefineStep(recordCardRemovedSince{}),
	}
}

// GetConnectorTriggerBindings declares the cardChanged binding and its board input for Dex Web.
func (*Flow) GetConnectorTriggerBindings() []sdkgo.TriggerBindingDefinition {
	return []sdkgo.TriggerBindingDefinition{
		trello.DefineCardChangedTriggerBinding(trello.CardChangedTriggerBindingConfig{
			ConnectionName: ConnectionName, BindingName: CardChangedTriggerBinding,
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
				ID: "watchedBoard", UnitID: trello.UIUnitBoardInput, Label: "Watched board", Required: true,
				Description: "Open the board at https://trello.com as the connection's member and paste its address, https://trello.com/b/<short link>/<board name>; the unit saves the 8-character short link, or a 24-character board ID pasted instead. It is not secret. Every card created, updated, commented on, moved in, or deleted on that board starts this Flow. The board is required: leaving it blank stops the Worker at start. The first start records earlier actions as a baseline and starts no Flow for them. Restart the Worker after saving; a changed board records a new baseline.",
				Bindings:    []sdkgo.ConnectorUIBinding{{Port: trello.UIBoardInputPortBoardID, JSONPointer: "/boardId"}},
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
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{cardChangeAttribute, cardChangeOutcomeAttribute}}
}

// GetDexSummary returns the delivered card action and its outcome.
//
// dex:field attribute-key:trello-card-change value-type:json editable:false description:"Delivered card action"
// dex:field attribute-key:trello-card-change-outcome value-type:json editable:false description:"Recorded change"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	change, outcome, err := cardChangeInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"trello-card-change":         change,
		"trello-card-change-outcome": outcome,
	}}, nil
}

// GetDexDisplay returns the delivered card action and the card read back from Trello.
//
// dex:field attribute-key:trello-card-change value-type:json editable:false description:"Board, action type, and card from the action history"
// dex:field attribute-key:trello-card-change-outcome value-type:json editable:false description:"Current card read back from Trello"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	change, outcome, err := cardChangeInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"trello-card-change":         change,
		"trello-card-change-outcome": outcome,
	}}, nil
}

func cardChangeInspection(ctx dex.Context) (Input, ChangeOutcome, error) {
	change, err := optionalAttribute(ctx, cardChangeAttribute)
	if err != nil {
		return Input{}, ChangeOutcome{}, err
	}
	outcome, err := optionalAttribute(ctx, cardChangeOutcomeAttribute)
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

// ResolveFlowID names one Flow per card action; a redelivered action finds its existing Flow.
func ResolveFlowID(event sdkgo.TriggerEvent[trello.CardChangedEvent]) string {
	return "trello-card-change-" + event.ID
}

// MapToFlowInput keeps the Trigger event ID beside the delivered card action.
func MapToFlowInput(event sdkgo.TriggerEvent[trello.CardChangedEvent]) Input {
	return Input{TriggerEventID: event.ID, Change: event.Payload}
}

// AcceptEveryCardChange admits every card action; an application narrows it, for example to cards moved
// into one list (ListAfter) or to comments.
func AcceptEveryCardChange(sdkgo.TriggerEvent[trello.CardChangedEvent]) bool { return true }

// MapToGetCardInput reads the changed card.
func MapToGetCardInput(input Input) trello.GetCardInput {
	return trello.GetCardInput{CardID: input.Change.Card.ID}
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the delivered card action, completing at once for a deleted card and otherwise reading the card back."
type recordCardChange struct {
	dex.StepDefaults
}

func (recordCardChange) GetStepType() string { return recordCardChangeStepType }

// WaitFor skips immediately because the Trigger starts the Flow at this Step.
func (recordCardChange) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordCardChange) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	if err := validateInput(input); err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	if err := cardChangeAttribute.Set(ctx, input); err != nil {
		return nil, err
	}
	if input.Change.ActionType == trello.CardActionDeleted {
		outcome := outcomeOf(input, nil)
		if err := cardChangeOutcomeAttribute.Set(ctx, outcome); err != nil {
			return nil, err
		}
		return dex.GracefulComplete(outcome), nil
	}
	return dex.GoTo(sdkgo.StepRef[Input](readChangedCardStepType), input), nil
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the card read back from Trello as the outcome and complete the Flow."
type completeCardChange struct {
	dex.StepDefaultsNoWaitFor[trello.GetCardResult]
}

func (completeCardChange) GetStepType() string { return completeCardChangeStepType }

func (completeCardChange) Execute(ctx dex.Context, result trello.GetCardResult) (*dex.StepDecision, error) {
	change, err := cardChangeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	current := result.Value
	outcome := outcomeOf(change, &current)
	if err := cardChangeOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record a card deleted after its action without a current state and complete the Flow."
type recordCardRemovedSince struct {
	dex.StepDefaultsNoWaitFor[trello.GetCardResult]
}

func (recordCardRemovedSince) GetStepType() string { return recordCardRemovedSinceStepType }

func (recordCardRemovedSince) Execute(ctx dex.Context, _ trello.GetCardResult) (*dex.StepDecision, error) {
	change, err := cardChangeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome := outcomeOf(change, nil)
	if err := cardChangeOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

func outcomeOf(input Input, current *trello.Card) ChangeOutcome {
	return ChangeOutcome{BoardID: input.Change.BoardID, CardID: input.Change.Card.ID, ActionType: input.Change.ActionType, Current: current}
}

var knownCardActionTypes = []trello.CardActionType{
	trello.CardActionCreated, trello.CardActionCopied, trello.CardActionConvertedFromCheckItem, trello.CardActionEmailed,
	trello.CardActionUpdated, trello.CardActionCommented, trello.CardActionMovedToBoard, trello.CardActionDeleted,
}

func validateInput(input Input) error {
	switch {
	case strings.TrimSpace(input.TriggerEventID) == "":
		return errors.New("triggerEventId is required")
	case strings.TrimSpace(input.Change.ActionID) == "" || strings.TrimSpace(input.Change.Card.ID) == "":
		return errors.New("change.actionId and change.card.id are required")
	case !slices.Contains(knownCardActionTypes, input.Change.ActionType):
		return errors.New("change.actionType must be a Trello card action type such as createCard or updateCard")
	}
	return nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
