// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package createrelease demonstrates bounded Sentry release creation.
package createrelease

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/sentry/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is this example's stable Flow identity.
	FlowType = "SentryCreateRelease"
	// ConnectionName is the static Dex Web connection name.
	ConnectionName = "sentry"
)

var outcomeAttribute = dex.DefineAttribute[sentry.Release]("sentry-created-release")

// Request identifies the project and desired release version.
type Request sentry.CreateReleaseInput

// Flow creates one release and completes with its new metadata.
type Flow struct {
	dex.FlowDefaults
	connection sentry.Connection
}

// NewFlow binds a Sentry connection.
func NewFlow(connection sentry.Connection) *Flow { return &Flow{connection: connection} }

// GetFlowType returns the stable Flow identity.
func (*Flow) GetFlowType() string { return FlowType }

// GetPersistenceSchema registers the created release result.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{outcomeAttribute}}
}

// GetRPCs exposes Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the created release.
// dex:field attribute-key:sentry-created-release value-type:json editable:false description:"Created release"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readCreatedRelease(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"sentry-created-release": value}}, nil
}

// GetDexDisplay returns the created release.
// dex:field attribute-key:sentry-created-release value-type:json editable:false description:"Created release"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readCreatedRelease(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"sentry-created-release": value}}, nil
}
func readCreatedRelease(ctx dex.Context) (sentry.Release, error) {
	value, err := outcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return sentry.Release{}, nil
	}
	return value, err
}

// GetSteps registers the release creation path.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(prepareRelease{}),
		dex.DefineStep(sentry.NewCreateReleaseStep(sentry.CreateReleaseStepConfig[Request]{
			StepType: "CreateRelease", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "sentry-release", GroupLabel: "Sentry release", Explanation: "Create the release for the selected Sentry project."},
			MapToOperationInput: func(request Request) sentry.CreateReleaseInput { return sentry.CreateReleaseInput(request) },
			Created:             sdkgo.GoTo(completeRelease{}),
		})),
		dex.DefineStep(completeRelease{}),
	}
}

// dex:group group-id:sentry-release-prepare group-label:"Prepare release"
// dex:explanation text:"Pass the typed release request into the provider creation."
type prepareRelease struct{ dex.StepDefaults }

func (prepareRelease) GetStepType() string { return "PrepareRelease" }
func (prepareRelease) WaitFor(dex.Context, Request) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (prepareRelease) Execute(_ dex.Context, request Request) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[Request]("CreateRelease"), request), nil
}

// dex:group group-id:sentry-release-complete group-label:"Complete release creation"
// dex:explanation text:"Complete with Sentry's created release metadata."
type completeRelease struct {
	dex.StepDefaultsNoWaitFor[sentry.CreateReleaseResult]
}

func (completeRelease) GetStepType() string { return "CompleteRelease" }
func (completeRelease) Execute(ctx dex.Context, result sentry.CreateReleaseResult) (*dex.StepDecision, error) {
	if err := outcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
