// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/activecampaign/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const createDealStepType = "CallCreateDeal"

var createdDealAttribute = dex.DefineAttribute[activecampaign.DealMetadata]("activecampaign-created-deal")

// CreateDealFlow demonstrates a bounded deal create through a Dex Step.
type CreateDealFlow struct {
	dex.FlowDefaults
	connection activecampaign.Connection
}

// NewCreateDealFlow binds the same static connection as the inspection Flow.
func NewCreateDealFlow(connection activecampaign.Connection) *CreateDealFlow {
	return &CreateDealFlow{connection: connection}
}

// GetFlowType is the stable create example identity.
func (*CreateDealFlow) GetFlowType() string { return "ActiveCampaignCreateDeal" }

// GetPersistenceSchema registers the created deal displayed in Dex Web.
func (*CreateDealFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdDealAttribute}}
}

// GetSteps registers the start, provider call, and completion boundary.
func (flow *CreateDealFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startCreateDeal{}),
		dex.DefineStep(activecampaign.NewCreateDealStep(activecampaign.CreateDealStepConfig[activecampaign.CreateDealInput]{
			StepType: createDealStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "ActiveCampaign", Explanation: "Create one deal for a contact in a pipeline stage."},
			Connection:  flow.connection, MapToOperationInput: MapToCreateDealInput,
			Created: sdkgo.GoTo(completeCreateDeal{}),
		})),
		dex.DefineStep(completeCreateDeal{}),
	}
}

// GetRPCs exposes the stored deal in Dex Web.
func (flow *CreateDealFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:activecampaign-created-deal value-type:json editable:false description:"Created deal"
// GetDexSummary shows the created deal in Dex Web.
func (*CreateDealFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	deal, err := readCreatedDeal(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"activecampaign-created-deal": deal}}, nil
}

// dex:field attribute-key:activecampaign-created-deal value-type:json editable:false description:"Created deal"
// GetDexDisplay shows the created deal in Dex Web.
func (*CreateDealFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	deal, err := readCreatedDeal(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"activecampaign-created-deal": deal}}, nil
}

func readCreatedDeal(ctx dex.Context) (activecampaign.DealMetadata, error) {
	deal, err := createdDealAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return activecampaign.DealMetadata{}, nil
	}
	return deal, err
}

// MapToCreateDealInput preserves the typed start input for the provider call.
func MapToCreateDealInput(input activecampaign.CreateDealInput) activecampaign.CreateDealInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the deal fields to the create Step."
type startCreateDeal struct{ dex.StepDefaults }

func (startCreateDeal) GetStepType() string { return "StartCreateDeal" }
func (startCreateDeal) WaitFor(dex.Context, activecampaign.CreateDealInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startCreateDeal) Execute(_ dex.Context, input activecampaign.CreateDealInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[activecampaign.CreateDealInput](createDealStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the created contact deal metadata."
type completeCreateDeal struct {
	dex.StepDefaultsNoWaitFor[activecampaign.CreateDealResult]
}

func (completeCreateDeal) GetStepType() string { return "CompleteCreateDeal" }
func (completeCreateDeal) Execute(ctx dex.Context, result activecampaign.CreateDealResult) (*dex.StepDecision, error) {
	if err := createdDealAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CreateDealFlow)(nil)
