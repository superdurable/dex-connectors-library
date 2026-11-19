// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package demo

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/elevenlabs/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// ModelsFlowType is the stable model catalog example identity.
const ModelsFlowType = "ElevenLabsModels"

var modelsAttribute = dex.DefineAttribute[elevenlabs.ModelsList]("elevenlabs-models")

// ModelsFlow lists models available to the connection.
type ModelsFlow struct {
	dex.FlowDefaults
	connection elevenlabs.Connection
}

// NewModelsFlow binds the provider connection at registration time.
func NewModelsFlow(connection elevenlabs.Connection) *ModelsFlow {
	return &ModelsFlow{connection: connection}
}

// GetFlowType preserves the model catalog Flow identity.
func (*ModelsFlow) GetFlowType() string { return ModelsFlowType }

// GetPersistenceSchema registers the model catalog Attribute.
func (*ModelsFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{modelsAttribute}}
}

// GetSteps registers the operation-specific model catalog Step.
func (flow *ModelsFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startModels{}),
		dex.DefineStep(elevenlabs.NewListModelsStep(elevenlabs.ListModelsStepConfig[elevenlabs.ListModelsInput]{
			StepType: "CallListModels", ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "ElevenLabs", Explanation: "Read available model IDs and speech support."},
			Connection:          flow.connection,
			MapToOperationInput: mapModelsInput,
			Listed:              sdkgo.GoTo(completeModels{}),
		})),
		dex.DefineStep(completeModels{}),
	}
}

// GetRPCs registers Dex Web summary and display views.
func (flow *ModelsFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:elevenlabs-models value-type:json editable:false description:"Available models"
// GetDexSummary shows the last model catalog in Dex Web.
func (*ModelsFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	models, err := readModels(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"elevenlabs-models": models}}, nil
}

// dex:field attribute-key:elevenlabs-models value-type:json editable:false description:"Available models"
// GetDexDisplay shows the last model catalog in Dex Web.
func (*ModelsFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	models, err := readModels(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"elevenlabs-models": models}}, nil
}

func readModels(ctx dex.Context) (elevenlabs.ModelsList, error) {
	models, err := modelsAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return elevenlabs.ModelsList{}, nil
	}
	return models, err
}

func mapModelsInput(input elevenlabs.ListModelsInput) elevenlabs.ListModelsInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Request the model catalog from ElevenLabs."
type startModels struct{ dex.StepDefaults }

func (startModels) GetStepType() string { return "StartModels" }
func (startModels) WaitFor(dex.Context, elevenlabs.ListModelsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startModels) Execute(_ dex.Context, input elevenlabs.ListModelsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[elevenlabs.ListModelsInput]("CallListModels"), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with available models and speech support."
type completeModels struct {
	dex.StepDefaultsNoWaitFor[elevenlabs.ListModelsResult]
}

func (completeModels) GetStepType() string { return "CompleteModels" }
func (completeModels) Execute(ctx dex.Context, result elevenlabs.ListModelsResult) (*dex.StepDecision, error) {
	if err := modelsAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*ModelsFlow)(nil)
