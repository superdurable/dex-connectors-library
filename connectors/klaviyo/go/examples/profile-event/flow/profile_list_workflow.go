// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package profileevent

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/klaviyo/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// ProfileListFlowType is the profile-list example Flow identity.
const ProfileListFlowType = "KlaviyoProfileList"

const profileListStepType = "CallListProfiles"

var profilesPageAttribute = dex.DefineAttribute[klaviyo.ProfilesPage]("klaviyo-profiles-page")

// ProfileListFlow reads a bounded page of Klaviyo profiles.
type ProfileListFlow struct {
	dex.FlowDefaults
	connection klaviyo.Connection
}

// NewProfileListFlow binds the Klaviyo connection at registration time.
func NewProfileListFlow(connection klaviyo.Connection) *ProfileListFlow {
	return &ProfileListFlow{connection: connection}
}

func (*ProfileListFlow) GetFlowType() string { return ProfileListFlowType }

func (*ProfileListFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{profilesPageAttribute}}
}

func (flow *ProfileListFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startProfileList{}),
		dex.DefineStep(klaviyo.NewListProfilesStep(klaviyo.ListProfilesStepConfig[klaviyo.ListProfilesInput]{
			StepType: profileListStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "profile-list", GroupLabel: "Profiles", Explanation: "Read one bounded profile page."},
			Connection:  flow.connection, MapToOperationInput: MapToListProfilesInput,
			Listed: sdkgo.GoTo(completeProfileList{}),
		})),
		dex.DefineStep(completeProfileList{}),
	}
}

func (flow *ProfileListFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:klaviyo-profiles-page value-type:json editable:false description:"Profile page"
// GetDexSummary shows the profile page in Dex Web.
func (*ProfileListFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readProfilesPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"klaviyo-profiles-page": page}}, nil
}

// dex:field attribute-key:klaviyo-profiles-page value-type:json editable:false description:"Profile page"
// GetDexDisplay shows the profile page in Dex Web.
func (*ProfileListFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readProfilesPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"klaviyo-profiles-page": page}}, nil
}

func readProfilesPage(ctx dex.Context) (klaviyo.ProfilesPage, error) {
	page, err := profilesPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return klaviyo.ProfilesPage{}, nil
	}
	return page, err
}

// MapToListProfilesInput passes the typed page request to Klaviyo.
func MapToListProfilesInput(input klaviyo.ListProfilesInput) klaviyo.ListProfilesInput { return input }

// dex:group group-id:profile-list group-label:"Profiles"
// dex:explanation text:"Pass the requested profile page to the provider Step."
type startProfileList struct{ dex.StepDefaults }

func (startProfileList) GetStepType() string { return "StartKlaviyoProfileList" }
func (startProfileList) WaitFor(dex.Context, klaviyo.ListProfilesInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startProfileList) Execute(_ dex.Context, input klaviyo.ListProfilesInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[klaviyo.ListProfilesInput](profileListStepType), input), nil
}

// dex:group group-id:profile-list group-label:"Profiles"
// dex:explanation text:"Complete the Flow with one typed profile page."
type completeProfileList struct {
	dex.StepDefaultsNoWaitFor[klaviyo.ListProfilesResult]
}

func (completeProfileList) GetStepType() string { return "CompleteKlaviyoProfileList" }
func (completeProfileList) Execute(ctx dex.Context, result klaviyo.ListProfilesResult) (*dex.StepDecision, error) {
	if err := profilesPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*ProfileListFlow)(nil)
