// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package transferinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/wise/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// ProfileFlowType identifies the profile inspection example.
	ProfileFlowType = "WiseProfileInspection"
	profileStepType = "CallGetProfile"
)

var profileAttribute = dex.DefineAttribute[wise.ProfileSummary]("wise-profile")

// ProfileFlow reads one profile without retaining personal details.
type ProfileFlow struct {
	dex.FlowDefaults
	connection wise.Connection
}

// NewProfileFlow binds the static Wise connection at registration.
func NewProfileFlow(connection wise.Connection) *ProfileFlow {
	return &ProfileFlow{connection: connection}
}

// GetFlowType returns the stable profile Flow identity.
func (*ProfileFlow) GetFlowType() string { return ProfileFlowType }

// GetPersistenceSchema registers the sanitized profile result.
func (*ProfileFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{profileAttribute}}
}

// GetSteps registers the profile request and completion Steps.
func (flow *ProfileFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startProfileRequest{}),
		dex.DefineStep(wise.NewGetProfileStep(wise.GetProfileStepConfig[wise.GetProfileInput]{
			StepType: profileStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider-profile", GroupLabel: "Wise", Explanation: "Read a profile ID and type."},
			Connection:  flow.connection, MapToOperationInput: mapProfileInput,
			Found: sdkgo.GoTo(completeProfileResult{}),
		})),
		dex.DefineStep(completeProfileResult{}),
	}
}

// GetRPCs exposes summary and display views of the profile result.
func (flow *ProfileFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the stored profile summary.
// dex:field attribute-key:wise-profile value-type:json editable:false description:"Profile ID and type"
func (*ProfileFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	profile, err := readProfile(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"wise-profile": profile}}, nil
}

// GetDexDisplay returns the stored profile summary.
// dex:field attribute-key:wise-profile value-type:json editable:false description:"Profile ID and type"
func (*ProfileFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	profile, err := readProfile(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"wise-profile": profile}}, nil
}

func readProfile(ctx dex.Context) (wise.ProfileSummary, error) {
	profile, err := profileAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return wise.ProfileSummary{}, nil
	}
	return profile, err
}

func mapProfileInput(input wise.GetProfileInput) wise.GetProfileInput { return input }

// dex:group group-id:profile-request group-label:"Profile request"
// dex:explanation text:"Pass the profile ID to Wise."
type startProfileRequest struct{ dex.StepDefaults }

func (startProfileRequest) GetStepType() string { return "StartGetProfile" }
func (startProfileRequest) WaitFor(dex.Context, wise.GetProfileInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startProfileRequest) Execute(_ dex.Context, input wise.GetProfileInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[wise.GetProfileInput](profileStepType), input), nil
}

// dex:group group-id:profile-result group-label:"Profile result"
// dex:explanation text:"Complete with the sanitized profile."
type completeProfileResult struct {
	dex.StepDefaultsNoWaitFor[wise.GetProfileResult]
}

func (completeProfileResult) GetStepType() string { return "CompleteGetProfile" }
func (completeProfileResult) Execute(ctx dex.Context, result wise.GetProfileResult) (*dex.StepDecision, error) {
	if err := profileAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*ProfileFlow)(nil)
