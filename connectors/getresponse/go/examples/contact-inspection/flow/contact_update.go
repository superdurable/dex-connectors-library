// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/getresponse/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const contactUpdateStepType = "CallUpdateContact"

var updatedContactAttribute = dex.DefineAttribute[getresponse.ContactMetadata]("getresponse-updated-contact")

// ContactUpdateFlow demonstrates an absolute contact update through Dex.
type ContactUpdateFlow struct {
	dex.FlowDefaults
	connection getresponse.Connection
}

// NewContactUpdateFlow binds the same static GetResponse connection as the inspection Flow.
func NewContactUpdateFlow(connection getresponse.Connection) *ContactUpdateFlow {
	return &ContactUpdateFlow{connection: connection}
}

// GetFlowType keeps the contact update example's durable identity stable.
func (*ContactUpdateFlow) GetFlowType() string { return "GetResponseContactUpdate" }

// GetPersistenceSchema registers the updated contact for Dex Web reads.
func (*ContactUpdateFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{updatedContactAttribute}}
}

// GetSteps wires an absolute update and its successful result.
func (flow *ContactUpdateFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startContactUpdate{}),
		dex.DefineStep(getresponse.NewUpdateContactStep(getresponse.UpdateContactStepConfig[getresponse.UpdateContactInput]{
			StepType: contactUpdateStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "GetResponse", Explanation: "Update one contact's supplied fields."},
			Connection:          flow.connection,
			MapToOperationInput: mapContactUpdateInput,
			Updated:             sdkgo.GoTo(completeContactUpdate{}),
		})),
		dex.DefineStep(completeContactUpdate{}),
	}
}

// GetRPCs exposes the contact update result in Dex Web.
func (flow *ContactUpdateFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:getresponse-updated-contact value-type:json editable:false description:"Updated contact"
// GetDexSummary returns the last confirmed update.
func (*ContactUpdateFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	contact, err := readUpdatedContact(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"getresponse-updated-contact": contact}}, nil
}

// dex:field attribute-key:getresponse-updated-contact value-type:json editable:false description:"Updated contact"
// GetDexDisplay returns the last confirmed update.
func (*ContactUpdateFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	contact, err := readUpdatedContact(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"getresponse-updated-contact": contact}}, nil
}

func readUpdatedContact(ctx dex.Context) (getresponse.ContactMetadata, error) {
	contact, err := updatedContactAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return getresponse.ContactMetadata{}, nil
	}
	return contact, err
}

func mapContactUpdateInput(input getresponse.UpdateContactInput) getresponse.UpdateContactInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Send the typed contact update input to GetResponse."
type startContactUpdate struct{ dex.StepDefaults }

func (startContactUpdate) GetStepType() string { return "StartContactUpdate" }
func (startContactUpdate) WaitFor(dex.Context, getresponse.UpdateContactInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startContactUpdate) Execute(_ dex.Context, input getresponse.UpdateContactInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[getresponse.UpdateContactInput](contactUpdateStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the confirmed contact update and complete the Flow."
type completeContactUpdate struct {
	dex.StepDefaultsNoWaitFor[getresponse.UpdateContactResult]
}

func (completeContactUpdate) GetStepType() string { return "CompleteContactUpdate" }
func (completeContactUpdate) Execute(ctx dex.Context, result getresponse.UpdateContactResult) (*dex.StepDecision, error) {
	if err := updatedContactAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*ContactUpdateFlow)(nil)
