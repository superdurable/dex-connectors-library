// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package createdomainclaim demonstrates Resend domain claim creation.
package createdomainclaim

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/resend/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is this example's stable Flow identity.
	FlowType = "ResendCreateDomainClaim"
	// ConnectionName is the static Dex Web connection name.
	ConnectionName = "resend"
)

// Request supplies the new domain claim settings.
type Request resend.CreateDomainClaimInput

var outcomeAttribute = dex.DefineAttribute[resend.DomainClaim]("resend-domain-claim-created")

// Flow creates one Resend domain claim and completes with its ID.
type Flow struct {
	dex.FlowDefaults
	connection resend.Connection
}

// NewFlow binds a Resend connection.
func NewFlow(connection resend.Connection) *Flow { return &Flow{connection: connection} }

// GetFlowType returns the stable Flow identity.
func (*Flow) GetFlowType() string { return FlowType }

// GetRPCs exposes Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetPersistenceSchema registers the verification result.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{outcomeAttribute}}
}

// GetDexSummary returns the created claim ID.
// dex:field attribute-key:resend-domain-claim-created value-type:json editable:false description:"Created domain claim"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readDomainClaimOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"resend-domain-claim-created": value}}, nil
}

// GetDexDisplay returns the created claim ID.
// dex:field attribute-key:resend-domain-claim-created value-type:json editable:false description:"Created domain claim"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readDomainClaimOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"resend-domain-claim-created": value}}, nil
}

func readDomainClaimOutcome(ctx dex.Context) (resend.DomainClaim, error) {
	value, err := outcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return resend.DomainClaim{}, nil
	}
	return value, err
}

// GetSteps registers the domain claim creation path.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(prepareDomainClaim{}),
		dex.DefineStep(resend.NewCreateDomainClaimStep(resend.CreateDomainClaimStepConfig[Request]{
			StepType: "CreateDomainClaim", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "resend-domain-claim", GroupLabel: "Resend domain claim", Explanation: "Create the selected Resend domain claim."},
			MapToOperationInput: func(request Request) resend.CreateDomainClaimInput { return resend.CreateDomainClaimInput(request) },
			Created:             sdkgo.GoTo(completeDomainClaim{}),
		})),
		dex.DefineStep(completeDomainClaim{}),
	}
}

// dex:group group-id:resend-domain-claim-prepare group-label:"Prepare domain claim"
// dex:explanation text:"Pass the typed claim request into the provider create."
type prepareDomainClaim struct{ dex.StepDefaults }

func (prepareDomainClaim) GetStepType() string { return "PrepareDomainClaim" }
func (prepareDomainClaim) WaitFor(dex.Context, Request) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (prepareDomainClaim) Execute(_ dex.Context, request Request) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[Request]("CreateDomainClaim"), request), nil
}

// dex:group group-id:resend-domain-claim-complete group-label:"Complete domain claim creation"
// dex:explanation text:"Complete after Resend creates the claim."
type completeDomainClaim struct {
	dex.StepDefaultsNoWaitFor[resend.CreateDomainClaimResult]
}

func (completeDomainClaim) GetStepType() string { return "CompleteDomainClaim" }
func (completeDomainClaim) Execute(ctx dex.Context, result resend.CreateDomainClaimResult) (*dex.StepDecision, error) {
	if err := outcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
