// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package leadinspection

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/adobe/marketo/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// GetOpportunityByExternalIDFlowType is the stable opportunity lookup Flow identity.
const GetOpportunityByExternalIDFlowType = "MarketoGetOpportunityByExternalID"
const getOpportunityByExternalIDStepType = "CallGetOpportunityByExternalID"

var opportunityProfileAttribute = dex.DefineAttribute[marketo.OpportunityProfile]("marketo-opportunity-profile")

// GetOpportunityByExternalIDFlow reads a opportunity by external ID and stores its profile.
type GetOpportunityByExternalIDFlow struct {
	dex.FlowDefaults
	connection marketo.Connection
}

// NewGetOpportunityByExternalIDFlow binds the provider connection at registration.
func NewGetOpportunityByExternalIDFlow(connection marketo.Connection) *GetOpportunityByExternalIDFlow {
	return &GetOpportunityByExternalIDFlow{connection: connection}
}
func (*GetOpportunityByExternalIDFlow) GetFlowType() string {
	return GetOpportunityByExternalIDFlowType
}
func (*GetOpportunityByExternalIDFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{opportunityProfileAttribute}}
}
func (flow *GetOpportunityByExternalIDFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startGetOpportunityByExternalID{}),
		dex.DefineStep(marketo.NewGetOpportunityByExternalIDStep(marketo.GetOpportunityByExternalIDStepConfig[marketo.GetOpportunityByExternalIDInput]{
			StepType: getOpportunityByExternalIDStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Marketo", Explanation: "Read one opportunity profile by external ID."},
			Connection:  flow.connection, MapToOperationInput: mapGetOpportunityByExternalIDInput,
			Found: sdkgo.GoTo(completeGetOpportunityByExternalID{}),
		})),
		dex.DefineStep(completeGetOpportunityByExternalID{}),
	}
}
func (flow *GetOpportunityByExternalIDFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:marketo-opportunity-profile value-type:json editable:false description:"Matching opportunity profile"
// GetDexSummary shows the matching page.
func (*GetOpportunityByExternalIDFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readOpportunityProfile(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"marketo-opportunity-profile": page}}, nil
}

// dex:field attribute-key:marketo-opportunity-profile value-type:json editable:false description:"Matching opportunity profile"
// GetDexDisplay shows the matching page.
func (*GetOpportunityByExternalIDFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readOpportunityProfile(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"marketo-opportunity-profile": page}}, nil
}
func readOpportunityProfile(ctx dex.Context) (marketo.OpportunityProfile, error) {
	page, err := opportunityProfileAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return marketo.OpportunityProfile{}, nil
	}
	return page, err
}
func mapGetOpportunityByExternalIDInput(input marketo.GetOpportunityByExternalIDInput) marketo.GetOpportunityByExternalIDInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the email search input to the provider Step."
type startGetOpportunityByExternalID struct{ dex.StepDefaults }

func (startGetOpportunityByExternalID) GetStepType() string { return "StartGetOpportunityByExternalID" }
func (startGetOpportunityByExternalID) WaitFor(dex.Context, marketo.GetOpportunityByExternalIDInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startGetOpportunityByExternalID) Execute(_ dex.Context, input marketo.GetOpportunityByExternalIDInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[marketo.GetOpportunityByExternalIDInput](getOpportunityByExternalIDStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Store the opportunity profile and complete the Flow."
type completeGetOpportunityByExternalID struct {
	dex.StepDefaultsNoWaitFor[marketo.GetOpportunityByExternalIDResult]
}

func (completeGetOpportunityByExternalID) GetStepType() string {
	return "CompleteGetOpportunityByExternalID"
}
func (completeGetOpportunityByExternalID) Execute(ctx dex.Context, result marketo.GetOpportunityByExternalIDResult) (*dex.StepDecision, error) {
	if err := opportunityProfileAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*GetOpportunityByExternalIDFlow)(nil)
