// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package identitylookup runs Amazon SES getEmailIdentity as one Dex Flow.
package identitylookup

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/amazon/ses/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "AmazonSESIdentityLookup"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "amazon-ses"
	operationStepType = "CallGetEmailIdentity"
)

var identityAttribute = dex.DefineAttribute[ses.EmailIdentityDetails]("ses-identity-lookup")

// Flow runs GetEmailIdentity once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection ses.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection ses.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{identityAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(ses.NewGetEmailIdentityStep(ses.GetEmailIdentityStepConfig[ses.GetEmailIdentityInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Amazon SES", Explanation: "Read one SES email identity verification status."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Found:               sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:ses-identity-lookup value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readEmailIdentityDetails(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"ses-identity-lookup": outcome}}, nil
}

// dex:field attribute-key:ses-identity-lookup value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readEmailIdentityDetails(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"ses-identity-lookup": outcome}}, nil
}

func readEmailIdentityDetails(ctx dex.Context) (ses.EmailIdentityDetails, error) {
	outcome, err := identityAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return ses.EmailIdentityDetails{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input ses.GetEmailIdentityInput) ses.GetEmailIdentityInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetEmailIdentity" }
func (startRequest) WaitFor(dex.Context, ses.GetEmailIdentityInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input ses.GetEmailIdentityInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[ses.GetEmailIdentityInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[ses.GetEmailIdentityResult]
}

func (completeResult) GetStepType() string { return "CompleteGetEmailIdentity" }
func (completeResult) Execute(ctx dex.Context, result ses.GetEmailIdentityResult) (*dex.StepDecision, error) {
	if err := identityAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
