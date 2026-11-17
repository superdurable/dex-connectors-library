// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package createorganization demonstrates bounded Sentry organization creation.
package createorganization

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/sentry/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is this example's stable Flow identity.
	FlowType = "SentryCreateOrganization"
	// ConnectionName is the static Dex Web connection name.
	ConnectionName = "sentry"
)

var outcomeAttribute = dex.DefineAttribute[sentry.Organization]("sentry-created-organization")

// Request identifies the desired organization slug and name.
type Request sentry.CreateOrganizationInput

// Flow creates one organization and completes with its new metadata.
type Flow struct {
	dex.FlowDefaults
	connection sentry.Connection
}

// NewFlow binds a Sentry connection.
func NewFlow(connection sentry.Connection) *Flow { return &Flow{connection: connection} }

// GetFlowType returns the stable Flow identity.
func (*Flow) GetFlowType() string { return FlowType }

// GetPersistenceSchema registers the created organization result.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{outcomeAttribute}}
}

// GetRPCs exposes Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the created organization.
// dex:field attribute-key:sentry-created-organization value-type:json editable:false description:"Created organization"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readCreatedOrganization(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"sentry-created-organization": value}}, nil
}

// GetDexDisplay returns the created organization.
// dex:field attribute-key:sentry-created-organization value-type:json editable:false description:"Created organization"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readCreatedOrganization(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"sentry-created-organization": value}}, nil
}
func readCreatedOrganization(ctx dex.Context) (sentry.Organization, error) {
	value, err := outcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return sentry.Organization{}, nil
	}
	return value, err
}

// GetSteps registers the organization creation path.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(prepareOrganization{}),
		dex.DefineStep(sentry.NewCreateOrganizationStep(sentry.CreateOrganizationStepConfig[Request]{
			StepType: "CreateOrganization", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "sentry-organization", GroupLabel: "Sentry organization", Explanation: "Create a named organization for the connected Sentry user."},
			MapToOperationInput: func(request Request) sentry.CreateOrganizationInput { return sentry.CreateOrganizationInput(request) },
			Created:             sdkgo.GoTo(completeOrganization{}),
		})),
		dex.DefineStep(completeOrganization{}),
	}
}

// dex:group group-id:sentry-organization-prepare group-label:"Prepare organization"
// dex:explanation text:"Pass the typed organization request into the provider creation."
type prepareOrganization struct{ dex.StepDefaults }

func (prepareOrganization) GetStepType() string { return "PrepareOrganization" }
func (prepareOrganization) WaitFor(dex.Context, Request) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (prepareOrganization) Execute(_ dex.Context, request Request) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[Request]("CreateOrganization"), request), nil
}

// dex:group group-id:sentry-organization-complete group-label:"Complete organization creation"
// dex:explanation text:"Complete with Sentry's created organization metadata."
type completeOrganization struct {
	dex.StepDefaultsNoWaitFor[sentry.CreateOrganizationResult]
}

func (completeOrganization) GetStepType() string { return "CompleteOrganization" }
func (completeOrganization) Execute(ctx dex.Context, result sentry.CreateOrganizationResult) (*dex.StepDecision, error) {
	if err := outcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
