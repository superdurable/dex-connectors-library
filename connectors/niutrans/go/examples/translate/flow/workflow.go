// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package translationflow runs NiuTrans translateText as one Dex Flow.
package translationflow

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/niutrans/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "NiuTransTranslation"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "niutrans"
	operationStepType = "CallTranslateText"
)

var translationOutcomeAttribute = dex.DefineAttribute[niutrans.TranslateTextOutput]("niutrans-translation-outcome")

// Flow runs TranslateText once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection niutrans.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection niutrans.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{translationOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(niutrans.NewTranslateTextStep(niutrans.TranslateTextStepConfig[niutrans.TranslateTextInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "NiuTrans", Explanation: "Translate the bounded source text with NiuTrans."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Translated:          sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:niutrans-translation-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readTranslationOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"niutrans-translation-outcome": outcome}}, nil
}

// dex:field attribute-key:niutrans-translation-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readTranslationOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"niutrans-translation-outcome": outcome}}, nil
}

func readTranslationOutcome(ctx dex.Context) (niutrans.TranslateTextOutput, error) {
	outcome, err := translationOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return niutrans.TranslateTextOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input niutrans.TranslateTextInput) niutrans.TranslateTextInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartTranslateText" }
func (startRequest) WaitFor(dex.Context, niutrans.TranslateTextInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input niutrans.TranslateTextInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[niutrans.TranslateTextInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[niutrans.TranslateTextResult]
}

func (completeResult) GetStepType() string { return "CompleteTranslateText" }
func (completeResult) Execute(ctx dex.Context, result niutrans.TranslateTextResult) (*dex.StepDecision, error) {
	if err := translationOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
