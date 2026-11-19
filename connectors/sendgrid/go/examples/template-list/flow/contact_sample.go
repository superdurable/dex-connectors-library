// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package templatelist

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/sendgrid/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// ContactSampleFlowType identifies the recent contact sample example.
const ContactSampleFlowType = "SendGridContactSampleDemo"

var contactSampleAttribute = dex.DefineAttribute[sendgrid.SearchContactsOutput]("sendgrid-contact-sample-outcome")

// ContactSampleFlow reads the provider's recent marketing contact sample.
type ContactSampleFlow struct {
	dex.FlowDefaults
	connection sendgrid.Connection
}

// NewContactSampleFlow binds the static SendGrid connection.
func NewContactSampleFlow(connection sendgrid.Connection) *ContactSampleFlow {
	return &ContactSampleFlow{connection: connection}
}

// GetFlowType returns the stable contact sample Flow identity.
func (*ContactSampleFlow) GetFlowType() string { return ContactSampleFlowType }

// GetPersistenceSchema registers the contact sample Attribute.
func (*ContactSampleFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{contactSampleAttribute}}
}

// GetSteps defines the sample Query and completion Step.
func (flow *ContactSampleFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(sendgrid.NewListContactsStep(sendgrid.ListContactsStepConfig[sendgrid.ListContactsInput]{
			StepType: "ReadRecentSendGridContacts", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "SendGrid", Explanation: "Read the provider's bounded recent contact sample."},
			MapToOperationInput: func(input sendgrid.ListContactsInput) sendgrid.ListContactsInput { return input },
			Listed:              sdkgo.GoTo(completeContactSample{}),
		})),
		dex.DefineStep(completeContactSample{}),
	}
}

// GetRPCs exposes Dex Web summary and display views.
func (flow *ContactSampleFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:sendgrid-contact-sample-outcome value-type:json editable:false description:"Recent contacts"
// GetDexSummary returns the saved contact sample.
func (*ContactSampleFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	output, err := readContactSample(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"sendgrid-contact-sample-outcome": output}}, nil
}

// dex:field attribute-key:sendgrid-contact-sample-outcome value-type:json editable:false description:"Recent contacts"
// GetDexDisplay returns the saved contact sample.
func (*ContactSampleFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	output, err := readContactSample(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"sendgrid-contact-sample-outcome": output}}, nil
}

func readContactSample(ctx dex.Context) (sendgrid.SearchContactsOutput, error) {
	output, err := contactSampleAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return sendgrid.SearchContactsOutput{}, nil
	}
	return output, err
}

// dex:group group-id:contact-sample-result group-label:"Contact sample"
// dex:explanation text:"Save the recent contacts and complete the Flow."
type completeContactSample struct {
	dex.StepDefaultsNoWaitFor[sendgrid.ListContactsResult]
}

func (completeContactSample) GetStepType() string { return "CompleteContactSample" }
func (completeContactSample) Execute(ctx dex.Context, result sendgrid.ListContactsResult) (*dex.StepDecision, error) {
	if err := contactSampleAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*ContactSampleFlow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*ContactSampleFlow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*ContactSampleFlow)(nil).GetDexDisplay
