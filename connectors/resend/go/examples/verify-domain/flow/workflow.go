// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package verifydomain demonstrates Resend's asynchronous DNS verification.
package verifydomain

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/resend/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is this example's stable Flow identity.
	FlowType = "ResendVerifyDomain"
	// ConnectionName is the static Dex Web connection name.
	ConnectionName = "resend"
)

// Request supplies the domain ID to verify.
type Request resend.VerifyDomainInput

var outcomeAttribute = dex.DefineAttribute[resend.DomainReference]("resend-verification-started")

// Flow starts verification for one Resend domain and completes with its ID.
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

// GetDexSummary returns the domain ID whose verification started.
// dex:field attribute-key:resend-verification-started value-type:json editable:false description:"Domain verification started"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readDomainOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"resend-verification-started": value}}, nil
}

// GetDexDisplay returns the domain ID whose verification started.
// dex:field attribute-key:resend-verification-started value-type:json editable:false description:"Domain verification started"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readDomainOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"resend-verification-started": value}}, nil
}

func readDomainOutcome(ctx dex.Context) (resend.DomainReference, error) {
	value, err := outcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return resend.DomainReference{}, nil
	}
	return value, err
}

// GetSteps registers the domain verify path.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(prepareDomain{}),
		dex.DefineStep(resend.NewVerifyDomainStep(resend.VerifyDomainStepConfig[Request]{
			StepType: "VerifyDomain", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "resend-domain", GroupLabel: "Resend domain", Explanation: "Start a DNS verification cycle for the selected Resend domain."},
			MapToOperationInput: func(request Request) resend.VerifyDomainInput { return resend.VerifyDomainInput(request) },
			VerificationStarted:             sdkgo.GoTo(completeDomain{}),
		})),
		dex.DefineStep(completeDomain{}),
	}
}

// dex:group group-id:resend-domain-prepare group-label:"Prepare domain"
// dex:explanation text:"Pass the typed domain request into the provider verify."
type prepareDomain struct{ dex.StepDefaults }

func (prepareDomain) GetStepType() string { return "PrepareDomain" }
func (prepareDomain) WaitFor(dex.Context, Request) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (prepareDomain) Execute(_ dex.Context, request Request) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[Request]("VerifyDomain"), request), nil
}

// dex:group group-id:resend-domain-complete group-label:"Complete verification request"
// dex:explanation text:"Complete after Resend accepts the verification request."
type completeDomain struct {
	dex.StepDefaultsNoWaitFor[resend.VerifyDomainResult]
}

func (completeDomain) GetStepType() string { return "CompleteDomain" }
func (completeDomain) Execute(ctx dex.Context, result resend.VerifyDomainResult) (*dex.StepDecision, error) {
	if err := outcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
