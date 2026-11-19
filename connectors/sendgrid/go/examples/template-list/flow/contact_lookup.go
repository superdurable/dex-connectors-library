// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package templatelist

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/sendgrid/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// ContactFlowType identifies the contact lookup example.
	ContactFlowType       = "SendGridContactLookupDemo"
	contactLookupStepType = "CallGetContact"
)

var contactOutcomeAttribute = dex.DefineAttribute[sendgrid.Contact]("sendgrid-contact-lookup-outcome")

// ContactFlow retrieves one marketing contact for a Flow request.
type ContactFlow struct {
	dex.FlowDefaults
	connection sendgrid.Connection
}

// NewContactFlow binds the static SendGrid connection.
func NewContactFlow(connection sendgrid.Connection) *ContactFlow {
	return &ContactFlow{connection: connection}
}

// GetFlowType returns the stable contact Flow identity.
func (*ContactFlow) GetFlowType() string { return ContactFlowType }

// GetPersistenceSchema registers the contact outcome Attribute.
func (*ContactFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{contactOutcomeAttribute}}
}

// GetSteps registers the request, provider lookup, and completion Steps.
func (flow *ContactFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startContactLookup{}),
		dex.DefineStep(sendgrid.NewGetContactStep(sendgrid.GetContactStepConfig[sendgrid.ContactIDInput]{
			StepType: contactLookupStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "SendGrid", Explanation: "Retrieve one marketing contact by its provider ID."},
			Connection:  flow.connection, MapToOperationInput: func(input sendgrid.ContactIDInput) sendgrid.ContactIDInput { return input },
			Found: sdkgo.GoTo(completeContactLookup{}),
		})),
		dex.DefineStep(completeContactLookup{}),
	}
}

// GetRPCs exposes Dex Web summary and display views.
func (flow *ContactFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:sendgrid-contact-lookup-outcome value-type:json editable:false description:"Marketing contact"
// GetDexSummary returns the saved contact.
func (*ContactFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	contact, err := readContactOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"sendgrid-contact-lookup-outcome": contact}}, nil
}

// dex:field attribute-key:sendgrid-contact-lookup-outcome value-type:json editable:false description:"Marketing contact"
// GetDexDisplay returns the saved contact.
func (*ContactFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	contact, err := readContactOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"sendgrid-contact-lookup-outcome": contact}}, nil
}

func readContactOutcome(ctx dex.Context) (sendgrid.Contact, error) {
	contact, err := contactOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return sendgrid.Contact{}, nil
	}
	return contact, err
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the contact ID to the provider Step."
type startContactLookup struct{ dex.StepDefaults }

func (startContactLookup) GetStepType() string { return "StartContactLookup" }
func (startContactLookup) WaitFor(dex.Context, sendgrid.ContactIDInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startContactLookup) Execute(_ dex.Context, input sendgrid.ContactIDInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[sendgrid.ContactIDInput](contactLookupStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Save the contact and complete the Flow."
type completeContactLookup struct {
	dex.StepDefaultsNoWaitFor[sendgrid.GetContactResult]
}

func (completeContactLookup) GetStepType() string { return "CompleteContactLookup" }
func (completeContactLookup) Execute(ctx dex.Context, result sendgrid.GetContactResult) (*dex.StepDecision, error) {
	if err := contactOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*ContactFlow)(nil)
