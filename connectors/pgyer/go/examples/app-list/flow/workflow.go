// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package applist runs Pgyer listUploadedApps as one Dex Flow.
package applist

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/pgyer/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "PgyerUploadedApps"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "pgyer"
	operationStepType = "CallListUploadedApps"
)

var uploadedAppListOutcomeAttribute = dex.DefineAttribute[pgyer.UploadedAppPage]("pgyer-app-list-outcome")

// Flow runs ListUploadedApps once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection pgyer.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection pgyer.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{uploadedAppListOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(pgyer.NewListUploadedAppsStep(pgyer.ListUploadedAppsStepConfig[pgyer.ListUploadedAppsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Pgyer", Explanation: "List app and build metadata uploaded to Pgyer."},
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

// dex:field attribute-key:pgyer-app-list-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readUploadedAppsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"pgyer-app-list-outcome": outcome}}, nil
}

// dex:field attribute-key:pgyer-app-list-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readUploadedAppsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"pgyer-app-list-outcome": outcome}}, nil
}

func readUploadedAppsOutcome(ctx dex.Context) (pgyer.UploadedAppPage, error) {
	outcome, err := uploadedAppListOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return pgyer.UploadedAppPage{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input pgyer.ListUploadedAppsInput) pgyer.ListUploadedAppsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListUploadedApps" }
func (startRequest) WaitFor(dex.Context, pgyer.ListUploadedAppsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input pgyer.ListUploadedAppsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[pgyer.ListUploadedAppsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[pgyer.ListUploadedAppsResult]
}

func (completeResult) GetStepType() string { return "CompleteListUploadedApps" }
func (completeResult) Execute(ctx dex.Context, result pgyer.ListUploadedAppsResult) (*dex.StepDecision, error) {
	if err := uploadedAppListOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
