// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package demo

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/elevenlabs/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// VoiceMetadataFlowType is the stable voice metadata edit example identity.
const VoiceMetadataFlowType = "ElevenLabsVoiceMetadata"

var voiceMetadataAttribute = dex.DefineAttribute[elevenlabs.EditedVoiceMetadata]("elevenlabs-voice-metadata")

// VoiceMetadataFlow replaces an owned voice's display metadata.
type VoiceMetadataFlow struct {
	dex.FlowDefaults
	connection elevenlabs.Connection
}

// NewVoiceMetadataFlow binds the provider connection at registration time.
func NewVoiceMetadataFlow(connection elevenlabs.Connection) *VoiceMetadataFlow {
	return &VoiceMetadataFlow{connection: connection}
}

// GetFlowType preserves the metadata edit Flow identity.
func (*VoiceMetadataFlow) GetFlowType() string { return VoiceMetadataFlowType }

// GetPersistenceSchema registers the metadata result Attribute.
func (*VoiceMetadataFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{voiceMetadataAttribute}}
}

// GetSteps registers the operation-specific voice metadata Step.
func (flow *VoiceMetadataFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startVoiceMetadata{}),
		dex.DefineStep(elevenlabs.NewEditVoiceMetadataStep(elevenlabs.EditVoiceMetadataStepConfig[elevenlabs.EditVoiceMetadataInput]{
			StepType: "CallEditVoiceMetadata", ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "ElevenLabs", Explanation: "Replace one owned voice's display name and description."},
			Connection:          flow.connection,
			MapToOperationInput: mapVoiceMetadataInput,
			Edited:              sdkgo.GoTo(completeVoiceMetadata{}),
		})),
		dex.DefineStep(completeVoiceMetadata{}),
	}
}

// GetRPCs registers Dex Web summary and display views.
func (flow *VoiceMetadataFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:elevenlabs-voice-metadata value-type:json editable:false description:"Edited voice metadata"
// GetDexSummary shows the last edited voice in Dex Web.
func (*VoiceMetadataFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	metadata, err := readVoiceMetadata(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"elevenlabs-voice-metadata": metadata}}, nil
}

// dex:field attribute-key:elevenlabs-voice-metadata value-type:json editable:false description:"Edited voice metadata"
// GetDexDisplay shows the last edited voice in Dex Web.
func (*VoiceMetadataFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	metadata, err := readVoiceMetadata(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"elevenlabs-voice-metadata": metadata}}, nil
}

func readVoiceMetadata(ctx dex.Context) (elevenlabs.EditedVoiceMetadata, error) {
	metadata, err := voiceMetadataAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return elevenlabs.EditedVoiceMetadata{}, nil
	}
	return metadata, err
}

func mapVoiceMetadataInput(input elevenlabs.EditVoiceMetadataInput) elevenlabs.EditVoiceMetadataInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the requested display metadata to ElevenLabs."
type startVoiceMetadata struct{ dex.StepDefaults }

func (startVoiceMetadata) GetStepType() string { return "StartVoiceMetadata" }
func (startVoiceMetadata) WaitFor(dex.Context, elevenlabs.EditVoiceMetadataInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startVoiceMetadata) Execute(_ dex.Context, input elevenlabs.EditVoiceMetadataInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[elevenlabs.EditVoiceMetadataInput]("CallEditVoiceMetadata"), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the accepted voice display metadata."
type completeVoiceMetadata struct {
	dex.StepDefaultsNoWaitFor[elevenlabs.EditVoiceMetadataResult]
}

func (completeVoiceMetadata) GetStepType() string { return "CompleteVoiceMetadata" }
func (completeVoiceMetadata) Execute(ctx dex.Context, result elevenlabs.EditVoiceMetadataResult) (*dex.StepDecision, error) {
	if err := voiceMetadataAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*VoiceMetadataFlow)(nil)
