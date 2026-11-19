// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/creatio/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const createLeadStepType = "CreateCreatioLead"

var createdLeadAttribute = dex.DefineAttribute[creatio.LeadSummary]("creatio-created-lead")

// LeadCreationFlow creates one Creatio lead with bounded sends.
type LeadCreationFlow struct {
	dex.FlowDefaults
	connection creatio.Connection
}

// NewLeadCreationFlow binds the static Creatio connection.
func NewLeadCreationFlow(connection creatio.Connection) *LeadCreationFlow {
	return &LeadCreationFlow{connection: connection}
}

// GetFlowType gives this example a stable Flow identity.
func (*LeadCreationFlow) GetFlowType() string { return "CreatioLeadCreation" }

// GetPersistenceSchema retains the created lead for Dex Web.
func (*LeadCreationFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdLeadAttribute}}
}

// GetSteps wires the operation-specific lead Mutation.
func (flow *LeadCreationFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startLeadCreation{}),
		dex.DefineStep(creatio.NewCreateLeadStep(creatio.CreateLeadStepConfig[creatio.CreateLeadInput]{
			StepType: createLeadStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Creatio", Explanation: "Create a Creatio lead."},
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

// dex:field attribute-key:creatio-created-lead value-type:json editable:false description:"Created lead"
// GetDexSummary shows the created lead in Dex Web.
func (*LeadCreationFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readLeadMetadata(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"creatio-created-lead": page}}, nil
}

// dex:field attribute-key:creatio-created-lead value-type:json editable:false description:"Created lead"
// GetDexDisplay shows the created lead in Dex Web.
func (*LeadCreationFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readLeadMetadata(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"creatio-created-lead": page}}, nil
}

func readLeadMetadata(ctx dex.Context) (creatio.LeadSummary, error) {
	page, err := createdLeadAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return creatio.LeadSummary{}, nil
	}
	return page, err
}

func mapLeadCreationInput(input creatio.CreateLeadInput) creatio.CreateLeadInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the requested lead name to Creatio."
type startLeadCreation struct{ dex.StepDefaults }

func (startLeadCreation) WaitFor(dex.Context, creatio.CreateLeadInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startLeadCreation) Execute(_ dex.Context, input creatio.CreateLeadInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[creatio.CreateLeadInput](createLeadStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Retain the created lead and complete the Flow."
type completeLeadCreation struct {
	dex.StepDefaultsNoWaitFor[creatio.CreateLeadResult]
}

func (completeLeadCreation) Execute(ctx dex.Context, result creatio.CreateLeadResult) (*dex.StepDecision, error) {
	if err := createdLeadAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*LeadCreationFlow)(nil)
