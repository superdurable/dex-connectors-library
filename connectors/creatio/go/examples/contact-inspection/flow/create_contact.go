// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/creatio/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const createContactStepType = "CreateCreatioContact"

var createdContactAttribute = dex.DefineAttribute[creatio.ContactSummary]("creatio-created-contact")

// ContactCreationFlow creates one Creatio contact with bounded sends.
type ContactCreationFlow struct {
	dex.FlowDefaults
	connection creatio.Connection
}

// NewContactCreationFlow binds the static Creatio connection.
func NewContactCreationFlow(connection creatio.Connection) *ContactCreationFlow {
	return &ContactCreationFlow{connection: connection}
}

// GetFlowType gives this example a stable Flow identity.
func (*ContactCreationFlow) GetFlowType() string { return "CreatioContactCreation" }

// GetPersistenceSchema retains the created contact for Dex Web.
func (*ContactCreationFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdContactAttribute}}
}

// GetSteps wires the operation-specific contact Mutation.
func (flow *ContactCreationFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startContactCreation{}),
		dex.DefineStep(creatio.NewCreateContactStep(creatio.CreateContactStepConfig[creatio.CreateContactInput]{
			StepType: createContactStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Creatio", Explanation: "Create a Creatio contact."},
			Connection:  flow.connection, MapToOperationInput: mapContactCreationInput,
			Created: sdkgo.GoTo(completeContactCreation{}),
		})),
		dex.DefineStep(completeContactCreation{}),
	}
}

// GetRPCs exposes read-only Dex Web result views.
func (flow *ContactCreationFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:creatio-created-contact value-type:json editable:false description:"Created contact"
// GetDexSummary shows the created contact in Dex Web.
func (*ContactCreationFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readContactMetadata(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"creatio-created-contact": page}}, nil
}

// dex:field attribute-key:creatio-created-contact value-type:json editable:false description:"Created contact"
// GetDexDisplay shows the created contact in Dex Web.
func (*ContactCreationFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readContactMetadata(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"creatio-created-contact": page}}, nil
}

func readContactMetadata(ctx dex.Context) (creatio.ContactSummary, error) {
	page, err := createdContactAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return creatio.ContactSummary{}, nil
	}
	return page, err
}

func mapContactCreationInput(input creatio.CreateContactInput) creatio.CreateContactInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the requested contact name and email to Creatio."
type startContactCreation struct{ dex.StepDefaults }

func (startContactCreation) WaitFor(dex.Context, creatio.CreateContactInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startContactCreation) Execute(_ dex.Context, input creatio.CreateContactInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[creatio.CreateContactInput](createContactStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Retain the created contact and complete the Flow."
type completeContactCreation struct {
	dex.StepDefaultsNoWaitFor[creatio.CreateContactResult]
}

func (completeContactCreation) Execute(ctx dex.Context, result creatio.CreateContactResult) (*dex.StepDecision, error) {
	if err := createdContactAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*ContactCreationFlow)(nil)
