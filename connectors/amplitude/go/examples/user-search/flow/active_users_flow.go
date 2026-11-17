// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package usersearch

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/amplitude/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// ActiveUsersFlowType is the stable Dex Web Start Flow identity for active user counts.
	ActiveUsersFlowType = "AmplitudeActiveUserCounts"
	activeUsersStepType = "CallGetActiveUserCounts"
)

var activeUsersAttribute = dex.DefineAttribute[amplitude.ActiveUserCounts]("amplitude-active-users")

// ActiveUsersFlow reads active user counts through the example's Amplitude connection.
type ActiveUsersFlow struct {
	dex.FlowDefaults
	connection amplitude.Connection
}

// NewActiveUsersFlow binds the provider connection at registration time.
func NewActiveUsersFlow(connection amplitude.Connection) *ActiveUsersFlow {
	return &ActiveUsersFlow{connection: connection}
}

// GetFlowType returns the stable active user count Flow identity.
func (*ActiveUsersFlow) GetFlowType() string { return ActiveUsersFlowType }

// GetPersistenceSchema registers the active user count series.
func (*ActiveUsersFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{activeUsersAttribute}}
}

// GetRPCs exposes read-only Dex Web views of the active user counts.
func (flow *ActiveUsersFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the active user count series when this Flow has one.
//
// dex:field attribute-key:amplitude-active-users value-type:json editable:false description:"Amplitude active user counts"
func (*ActiveUsersFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	counts, err := readActiveUserCounts(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"amplitude-active-users": counts}}, nil
}

// GetDexDisplay returns the active user count series when this Flow has one.
//
// dex:field attribute-key:amplitude-active-users value-type:json editable:false description:"Amplitude active user counts"
func (*ActiveUsersFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	counts, err := readActiveUserCounts(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"amplitude-active-users": counts}}, nil
}

func readActiveUserCounts(ctx dex.Context) (amplitude.ActiveUserCounts, error) {
	counts, err := activeUsersAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return amplitude.ActiveUserCounts{}, nil
	}
	return counts, err
}

// GetSteps registers the start, provider count, and completion Steps.
func (flow *ActiveUsersFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startActiveUsers{}),
		dex.DefineStep(amplitude.NewGetActiveUserCountsStep(amplitude.GetActiveUserCountsStepConfig[amplitude.ActiveUserCountsInput]{
			StepType: activeUsersStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Amplitude", Explanation: "Read active user counts for the requested date range."},
			Connection:  flow.connection, MapToOperationInput: mapToActiveUsersInput,
			Counted: sdkgo.GoTo(completeActiveUsers{}),
		})),
		dex.DefineStep(completeActiveUsers{}),
	}
}

func mapToActiveUsersInput(input amplitude.ActiveUserCountsInput) amplitude.ActiveUserCountsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the date range to the Amplitude provider Step."
type startActiveUsers struct{ dex.StepDefaults }

func (startActiveUsers) GetStepType() string { return "StartGetActiveUserCounts" }
func (startActiveUsers) WaitFor(dex.Context, amplitude.ActiveUserCountsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startActiveUsers) Execute(_ dex.Context, input amplitude.ActiveUserCountsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[amplitude.ActiveUserCountsInput](activeUsersStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the active user count series."
type completeActiveUsers struct {
	dex.StepDefaultsNoWaitFor[amplitude.GetActiveUserCountsResult]
}

func (completeActiveUsers) GetStepType() string { return "CompleteGetActiveUserCounts" }
func (completeActiveUsers) Execute(ctx dex.Context, result amplitude.GetActiveUserCountsResult) (*dex.StepDecision, error) {
	if err := activeUsersAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*ActiveUsersFlow)(nil)
