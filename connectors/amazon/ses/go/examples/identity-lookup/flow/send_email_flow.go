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
	// SendFlowType is the stable Dex Web Start Flow identity for a simple email send.
	SendFlowType      = "AmazonSESSimpleEmailSend"
	sendEmailStepType = "CallSendEmail"
)

var sentEmailAttribute = dex.DefineAttribute[ses.SentEmail]("ses-sent-email")

// SendFlow sends one simple email through the same SES connection as the lookup Flow.
type SendFlow struct {
	dex.FlowDefaults
	connection ses.Connection
}

// NewSendFlow binds the provider connection for one simple email Flow.
func NewSendFlow(connection ses.Connection) *SendFlow { return &SendFlow{connection: connection} }

// GetFlowType returns the stable send Flow identity.
func (*SendFlow) GetFlowType() string { return SendFlowType }

// GetPersistenceSchema registers the accepted message identity.
func (*SendFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{sentEmailAttribute}}
}

// GetRPCs exposes read-only Dex Web views of the accepted message ID.
func (flow *SendFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the accepted message ID when this Flow has one.
//
// dex:field attribute-key:ses-sent-email value-type:json editable:false description:"Accepted SES message ID"
func (*SendFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	message, err := readSentEmail(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"ses-sent-email": message}}, nil
}

// GetDexDisplay returns the accepted message ID when this Flow has one.
//
// dex:field attribute-key:ses-sent-email value-type:json editable:false description:"Accepted SES message ID"
func (*SendFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	message, err := readSentEmail(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"ses-sent-email": message}}, nil
}

func readSentEmail(ctx dex.Context) (ses.SentEmail, error) {
	message, err := sentEmailAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return ses.SentEmail{}, nil
	}
	return message, err
}

// GetSteps registers the start, provider send, and completion Steps.
func (flow *SendFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startEmailSend{}),
		dex.DefineStep(ses.NewSendEmailStep(ses.SendEmailStepConfig[ses.SendEmailInput]{
			StepType: sendEmailStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Amazon SES", Explanation: "Send one plain text email from a verified SES identity."},
			Connection:  flow.connection, MapToOperationInput: mapToSendEmailInput,
			Sent: sdkgo.GoTo(completeEmailSend{}),
		})),
		dex.DefineStep(completeEmailSend{}),
	}
}

func mapToSendEmailInput(input ses.SendEmailInput) ses.SendEmailInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the requested email to the SES provider Step."
type startEmailSend struct{ dex.StepDefaults }

func (startEmailSend) GetStepType() string { return "StartSendEmail" }
func (startEmailSend) WaitFor(dex.Context, ses.SendEmailInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startEmailSend) Execute(_ dex.Context, input ses.SendEmailInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[ses.SendEmailInput](sendEmailStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with SES's accepted message ID."
type completeEmailSend struct {
	dex.StepDefaultsNoWaitFor[ses.SendEmailResult]
}

func (completeEmailSend) GetStepType() string { return "CompleteSendEmail" }
func (completeEmailSend) Execute(ctx dex.Context, result ses.SendEmailResult) (*dex.StepDecision, error) {
	if err := sentEmailAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*SendFlow)(nil)
