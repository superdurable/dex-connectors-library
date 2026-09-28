// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package authenticatedprofile demonstrates the LinkedIn UserInfo Query in a
// Flow started from Dex Web Start Flow.
package authenticatedprofile

import (
	"errors"
	"strings"

	linkedinconnector "github.com/superdurable/dex-connectors-library/connectors/linkedin"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "LinkedInAuthenticatedProfile"
	// ConnectionName is the static Dex Web connection for LinkedIn authorization.
	ConnectionName = "linkedin-profile"

	recordProfileRequestStepType = "RecordLinkedInProfileRequest"
	loadProfileStepType          = "LoadLinkedInAuthenticatedProfile"
	completeProfileStepType      = "CompleteLinkedInProfile"
)

var (
	profileRequestAttribute = dex.DefineAttribute[Input]("linkedin-profile-request")
	profileAttribute        = dex.DefineAttribute[linkedinconnector.AuthenticatedProfile]("linkedin-authenticated-profile")
)

// Input identifies one profile lookup in Dex Web.
type Input struct {
	// RequestLabel is a non-sensitive label for this profile lookup.
	RequestLabel string `json:"requestLabel"`
}

// Flow loads the authenticated LinkedIn member's verified profile.
type Flow struct {
	dex.FlowDefaults
	connection linkedinconnector.Connection
}

// NewFlow binds the LinkedIn Connection at registration time.
func NewFlow(connection linkedinconnector.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the request, connector, and completion Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordProfileRequest{}),
		dex.DefineStep(linkedinconnector.NewGetAuthenticatedProfileStep(linkedinconnector.GetAuthenticatedProfileStepConfig[Input]{
			StepType: loadProfileStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "linkedin", GroupLabel: "LinkedIn",
				Explanation: "Load the authenticated LinkedIn member's verified OpenID Connect profile.",
			},
			Connection: flow.connection, MapToOperationInput: flow.MapToGetAuthenticatedProfileInput,
			ProfileLoaded: sdkgo.GoTo(completeProfile{}),
		})),
		dex.DefineStep(completeProfile{}),
	}
}

// GetRPCs returns the summary and display RPCs used by Dex Web.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the request and profile Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{profileRequestAttribute, profileAttribute}}
}

// GetDexSummary returns the lookup label and authenticated profile.
//
// dex:field attribute-key:linkedin-profile-request value-type:json editable:false description:"Submitted profile lookup"
// dex:field attribute-key:linkedin-authenticated-profile value-type:json editable:false description:"Verified LinkedIn profile"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, profile, err := profileInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"linkedin-profile-request":       request,
		"linkedin-authenticated-profile": profile,
	}}, nil
}

// GetDexDisplay returns the lookup label and authenticated profile.
//
// dex:field attribute-key:linkedin-profile-request value-type:json editable:false description:"Non-sensitive profile lookup label"
// dex:field attribute-key:linkedin-authenticated-profile value-type:json editable:false description:"OIDC subject, name, locale, picture, and verified email"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, profile, err := profileInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"linkedin-profile-request":       request,
		"linkedin-authenticated-profile": profile,
	}}, nil
}

// MapToGetAuthenticatedProfileInput returns the provider Query input.
func (*Flow) MapToGetAuthenticatedProfileInput(Input) linkedinconnector.GetAuthenticatedProfileInput {
	return linkedinconnector.GetAuthenticatedProfileInput{}
}

func profileInspection(ctx dex.Context) (Input, linkedinconnector.AuthenticatedProfile, error) {
	request, err := optionalAttribute(ctx, profileRequestAttribute)
	if err != nil {
		return Input{}, linkedinconnector.AuthenticatedProfile{}, err
	}
	profile, err := optionalAttribute(ctx, profileAttribute)
	if err != nil {
		return Input{}, linkedinconnector.AuthenticatedProfile{}, err
	}
	return request, profile, nil
}

func optionalAttribute[T any](ctx dex.Context, attribute dex.Attribute[T]) (T, error) {
	value, err := attribute.Get(ctx)
	var missingAttribute *dex.AttributeNotFoundError
	if errors.As(err, &missingAttribute) {
		var zero T
		return zero, nil
	}
	return value, err
}

// dex:group group-id:linkedin group-label:"LinkedIn"
// dex:explanation text:"Validate and record the lookup before calling LinkedIn."
type recordProfileRequest struct {
	dex.StepDefaults
}

func (recordProfileRequest) GetStepType() string { return recordProfileRequestStepType }

// WaitFor skips immediately because Dex Web invokes the start Step's WaitFor.
func (recordProfileRequest) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordProfileRequest) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	input.RequestLabel = strings.TrimSpace(input.RequestLabel)
	if input.RequestLabel == "" {
		return dex.ForceFail("requestLabel is required"), nil
	}
	if err := profileRequestAttribute.Set(ctx, input); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[Input](loadProfileStepType), input), nil
}

// dex:group group-id:linkedin group-label:"LinkedIn"
// dex:explanation text:"Persist the verified LinkedIn profile and complete the Flow."
type completeProfile struct {
	dex.StepDefaultsNoWaitFor[linkedinconnector.GetAuthenticatedProfileResult]
}

func (completeProfile) GetStepType() string { return completeProfileStepType }

func (completeProfile) Execute(ctx dex.Context, result linkedinconnector.GetAuthenticatedProfileResult) (*dex.StepDecision, error) {
	if err := profileAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
