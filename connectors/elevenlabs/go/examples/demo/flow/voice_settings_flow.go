// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package demo

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/elevenlabs/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// VoiceSettingsFlowType identifies the voice settings example.
const VoiceSettingsFlowType = "ElevenLabsVoiceSettings"

var voiceSettingsOutcomeAttribute = dex.DefineAttribute[elevenlabs.UpdatedVoiceSettings]("elevenlabs-voice-settings-outcome")

// VoiceSettingsFlow replaces the settings of one account voice.
type VoiceSettingsFlow struct {
	dex.FlowDefaults
	connection elevenlabs.Connection
}

// NewVoiceSettingsFlow binds the static connection at registration time.
func NewVoiceSettingsFlow(connection elevenlabs.Connection) *VoiceSettingsFlow {
	return &VoiceSettingsFlow{connection: connection}
}

// GetFlowType returns the stable Flow identity.
func (*VoiceSettingsFlow) GetFlowType() string { return VoiceSettingsFlowType }

// GetPersistenceSchema registers the outcome Attribute.
func (*VoiceSettingsFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{voiceSettingsOutcomeAttribute}}
}

// GetSteps declares the provider update and completion path.
func (flow *VoiceSettingsFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startVoiceSettings{}),
		dex.DefineStep(elevenlabs.NewUpdateVoiceSettingsStep(elevenlabs.UpdateVoiceSettingsStepConfig[elevenlabs.UpdateVoiceSettingsInput]{
			StepType: "CallUpdateVoiceSettings", ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "ElevenLabs", Explanation: "Replace one voice's generation settings."},
			Connection:  flow.connection, MapToOperationInput: mapToVoiceSettingsInput,
			Updated: sdkgo.GoTo(completeVoiceSettings{}),
		})),
		dex.DefineStep(completeVoiceSettings{}),
	}
}

// GetRPCs exposes read-only Dex Web views.
func (flow *VoiceSettingsFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:elevenlabs-voice-settings-outcome value-type:json editable:false description:"Voice settings update"
// GetDexSummary shows the target voice in Dex Web.
func (*VoiceSettingsFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readVoiceSettingsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"elevenlabs-voice-settings-outcome": outcome}}, nil
}

// dex:field attribute-key:elevenlabs-voice-settings-outcome value-type:json editable:false description:"Voice settings update"
// GetDexDisplay shows the target voice in Dex Web.
func (*VoiceSettingsFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readVoiceSettingsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"elevenlabs-voice-settings-outcome": outcome}}, nil
}

func readVoiceSettingsOutcome(ctx dex.Context) (elevenlabs.UpdatedVoiceSettings, error) {
	outcome, err := voiceSettingsOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return elevenlabs.UpdatedVoiceSettings{}, nil
	}
	return outcome, err
}

func mapToVoiceSettingsInput(input elevenlabs.UpdateVoiceSettingsInput) elevenlabs.UpdateVoiceSettingsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass complete voice settings to the provider Step."
type startVoiceSettings struct{ dex.StepDefaults }

func (startVoiceSettings) GetStepType() string { return "StartVoiceSettings" }
func (startVoiceSettings) WaitFor(dex.Context, elevenlabs.UpdateVoiceSettingsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startVoiceSettings) Execute(_ dex.Context, input elevenlabs.UpdateVoiceSettingsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[elevenlabs.UpdateVoiceSettingsInput]("CallUpdateVoiceSettings"), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the updated voice identity."
type completeVoiceSettings struct {
	dex.StepDefaultsNoWaitFor[elevenlabs.UpdateVoiceSettingsResult]
}

func (completeVoiceSettings) GetStepType() string { return "CompleteVoiceSettings" }
func (completeVoiceSettings) Execute(ctx dex.Context, result elevenlabs.UpdateVoiceSettingsResult) (*dex.StepDecision, error) {
	if err := voiceSettingsOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*VoiceSettingsFlow)(nil)
