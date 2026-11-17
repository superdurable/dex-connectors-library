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
	// campaignMetricsFlowType is the stable example Flow identity.
	campaignMetricsFlowType = "CustomerioCampaignMetrics"
	campaignMetricsStepType = "CallGetCampaignMetrics"
)

var campaignMetricsAttribute = dex.DefineAttribute[customerio.CampaignMetrics]("customerio-campaignmetrics")

// CampaignMetricsFlow runs GetCampaignMetrics once and completes with its typed output.
type CampaignMetricsFlow struct {
	dex.FlowDefaults
	connection customerio.Connection
}

// NewCampaignMetricsFlow binds the provider connection at registration time.
func NewCampaignMetricsFlow(connection customerio.Connection) *CampaignMetricsFlow {
	return &CampaignMetricsFlow{connection: connection}
}

func (*CampaignMetricsFlow) GetFlowType() string { return campaignMetricsFlowType }

func (*CampaignMetricsFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{campaignMetricsAttribute}}
}

func (flow *CampaignMetricsFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startCampaignMetricsFlow{}),
		dex.DefineStep(customerio.NewGetCampaignMetricsStep(customerio.GetCampaignMetricsStepConfig[customerio.GetCampaignMetricsInput]{
			StepType: campaignMetricsStepType, ConnectionName: ConnectionName,
			Annotations:                 sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Customer.io", Explanation: "Find campaignMetrics in Customer.io."},
			Connection:                  flow.connection,
			MapToOperationInput: mapCampaignMetricsFlowInput,
			Found:                       sdkgo.GoTo(completeCampaignMetricsFlow{}),
		})),
		dex.DefineStep(completeCampaignMetricsFlow{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *CampaignMetricsFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:customerio-campaignmetrics value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*CampaignMetricsFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readCampaignMetrics(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"customerio-campaignmetrics": outcome}}, nil
}

// dex:field attribute-key:customerio-campaignmetrics value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*CampaignMetricsFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readCampaignMetrics(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"customerio-campaignmetrics": outcome}}, nil
}

func readCampaignMetrics(ctx dex.Context) (customerio.CampaignMetrics, error) {
	outcome, err := campaignMetricsAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return customerio.CampaignMetrics{}, nil
	}
	return outcome, err
}

// mapCampaignMetricsFlowInput preserves the validated typed start input for the provider call.
func mapCampaignMetricsFlowInput(input customerio.GetCampaignMetricsInput) customerio.GetCampaignMetricsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startCampaignMetricsFlow struct{ dex.StepDefaults }

func (startCampaignMetricsFlow) GetStepType() string { return "StartGetCampaignMetrics" }
func (startCampaignMetricsFlow) WaitFor(dex.Context, customerio.GetCampaignMetricsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startCampaignMetricsFlow) Execute(_ dex.Context, input customerio.GetCampaignMetricsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[customerio.GetCampaignMetricsInput](campaignMetricsStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeCampaignMetricsFlow struct {
	dex.StepDefaultsNoWaitFor[customerio.GetCampaignMetricsResult]
}

func (completeCampaignMetricsFlow) GetStepType() string { return "CompleteGetCampaignMetrics" }
func (completeCampaignMetricsFlow) Execute(ctx dex.Context, result customerio.GetCampaignMetricsResult) (*dex.StepDecision, error) {
	if err := campaignMetricsAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CampaignMetricsFlow)(nil)
