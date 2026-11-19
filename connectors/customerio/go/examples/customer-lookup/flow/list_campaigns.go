// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package customerlookup

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/customerio/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// campaignsFlowType is the stable example Flow identity.
	campaignsFlowType = "CustomerioCampaignSearch"
	campaignsStepType = "CallListCampaigns"
)

var campaignsAttribute = dex.DefineAttribute[customerio.CampaignsPage]("customerio-campaigns")

// CampaignsSearchFlow runs ListCampaigns once and completes with its typed output.
type CampaignsSearchFlow struct {
	dex.FlowDefaults
	connection customerio.Connection
}

// NewCampaignsSearchFlow binds the provider connection at registration time.
func NewCampaignsSearchFlow(connection customerio.Connection) *CampaignsSearchFlow {
	return &CampaignsSearchFlow{connection: connection}
}

func (*CampaignsSearchFlow) GetFlowType() string { return campaignsFlowType }

func (*CampaignsSearchFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{campaignsAttribute}}
}

func (flow *CampaignsSearchFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startCampaignsSearchFlow{}),
		dex.DefineStep(customerio.NewListCampaignsStep(customerio.ListCampaignsStepConfig[customerio.ListCampaignsInput]{
			StepType: campaignsStepType, ConnectionName: ConnectionName,
			Annotations:                 sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Customer.io", Explanation: "Find campaigns in Customer.io."},
			Connection:                  flow.connection,
			MapToOperationInput: mapCampaignsSearchFlowInput,
			Listed:                      sdkgo.GoTo(completeCampaignsSearchFlow{}),
		})),
		dex.DefineStep(completeCampaignsSearchFlow{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *CampaignsSearchFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:customerio-campaigns value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*CampaignsSearchFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readCampaigns(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"customerio-campaigns": outcome}}, nil
}

// dex:field attribute-key:customerio-campaigns value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*CampaignsSearchFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readCampaigns(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"customerio-campaigns": outcome}}, nil
}

func readCampaigns(ctx dex.Context) (customerio.CampaignsPage, error) {
	outcome, err := campaignsAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return customerio.CampaignsPage{}, nil
	}
	return outcome, err
}

// mapCampaignsSearchFlowInput preserves the validated typed start input for the provider call.
func mapCampaignsSearchFlowInput(input customerio.ListCampaignsInput) customerio.ListCampaignsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startCampaignsSearchFlow struct{ dex.StepDefaults }

func (startCampaignsSearchFlow) GetStepType() string { return "StartListCampaigns" }
func (startCampaignsSearchFlow) WaitFor(dex.Context, customerio.ListCampaignsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startCampaignsSearchFlow) Execute(_ dex.Context, input customerio.ListCampaignsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[customerio.ListCampaignsInput](campaignsStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeCampaignsSearchFlow struct {
	dex.StepDefaultsNoWaitFor[customerio.ListCampaignsResult]
}

func (completeCampaignsSearchFlow) GetStepType() string { return "CompleteListCampaigns" }
func (completeCampaignsSearchFlow) Execute(ctx dex.Context, result customerio.ListCampaignsResult) (*dex.StepDecision, error) {
	if err := campaignsAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CampaignsSearchFlow)(nil)
