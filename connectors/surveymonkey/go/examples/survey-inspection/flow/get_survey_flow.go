// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package surveyinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/surveymonkey/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const getSurveyStepType = "CallGetSurvey"

var surveyDetailAttribute = dex.DefineAttribute[surveymonkey.SurveyMetadata]("surveymonkey-survey-detail")

// GetSurveyFlow reads one survey through the static SurveyMonkey connection.
type GetSurveyFlow struct {
	dex.FlowDefaults
	connection surveymonkey.Connection
}

// NewGetSurveyFlow binds the connector connection at Worker registration.
func NewGetSurveyFlow(connection surveymonkey.Connection) *GetSurveyFlow {
	return &GetSurveyFlow{connection: connection}
}

// GetFlowType returns the stable survey-detail Flow identity.
func (*GetSurveyFlow) GetFlowType() string { return "SurveyMonkeyGetSurvey" }

// GetPersistenceSchema registers the completed survey metadata.
func (*GetSurveyFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{surveyDetailAttribute}}
}

// GetSteps registers the provider query and completion Steps.
func (flow *GetSurveyFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(beginGetSurvey{}),
		dex.DefineStep(surveymonkey.NewGetSurveyStep(surveymonkey.GetSurveyStepConfig[surveymonkey.GetSurveyInput]{
			StepType: getSurveyStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "SurveyMonkey", Explanation: "Read one survey's ID and title."},
			MapToOperationInput: mapGetSurveyInput, Found: sdkgo.GoTo(completeGetSurvey{}),
		})),
		dex.DefineStep(completeGetSurvey{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *GetSurveyFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the stored survey metadata when available.
// dex:field attribute-key:surveymonkey-survey-detail value-type:json editable:false description:"Survey metadata"
func (*GetSurveyFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	survey, err := readSurveyDetail(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"surveymonkey-survey-detail": survey}}, nil
}

// GetDexDisplay returns the stored survey metadata when available.
// dex:field attribute-key:surveymonkey-survey-detail value-type:json editable:false description:"Survey metadata"
func (*GetSurveyFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	survey, err := readSurveyDetail(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"surveymonkey-survey-detail": survey}}, nil
}

func readSurveyDetail(ctx dex.Context) (surveymonkey.SurveyMetadata, error) {
	survey, err := surveyDetailAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return surveymonkey.SurveyMetadata{}, nil
	}
	return survey, err
}

func mapGetSurveyInput(input surveymonkey.GetSurveyInput) surveymonkey.GetSurveyInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the requested survey ID to the provider Step."
type beginGetSurvey struct{ dex.StepDefaults }

func (beginGetSurvey) WaitFor(dex.Context, surveymonkey.GetSurveyInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (beginGetSurvey) Execute(_ dex.Context, input surveymonkey.GetSurveyInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[surveymonkey.GetSurveyInput](getSurveyStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the survey metadata."
type completeGetSurvey struct {
	dex.StepDefaultsNoWaitFor[surveymonkey.GetSurveyResult]
}

func (completeGetSurvey) Execute(ctx dex.Context, result surveymonkey.GetSurveyResult) (*dex.StepDecision, error) {
	if err := surveyDetailAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*GetSurveyFlow)(nil)
