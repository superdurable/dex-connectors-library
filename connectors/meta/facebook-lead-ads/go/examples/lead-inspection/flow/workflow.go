// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package leadinspection runs Facebook Lead Ads getLead as one Dex Flow.
package leadinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/meta/facebook-lead-ads/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "FacebookLeadAdsLeadInspection"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "facebook-lead-ads"
	operationStepType = "CallGetLead"
)

var leadMetadataAttribute = dex.DefineAttribute[facebookleadads.LeadMetadata]("facebook-lead-ads-lead-metadata")

// Flow runs GetLead once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection facebookleadads.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection facebookleadads.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{leadMetadataAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(facebookleadads.NewGetLeadStep(facebookleadads.GetLeadStepConfig[facebookleadads.GetLeadInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Facebook Lead Ads", Explanation: "Call the provider's getLead operation."},
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

// dex:field attribute-key:facebook-lead-ads-lead-metadata value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readLeadMetadata(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"facebook-lead-ads-lead-metadata": outcome}}, nil
}

// dex:field attribute-key:facebook-lead-ads-lead-metadata value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readLeadMetadata(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"facebook-lead-ads-lead-metadata": outcome}}, nil
}

func readLeadMetadata(ctx dex.Context) (facebookleadads.LeadMetadata, error) {
	outcome, err := leadMetadataAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return facebookleadads.LeadMetadata{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input facebookleadads.GetLeadInput) facebookleadads.GetLeadInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetLead" }
func (startRequest) WaitFor(dex.Context, facebookleadads.GetLeadInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input facebookleadads.GetLeadInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[facebookleadads.GetLeadInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[facebookleadads.GetLeadResult]
}

func (completeResult) GetStepType() string { return "CompleteGetLead" }
func (completeResult) Execute(ctx dex.Context, result facebookleadads.GetLeadResult) (*dex.StepDecision, error) {
	if err := leadMetadataAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
