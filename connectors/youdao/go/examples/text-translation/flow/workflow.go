// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package texttranslation runs Youdao translateText as one Dex Flow.
package texttranslation

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/youdao/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "YoudaoTextTranslation"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "youdao"
	operationStepType = "CallTranslateText"
)

var translationAttribute = dex.DefineAttribute[youdao.TextTranslation]("youdao-text-translation")

// Flow runs TranslateText once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection youdao.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection youdao.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{translationAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(youdao.NewTranslateTextStep(youdao.TranslateTextStepConfig[youdao.TranslateTextInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Youdao", Explanation: "Translate source text with Youdao."},
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

// dex:field attribute-key:youdao-text-translation value-type:json editable:false description:"Youdao translated text"
// GetDexSummary shows the last Youdao translated text in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readTranslation(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"youdao-text-translation": outcome}}, nil
}

// dex:field attribute-key:youdao-text-translation value-type:json editable:false description:"Youdao translated text"
// GetDexDisplay shows the last Youdao translated text in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readTranslation(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"youdao-text-translation": outcome}}, nil
}

func readTranslation(ctx dex.Context) (youdao.TextTranslation, error) {
	outcome, err := translationAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return youdao.TextTranslation{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input youdao.TranslateTextInput) youdao.TranslateTextInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartTranslateText" }
func (startRequest) WaitFor(dex.Context, youdao.TranslateTextInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input youdao.TranslateTextInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[youdao.TranslateTextInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[youdao.TranslateTextResult]
}

func (completeResult) GetStepType() string { return "CompleteTranslateText" }
func (completeResult) Execute(ctx dex.Context, result youdao.TranslateTextResult) (*dex.StepDecision, error) {
	if err := translationAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
