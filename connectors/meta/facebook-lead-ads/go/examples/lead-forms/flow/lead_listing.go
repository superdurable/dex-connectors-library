// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package leadforms

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/meta/facebook-lead-ads/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const leadListingStepType = "CallListLeads"

var leadListingAttribute = dex.DefineAttribute[facebookleadads.LeadsPage]("facebook-lead-ads-leads-page")

// LeadListingFlow lists lead identities from one form.
type LeadListingFlow struct {
	dex.FlowDefaults
	connection facebookleadads.Connection
}

// NewLeadListingFlow binds the static Facebook Lead Ads connection.
func NewLeadListingFlow(connection facebookleadads.Connection) *LeadListingFlow {
	return &LeadListingFlow{connection: connection}
}

// GetFlowType identifies the example lead listing Flow.
func (*LeadListingFlow) GetFlowType() string { return "FacebookLeadAdsLeadListing" }

// GetPersistenceSchema registers the durable lead page.
func (*LeadListingFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{leadListingAttribute}}
}

// GetSteps wires the operation-specific lead listing Query.
func (flow *LeadListingFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startLeadListing{}),
		dex.DefineStep(facebookleadads.NewListLeadsStep(facebookleadads.ListLeadsStepConfig[facebookleadads.ListLeadsInput]{
			StepType: leadListingStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Facebook Lead Ads", Explanation: "Read lead identities from a form."},
			Connection:  flow.connection, MapToOperationInput: leadListingInput,
			Listed: sdkgo.GoTo(completeLeadListing{}),
		})),
		dex.DefineStep(completeLeadListing{}),
	}
}

// GetRPCs exposes the lead page in Dex Web.
func (flow *LeadListingFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:facebook-lead-ads-leads-page value-type:json editable:false description:"Lead identity page"
// GetDexSummary returns the last lead page.
func (*LeadListingFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	leads, err := readLeadListing(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"facebook-lead-ads-leads-page": leads}}, nil
}

// dex:field attribute-key:facebook-lead-ads-leads-page value-type:json editable:false description:"Lead identity page"
// GetDexDisplay returns the last lead page.
func (*LeadListingFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	leads, err := readLeadListing(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"facebook-lead-ads-leads-page": leads}}, nil
}

func readLeadListing(ctx dex.Context) (facebookleadads.LeadsPage, error) {
	leads, err := leadListingAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return facebookleadads.LeadsPage{}, nil
	}
	return leads, err
}

func leadListingInput(input facebookleadads.ListLeadsInput) facebookleadads.ListLeadsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the form ID and cursor to the provider Step."
type startLeadListing struct{ dex.StepDefaults }

func (startLeadListing) GetStepType() string { return "StartLeadListing" }
func (startLeadListing) WaitFor(dex.Context, facebookleadads.ListLeadsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startLeadListing) Execute(_ dex.Context, input facebookleadads.ListLeadsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[facebookleadads.ListLeadsInput](leadListingStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Save the lead page and complete the Flow."
type completeLeadListing struct {
	dex.StepDefaultsNoWaitFor[facebookleadads.ListLeadsResult]
}

func (completeLeadListing) GetStepType() string { return "CompleteLeadListing" }
func (completeLeadListing) Execute(ctx dex.Context, result facebookleadads.ListLeadsResult) (*dex.StepDecision, error) {
	if err := leadListingAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*LeadListingFlow)(nil)
