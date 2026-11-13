// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package campaigncatalog runs Sensorsdata listCampaigns as one Dex Flow.
package campaigncatalog

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/sensorsdata/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "SensorsdataCampaignCatalog"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "sensorsdata"
	operationStepType = "CallListCampaigns"
)

var campaignCatalogOutcomeAttribute = dex.DefineAttribute[sensorsdata.ListCampaignsOutput]("sensorsdata-campaign-catalog-outcome")

// Flow runs ListCampaigns once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection sensorsdata.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection sensorsdata.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{campaignCatalogOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(sensorsdata.NewListCampaignsStep(sensorsdata.ListCampaignsStepConfig[sensorsdata.ListCampaignsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Sensorsdata", Explanation: "Read one campaign page with names, link counts, and creation times."},
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

// dex:field attribute-key:sensorsdata-campaign-catalog-outcome value-type:json editable:false description:"Campaign catalog"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readCampaignCatalogOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"sensorsdata-campaign-catalog-outcome": outcome}}, nil
}

// dex:field attribute-key:sensorsdata-campaign-catalog-outcome value-type:json editable:false description:"Campaign catalog"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readCampaignCatalogOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"sensorsdata-campaign-catalog-outcome": outcome}}, nil
}

func readCampaignCatalogOutcome(ctx dex.Context) (sensorsdata.ListCampaignsOutput, error) {
	outcome, err := campaignCatalogOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return sensorsdata.ListCampaignsOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input sensorsdata.ListCampaignsInput) sensorsdata.ListCampaignsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListCampaigns" }
func (startRequest) WaitFor(dex.Context, sensorsdata.ListCampaignsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input sensorsdata.ListCampaignsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[sensorsdata.ListCampaignsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[sensorsdata.ListCampaignsResult]
}

func (completeResult) GetStepType() string { return "CompleteListCampaigns" }
func (completeResult) Execute(ctx dex.Context, result sensorsdata.ListCampaignsResult) (*dex.StepDecision, error) {
	if err := campaignCatalogOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
