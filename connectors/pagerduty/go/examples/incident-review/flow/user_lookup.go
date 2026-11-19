// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package incidentreview

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/pagerduty/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// UserLookupFlowType is the durable identity of the user lookup example.
const UserLookupFlowType = "PagerDutyUserLookup"

const userLookupStepType = "GetUser"

var userOutcomeAttribute = dex.DefineAttribute[pagerduty.User]("pagerduty-user-lookup-outcome")

// UserLookupRequest identifies a PagerDuty user to inspect.
type UserLookupRequest pagerduty.GetUserInput

// UserLookupFlow reads a user through a generated PagerDuty Connector Step.
type UserLookupFlow struct {
	dex.FlowDefaults
	connection pagerduty.Connection
}

// NewUserLookupFlow binds the named PagerDuty connection.
func NewUserLookupFlow(connection pagerduty.Connection) *UserLookupFlow {
	return &UserLookupFlow{connection: connection}
}

// GetFlowType preserves the stable user lookup identity.
func (*UserLookupFlow) GetFlowType() string { return UserLookupFlowType }

// GetSteps registers the user lookup graph.
func (flow *UserLookupFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(prepareUserLookup{}),
		dex.DefineStep(pagerduty.NewGetUserStep(pagerduty.GetUserStepConfig[UserLookupRequest]{
			StepType: userLookupStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "pagerduty-user", GroupLabel: "PagerDuty user", Explanation: "Read the selected PagerDuty user."},
			MapToOperationInput: func(request UserLookupRequest) pagerduty.GetUserInput { return pagerduty.GetUserInput(request) },
			Found:               sdkgo.GoTo(completeUserLookup{}),
		})),
		dex.DefineStep(completeUserLookup{}),
	}
}

// GetRPCs registers Dex Web summary and display views.
func (flow *UserLookupFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetPersistenceSchema registers the outcome Attribute.
func (*UserLookupFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{userOutcomeAttribute}}
}

// GetDexSummary returns the looked-up profile.
// dex:field attribute-key:pagerduty-user-lookup-outcome value-type:json editable:false description:"PagerDuty user"
func (*UserLookupFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	user, err := readUserOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"pagerduty-user-lookup-outcome": user}}, nil
}

// GetDexDisplay returns the looked-up profile.
// dex:field attribute-key:pagerduty-user-lookup-outcome value-type:json editable:false description:"PagerDuty user"
func (*UserLookupFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	user, err := readUserOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"pagerduty-user-lookup-outcome": user}}, nil
}

func readUserOutcome(ctx dex.Context) (pagerduty.User, error) {
	user, err := userOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return pagerduty.User{}, nil
	}
	return user, err
}

// dex:group group-id:pagerduty-user-prepare group-label:"Prepare user lookup"
// dex:explanation text:"Pass the selected user ID to the connector operation."
type prepareUserLookup struct{ dex.StepDefaults }

func (prepareUserLookup) GetStepType() string { return "PrepareUserLookup" }
func (prepareUserLookup) WaitFor(dex.Context, UserLookupRequest) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (prepareUserLookup) Execute(_ dex.Context, request UserLookupRequest) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[UserLookupRequest](userLookupStepType), request), nil
}

// dex:group group-id:pagerduty-user-complete group-label:"Complete user lookup"
// dex:explanation text:"Store and return the selected PagerDuty user."
type completeUserLookup struct {
	dex.StepDefaultsNoWaitFor[pagerduty.GetUserResult]
}

func (completeUserLookup) GetStepType() string { return "CompleteUserLookup" }
func (completeUserLookup) Execute(ctx dex.Context, result pagerduty.GetUserResult) (*dex.StepDecision, error) {
	if err := userOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*UserLookupFlow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*UserLookupFlow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*UserLookupFlow)(nil).GetDexDisplay
