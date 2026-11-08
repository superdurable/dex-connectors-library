// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package leadforms runs Facebook Lead Ads listLeadForms as one Dex Flow.
package leadforms

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/meta/facebook-lead-ads/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "FacebookLeadAdsLeadForms"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "facebook-lead-ads"
	operationStepType = "CallListLeadForms"
)

var leadFormsPageAttribute = dex.DefineAttribute[facebookleadads.LeadFormsPage]("facebook-lead-ads-lead-forms-page")

// Flow runs ListLeadForms once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection facebookleadads.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection facebookleadads.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{leadFormsPageAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(facebookleadads.NewListLeadFormsStep(facebookleadads.ListLeadFormsStepConfig[facebookleadads.ListLeadFormsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Facebook Lead Ads", Explanation: "Call the provider's listLeadForms operation."},
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

// dex:field attribute-key:facebook-lead-ads-lead-forms-page value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readLeadFormsPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"facebook-lead-ads-lead-forms-page": outcome}}, nil
}

// dex:field attribute-key:facebook-lead-ads-lead-forms-page value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readLeadFormsPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"facebook-lead-ads-lead-forms-page": outcome}}, nil
}

func readLeadFormsPage(ctx dex.Context) (facebookleadads.LeadFormsPage, error) {
	outcome, err := leadFormsPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return facebookleadads.LeadFormsPage{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input facebookleadads.ListLeadFormsInput) facebookleadads.ListLeadFormsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListLeadForms" }
func (startRequest) WaitFor(dex.Context, facebookleadads.ListLeadFormsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input facebookleadads.ListLeadFormsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[facebookleadads.ListLeadFormsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[facebookleadads.ListLeadFormsResult]
}

func (completeResult) GetStepType() string { return "CompleteListLeadForms" }
func (completeResult) Execute(ctx dex.Context, result facebookleadads.ListLeadFormsResult) (*dex.StepDecision, error) {
	if err := leadFormsPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
