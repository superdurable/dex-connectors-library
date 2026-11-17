// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package personlookup

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/copper/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const createLeadStepType = "CreateCopperLead"

var createdLeadAttribute = dex.DefineAttribute[copper.LeadMetadata]("copper-created-lead")

// LeadCreationFlow creates one Copper lead with bounded sends.
type LeadCreationFlow struct {
	dex.FlowDefaults
	connection copper.Connection
}

// NewLeadCreationFlow binds the static Copper connection.
func NewLeadCreationFlow(connection copper.Connection) *LeadCreationFlow {
	return &LeadCreationFlow{connection: connection}
}

// GetFlowType gives this example a stable Flow identity.
func (*LeadCreationFlow) GetFlowType() string { return "CopperLeadCreation" }

// GetPersistenceSchema retains the created lead for Dex Web.
func (*LeadCreationFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdLeadAttribute}}
}

// GetSteps wires the operation-specific lead Mutation.
func (flow *LeadCreationFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startLeadCreation{}),
		dex.DefineStep(copper.NewCreateLeadStep(copper.CreateLeadStepConfig[copper.CreateLeadInput]{
			StepType: createLeadStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Copper", Explanation: "Create a Copper lead."},
			Connection:  flow.connection, MapToOperationInput: mapLeadCreationInput,
			Created: sdkgo.GoTo(completeLeadCreation{}),
		})),
		dex.DefineStep(completeLeadCreation{}),
	}
}

// GetRPCs exposes read-only Dex Web result views.
func (flow *LeadCreationFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:copper-created-lead value-type:json editable:false description:"Created lead"
// GetDexSummary shows the created lead in Dex Web.
func (*LeadCreationFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readLeadMetadata(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"copper-created-lead": page}}, nil
}

// dex:field attribute-key:copper-created-lead value-type:json editable:false description:"Created lead"
// GetDexDisplay shows the created lead in Dex Web.
func (*LeadCreationFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readLeadMetadata(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"copper-created-lead": page}}, nil
}

func readLeadMetadata(ctx dex.Context) (copper.LeadMetadata, error) {
	page, err := createdLeadAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return copper.LeadMetadata{}, nil
	}
	return page, err
}

func mapLeadCreationInput(input copper.CreateLeadInput) copper.CreateLeadInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the requested lead name and email to Copper."
type startLeadCreation struct{ dex.StepDefaults }

func (startLeadCreation) WaitFor(dex.Context, copper.CreateLeadInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startLeadCreation) Execute(_ dex.Context, input copper.CreateLeadInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[copper.CreateLeadInput](createLeadStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Retain the created lead and complete the Flow."
type completeLeadCreation struct {
	dex.StepDefaultsNoWaitFor[copper.CreateLeadResult]
}

func (completeLeadCreation) Execute(ctx dex.Context, result copper.CreateLeadResult) (*dex.StepDecision, error) {
	if err := createdLeadAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*LeadCreationFlow)(nil)
