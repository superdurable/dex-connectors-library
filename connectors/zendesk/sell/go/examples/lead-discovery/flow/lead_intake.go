// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package leaddiscovery

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/zendesk/sell/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const leadIntakeStepType = "CreateZendeskSellLead"

var createdLeadAttribute = dex.DefineAttribute[sell.LeadMetadata]("zendesk-sell-created-lead")

// LeadIntakeFlow creates a new Zendesk Sell lead and retains its public identity.
type LeadIntakeFlow struct {
	dex.FlowDefaults
	connection sell.Connection
}

// NewLeadIntakeFlow binds the connection used by the create Step.
func NewLeadIntakeFlow(connection sell.Connection) *LeadIntakeFlow {
	return &LeadIntakeFlow{connection: connection}
}

// GetFlowType preserves this example's durable identity.
func (*LeadIntakeFlow) GetFlowType() string { return "ZendeskSellLeadIntake" }

// GetPersistenceSchema registers the created lead identity.
func (*LeadIntakeFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdLeadAttribute}}
}

// GetSteps connects the start request, provider create, and completion.
func (flow *LeadIntakeFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startLeadIntake{}),
		dex.DefineStep(sell.NewCreateLeadStep(sell.CreateLeadStepConfig[sell.CreateLeadInput]{
			StepType: leadIntakeStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Zendesk Sell", Explanation: "Create a new individual or organization lead."},
			Connection:          flow.connection,
			MapToOperationInput: mapLeadIntakeInput,
			Created:             sdkgo.GoTo(completeLeadIntake{}),
		})),
		dex.DefineStep(completeLeadIntake{}),
	}
}

// GetRPCs exposes the created lead to Dex Web.
func (flow *LeadIntakeFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:zendesk-sell-created-lead value-type:json editable:false description:"Created lead"
// GetDexSummary returns the created lead identity.
func (*LeadIntakeFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	lead, err := readCreatedLead(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"zendesk-sell-created-lead": lead}}, nil
}

// dex:field attribute-key:zendesk-sell-created-lead value-type:json editable:false description:"Created lead"
// GetDexDisplay returns the created lead identity.
func (*LeadIntakeFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	lead, err := readCreatedLead(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"zendesk-sell-created-lead": lead}}, nil
}

func readCreatedLead(ctx dex.Context) (sell.LeadMetadata, error) {
	lead, err := createdLeadAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return sell.LeadMetadata{}, nil
	}
	return lead, err
}

func mapLeadIntakeInput(input sell.CreateLeadInput) sell.CreateLeadInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Send the typed lead input to the provider create Step."
type startLeadIntake struct{ dex.StepDefaults }

func (startLeadIntake) GetStepType() string { return "StartZendeskSellLeadIntake" }
func (startLeadIntake) WaitFor(dex.Context, sell.CreateLeadInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startLeadIntake) Execute(_ dex.Context, input sell.CreateLeadInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[sell.CreateLeadInput](leadIntakeStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Keep the created lead identity and complete the Flow."
type completeLeadIntake struct {
	dex.StepDefaultsNoWaitFor[sell.CreateLeadResult]
}

func (completeLeadIntake) GetStepType() string { return "CompleteZendeskSellLeadIntake" }
func (completeLeadIntake) Execute(ctx dex.Context, result sell.CreateLeadResult) (*dex.StepDecision, error) {
	if err := createdLeadAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*LeadIntakeFlow)(nil)
