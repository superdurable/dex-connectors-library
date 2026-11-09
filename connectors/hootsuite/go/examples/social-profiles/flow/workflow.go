// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package socialprofiles runs Hootsuite listSocialProfiles as one Dex Flow.
package socialprofiles

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/hootsuite/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "HootsuiteSocialProfiles"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "hootsuite"
	operationStepType = "CallListSocialProfiles"
)

var socialProfilesPageAttribute = dex.DefineAttribute[hootsuite.SocialProfilesPage]("hootsuite-social-profiles-page")

// Flow runs ListSocialProfiles once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection hootsuite.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection hootsuite.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{socialProfilesPageAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(hootsuite.NewListSocialProfilesStep(hootsuite.ListSocialProfilesStepConfig[hootsuite.ListSocialProfilesInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Hootsuite", Explanation: "Call the provider's listSocialProfiles operation."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Listed:              sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:hootsuite-social-profiles-page value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readSocialProfilesPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"hootsuite-social-profiles-page": outcome}}, nil
}

// dex:field attribute-key:hootsuite-social-profiles-page value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readSocialProfilesPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"hootsuite-social-profiles-page": outcome}}, nil
}

func readSocialProfilesPage(ctx dex.Context) (hootsuite.SocialProfilesPage, error) {
	outcome, err := socialProfilesPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return hootsuite.SocialProfilesPage{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input hootsuite.ListSocialProfilesInput) hootsuite.ListSocialProfilesInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListSocialProfiles" }
func (startRequest) WaitFor(dex.Context, hootsuite.ListSocialProfilesInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input hootsuite.ListSocialProfilesInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[hootsuite.ListSocialProfilesInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[hootsuite.ListSocialProfilesResult]
}

func (completeResult) GetStepType() string { return "CompleteListSocialProfiles" }
func (completeResult) Execute(ctx dex.Context, result hootsuite.ListSocialProfilesResult) (*dex.StepDecision, error) {
	if err := socialProfilesPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
