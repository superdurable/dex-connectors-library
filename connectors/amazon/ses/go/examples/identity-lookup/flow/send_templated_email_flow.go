// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package identitylookup

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/amazon/ses/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// TemplatedSendFlowType is the Dex Web Start Flow identity for a template send.
	TemplatedSendFlowType = "AmazonSESTemplatedEmailSend"
	templatedSendStepType = "CallSendTemplatedEmail"
)

var templatedSentEmailAttribute = dex.DefineAttribute[ses.SentEmail]("ses-templated-sent-email")

// TemplatedSendFlow sends a stored SES template with JSON variables.
type TemplatedSendFlow struct {
	dex.FlowDefaults
	connection ses.Connection
}

// NewTemplatedSendFlow binds the SES connection for one templated email Flow.
func NewTemplatedSendFlow(connection ses.Connection) *TemplatedSendFlow {
	return &TemplatedSendFlow{connection: connection}
}

// GetFlowType returns the stable templated send Flow identity.
func (*TemplatedSendFlow) GetFlowType() string { return TemplatedSendFlowType }

// GetPersistenceSchema registers the accepted message identity.
func (*TemplatedSendFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{templatedSentEmailAttribute}}
}

// GetRPCs exposes the accepted message ID in Dex Web.
func (flow *TemplatedSendFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the accepted message ID, if available.
//
// dex:field attribute-key:ses-templated-sent-email value-type:json editable:false description:"Accepted SES templated message ID"
func (*TemplatedSendFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	message, err := readTemplatedSentEmail(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"ses-templated-sent-email": message}}, nil
}

// GetDexDisplay returns the accepted message ID, if available.
//
// dex:field attribute-key:ses-templated-sent-email value-type:json editable:false description:"Accepted SES templated message ID"
func (*TemplatedSendFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	message, err := readTemplatedSentEmail(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"ses-templated-sent-email": message}}, nil
}

func readTemplatedSentEmail(ctx dex.Context) (ses.SentEmail, error) {
	message, err := templatedSentEmailAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return ses.SentEmail{}, nil
	}
	return message, err
}

// GetSteps registers start, provider send, and completion Steps.
func (flow *TemplatedSendFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startTemplatedEmailSend{}),
		dex.DefineStep(ses.NewSendTemplatedEmailStep(ses.SendTemplatedEmailStepConfig[ses.SendTemplatedEmailInput]{
			StepType: templatedSendStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Amazon SES", Explanation: "Send a stored SES template with JSON variables."},
			Connection:  flow.connection, MapToOperationInput: mapToTemplatedEmailInput,
			Sent: sdkgo.GoTo(completeTemplatedEmailSend{}),
		})),
		dex.DefineStep(completeTemplatedEmailSend{}),
	}
}

func mapToTemplatedEmailInput(input ses.SendTemplatedEmailInput) ses.SendTemplatedEmailInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the template name, sender, recipient, and variables to SES."
type startTemplatedEmailSend struct{ dex.StepDefaults }

func (startTemplatedEmailSend) GetStepType() string { return "StartSendTemplatedEmail" }
func (startTemplatedEmailSend) WaitFor(dex.Context, ses.SendTemplatedEmailInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startTemplatedEmailSend) Execute(_ dex.Context, input ses.SendTemplatedEmailInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[ses.SendTemplatedEmailInput](templatedSendStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with SES's accepted templated message ID."
type completeTemplatedEmailSend struct {
	dex.StepDefaultsNoWaitFor[ses.SendTemplatedEmailResult]
}

func (completeTemplatedEmailSend) GetStepType() string { return "CompleteSendTemplatedEmail" }
func (completeTemplatedEmailSend) Execute(ctx dex.Context, result ses.SendTemplatedEmailResult) (*dex.StepDecision, error) {
	if err := templatedSentEmailAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*TemplatedSendFlow)(nil)
