// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package leadinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/adobe/marketo/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// CreateLeadFlowType is the stable lead creation Flow identity.
const CreateLeadFlowType = "MarketoCreateLead"
const createLeadStepType = "CallMarketoCreateLead"

var createdLeadAttribute = dex.DefineAttribute[marketo.CreatedLead]("marketo-created-lead")

// CreateLeadFlow creates one lead by email and stores its returned ID.
type CreateLeadFlow struct {
	dex.FlowDefaults
	connection marketo.Connection
}

// NewCreateLeadFlow binds the static Marketo connection.
func NewCreateLeadFlow(connection marketo.Connection) *CreateLeadFlow {
	return &CreateLeadFlow{connection: connection}
}

// GetFlowType identifies the creation example.
func (*CreateLeadFlow) GetFlowType() string { return CreateLeadFlowType }

// GetPersistenceSchema registers the created lead result.
func (*CreateLeadFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdLeadAttribute}}
}

// GetSteps registers the create and completion boundaries.
func (flow *CreateLeadFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startCreateLead{}),
		dex.DefineStep(marketo.NewCreateLeadStep(marketo.CreateLeadStepConfig[marketo.CreateLeadInput]{
			StepType: createLeadStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Marketo", Explanation: "Create a lead by email with bounded resend."},
			Connection:  flow.connection, MapToOperationInput: mapCreateLeadInput,
			Created: sdkgo.GoTo(completeCreateLead{}),
		})),
		dex.DefineStep(completeCreateLead{}),
	}
}

// GetRPCs exposes the result to Dex Web.
func (flow *CreateLeadFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:marketo-created-lead value-type:json editable:false description:"Created Marketo lead"
// GetDexSummary returns the created lead ID.
func (*CreateLeadFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := readCreatedLead(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"marketo-created-lead": result}}, nil
}

// dex:field attribute-key:marketo-created-lead value-type:json editable:false description:"Created Marketo lead"
// GetDexDisplay returns the created lead ID.
func (*CreateLeadFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := readCreatedLead(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"marketo-created-lead": result}}, nil
}

func readCreatedLead(ctx dex.Context) (marketo.CreatedLead, error) {
	result, err := createdLeadAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return marketo.CreatedLead{}, nil
	}
	return result, err
}

func mapCreateLeadInput(input marketo.CreateLeadInput) marketo.CreateLeadInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the email and selected names to Marketo."
type startCreateLead struct{ dex.StepDefaults }

func (startCreateLead) GetStepType() string { return "StartMarketoCreateLead" }
func (startCreateLead) WaitFor(dex.Context, marketo.CreateLeadInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startCreateLead) Execute(_ dex.Context, input marketo.CreateLeadInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[marketo.CreateLeadInput](createLeadStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Store the created lead ID and complete the Flow."
type completeCreateLead struct {
	dex.StepDefaultsNoWaitFor[marketo.CreateLeadResult]
}

func (completeCreateLead) GetStepType() string { return "CompleteMarketoCreateLead" }
func (completeCreateLead) Execute(ctx dex.Context, result marketo.CreateLeadResult) (*dex.StepDecision, error) {
	if err := createdLeadAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CreateLeadFlow)(nil)
