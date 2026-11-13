// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package companyprofile runs Tianyancha getCompanyProfile as one Dex Flow.
package companyprofile

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/tianyancha/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "TianyanchaCompanyProfile"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "tianyancha"
	operationStepType = "CallGetCompanyProfile"
)

var companyProfileOutcomeAttribute = dex.DefineAttribute[tianyancha.GetCompanyProfileOutput]("tianyancha-get-company-profile-outcome")

// Flow runs GetCompanyProfile once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection tianyancha.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection tianyancha.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{companyProfileOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(tianyancha.NewGetCompanyProfileStep(tianyancha.GetCompanyProfileStepConfig[tianyancha.GetCompanyProfileInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Tianyancha", Explanation: "Read Tianyancha company registration facts for the supplied keyword."},
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

// dex:field attribute-key:tianyancha-get-company-profile-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readGetCompanyProfileOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"tianyancha-get-company-profile-outcome": outcome}}, nil
}

// dex:field attribute-key:tianyancha-get-company-profile-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readGetCompanyProfileOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"tianyancha-get-company-profile-outcome": outcome}}, nil
}

func readGetCompanyProfileOutcome(ctx dex.Context) (tianyancha.GetCompanyProfileOutput, error) {
	outcome, err := companyProfileOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return tianyancha.GetCompanyProfileOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input tianyancha.GetCompanyProfileInput) tianyancha.GetCompanyProfileInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetCompanyProfile" }
func (startRequest) WaitFor(dex.Context, tianyancha.GetCompanyProfileInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input tianyancha.GetCompanyProfileInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[tianyancha.GetCompanyProfileInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[tianyancha.GetCompanyProfileResult]
}

func (completeResult) GetStepType() string { return "CompleteGetCompanyProfile" }
func (completeResult) Execute(ctx dex.Context, result tianyancha.GetCompanyProfileResult) (*dex.StepDecision, error) {
	if err := companyProfileOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
