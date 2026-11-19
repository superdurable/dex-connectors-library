// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package personlookup

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/copper/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const createOpportunityStepType = "CreateCopperOpportunity"

var createdOpportunityAttribute = dex.DefineAttribute[copper.OpportunityMetadata]("copper-created-opportunity")

// OpportunityCreationFlow creates one Copper opportunity with bounded sends.
type OpportunityCreationFlow struct {
	dex.FlowDefaults
	connection copper.Connection
}

// NewOpportunityCreationFlow binds the static Copper connection.
func NewOpportunityCreationFlow(connection copper.Connection) *OpportunityCreationFlow {
	return &OpportunityCreationFlow{connection: connection}
}

// GetFlowType gives this example a stable Flow identity.
func (*OpportunityCreationFlow) GetFlowType() string { return "CopperOpportunityCreation" }

// GetPersistenceSchema retains the created opportunity for Dex Web.
func (*OpportunityCreationFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdOpportunityAttribute}}
}

// GetSteps wires the operation-specific opportunity Mutation.
func (flow *OpportunityCreationFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startOpportunityCreation{}),
		dex.DefineStep(copper.NewCreateOpportunityStep(copper.CreateOpportunityStepConfig[copper.CreateOpportunityInput]{
			StepType: createOpportunityStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Copper", Explanation: "Create a Copper opportunity."},
			Connection:  flow.connection, MapToOperationInput: mapOpportunityCreationInput,
			Created: sdkgo.GoTo(completeOpportunityCreation{}),
		})),
		dex.DefineStep(completeOpportunityCreation{}),
	}
}

// GetRPCs exposes read-only Dex Web result views.
func (flow *OpportunityCreationFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:copper-created-opportunity value-type:json editable:false description:"Created opportunity"
// GetDexSummary shows the created opportunity in Dex Web.
func (*OpportunityCreationFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readOpportunityMetadata(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"copper-created-opportunity": page}}, nil
}

// dex:field attribute-key:copper-created-opportunity value-type:json editable:false description:"Created opportunity"
// GetDexDisplay shows the created opportunity in Dex Web.
func (*OpportunityCreationFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readOpportunityMetadata(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"copper-created-opportunity": page}}, nil
}

func readOpportunityMetadata(ctx dex.Context) (copper.OpportunityMetadata, error) {
	page, err := createdOpportunityAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return copper.OpportunityMetadata{}, nil
	}
	return page, err
}

func mapOpportunityCreationInput(input copper.CreateOpportunityInput) copper.CreateOpportunityInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the requested opportunity name to Copper."
type startOpportunityCreation struct{ dex.StepDefaults }

func (startOpportunityCreation) WaitFor(dex.Context, copper.CreateOpportunityInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startOpportunityCreation) Execute(_ dex.Context, input copper.CreateOpportunityInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[copper.CreateOpportunityInput](createOpportunityStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Retain the created opportunity and complete the Flow."
type completeOpportunityCreation struct {
	dex.StepDefaultsNoWaitFor[copper.CreateOpportunityResult]
}

func (completeOpportunityCreation) Execute(ctx dex.Context, result copper.CreateOpportunityResult) (*dex.StepDecision, error) {
	if err := createdOpportunityAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*OpportunityCreationFlow)(nil)
