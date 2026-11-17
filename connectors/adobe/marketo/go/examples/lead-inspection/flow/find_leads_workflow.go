// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package leadinspection

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/adobe/marketo/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// FindLeadsFlowType is the stable lead search Flow identity.
const FindLeadsFlowType = "MarketoFindLeadsByEmail"
const findLeadsStepType = "CallFindLeadsByEmail"

var leadProfilesPageAttribute = dex.DefineAttribute[marketo.LeadProfilesPage]("marketo-lead-profiles-page")

// FindLeadsFlow searches one page by exact email and stores the result.
type FindLeadsFlow struct {
	dex.FlowDefaults
	connection marketo.Connection
}

// NewFindLeadsFlow binds the provider connection at registration.
func NewFindLeadsFlow(connection marketo.Connection) *FindLeadsFlow {
	return &FindLeadsFlow{connection: connection}
}
func (*FindLeadsFlow) GetFlowType() string { return FindLeadsFlowType }
func (*FindLeadsFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{leadProfilesPageAttribute}}
}
func (flow *FindLeadsFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startFindLeads{}),
		dex.DefineStep(marketo.NewFindLeadsByEmailStep(marketo.FindLeadsByEmailStepConfig[marketo.FindLeadsByEmailInput]{
			StepType: findLeadsStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Marketo", Explanation: "Find one page of lead profiles by email."},
			Connection:  flow.connection, MapToOperationInput: mapFindLeadsInput,
			Found: sdkgo.GoTo(completeFindLeads{}),
		})),
		dex.DefineStep(completeFindLeads{}),
	}
}
func (flow *FindLeadsFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:marketo-lead-profiles-page value-type:json editable:false description:"Matching lead profiles"
// GetDexSummary shows the matching page.
func (*FindLeadsFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readLeadProfilesPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"marketo-lead-profiles-page": page}}, nil
}

// dex:field attribute-key:marketo-lead-profiles-page value-type:json editable:false description:"Matching lead profiles"
// GetDexDisplay shows the matching page.
func (*FindLeadsFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readLeadProfilesPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"marketo-lead-profiles-page": page}}, nil
}
func readLeadProfilesPage(ctx dex.Context) (marketo.LeadProfilesPage, error) {
	page, err := leadProfilesPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return marketo.LeadProfilesPage{}, nil
	}
	return page, err
}
func mapFindLeadsInput(input marketo.FindLeadsByEmailInput) marketo.FindLeadsByEmailInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the email search input to the provider Step."
type startFindLeads struct{ dex.StepDefaults }

func (startFindLeads) GetStepType() string { return "StartFindLeadsByEmail" }
func (startFindLeads) WaitFor(dex.Context, marketo.FindLeadsByEmailInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startFindLeads) Execute(_ dex.Context, input marketo.FindLeadsByEmailInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[marketo.FindLeadsByEmailInput](findLeadsStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Store the lead page and complete the Flow."
type completeFindLeads struct {
	dex.StepDefaultsNoWaitFor[marketo.FindLeadsByEmailResult]
}

func (completeFindLeads) GetStepType() string { return "CompleteFindLeadsByEmail" }
func (completeFindLeads) Execute(ctx dex.Context, result marketo.FindLeadsByEmailResult) (*dex.StepDecision, error) {
	if err := leadProfilesPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*FindLeadsFlow)(nil)
