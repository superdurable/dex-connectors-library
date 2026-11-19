// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package eventtrack

import (
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/mixpanel/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// ProfileRequest identifies the Mixpanel profile to query.
type ProfileRequest struct {
	// DistinctID is the profile's Mixpanel identity.
	DistinctID string `json:"distinctID"`
}

// ProfileFlow queries a single profile through the configured connection.
type ProfileFlow struct {
	dex.FlowDefaults
	connection mixpanel.Connection
}

// NewProfileFlow binds the profile query to the configured connection.
func NewProfileFlow(connection mixpanel.Connection) *ProfileFlow {
	return &ProfileFlow{connection: connection}
}

func (*ProfileFlow) GetFlowType() string                         { return "MixpanelProfileQuery" }
func (*ProfileFlow) GetPersistenceSchema() dex.PersistenceSchema { return dex.PersistenceSchema{} }
func (flow *ProfileFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(profileStart{}),
		dex.DefineStep(mixpanel.NewQueryProfilesStep(mixpanel.QueryProfilesStepConfig[ProfileRequest]{
			StepType: "QueryMixpanelProfile", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations: sdkgo.StepAnnotations{GroupID: "profile-query", GroupLabel: "Profile query", Explanation: "Query one profile identity without returning properties."},
			MapToOperationInput: func(request ProfileRequest) mixpanel.QueryProfilesInput {
				return mixpanel.QueryProfilesInput{DistinctID: request.DistinctID}
			},
			Queried: sdkgo.GoTo(profileQueried{}),
		})),
		dex.DefineStep(profileQueried{}),
	}
}

// GetRPCs registers Dex Web summary and display views.
func (flow *ProfileFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns an empty view while the profile query executes.
func (*ProfileFlow) GetDexSummary(_ dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{}}, nil
}

// GetDexDisplay returns an empty view while the profile query executes.
func (*ProfileFlow) GetDexDisplay(_ dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{}}, nil
}

// dex:group group-id:profile-query group-label:"Profile query"
// dex:explanation text:"Validate the profile identity before querying Mixpanel."
type profileStart struct{ dex.StepDefaults }

func (profileStart) GetStepType() string { return "StartMixpanelProfileQuery" }
func (profileStart) WaitFor(dex.Context, ProfileRequest) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (profileStart) Execute(_ dex.Context, request ProfileRequest) (*dex.StepDecision, error) {
	request.DistinctID = strings.TrimSpace(request.DistinctID)
	if request.DistinctID == "" {
		return dex.ForceFail("distinctID is required"), nil
	}
	return dex.GoTo(sdkgo.StepRef[ProfileRequest]("QueryMixpanelProfile"), request), nil
}

// dex:group group-id:profile-query group-label:"Profile query"
// dex:explanation text:"Complete after Mixpanel returns the matching profile identity."
type profileQueried struct {
	dex.StepDefaultsNoWaitFor[mixpanel.QueryProfilesResult]
}

func (profileQueried) GetStepType() string { return "MixpanelProfileQueried" }
func (profileQueried) Execute(_ dex.Context, result mixpanel.QueryProfilesResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*ProfileFlow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*ProfileFlow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*ProfileFlow)(nil).GetDexDisplay
