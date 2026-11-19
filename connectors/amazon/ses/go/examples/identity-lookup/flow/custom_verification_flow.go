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
	// CustomVerificationFlowType is the Dex Web Start Flow identity for verification email.
	CustomVerificationFlowType = "AmazonSESCustomVerificationEmailSend"
	customVerificationStepType = "CallSendCustomVerificationEmail"
)

var customVerificationSentAttribute = dex.DefineAttribute[ses.SentEmail]("ses-custom-verification-sent-email")

// CustomVerificationFlow sends a custom verification email.
type CustomVerificationFlow struct {
	dex.FlowDefaults
	connection ses.Connection
}

// NewCustomVerificationFlow binds the SES connection for one verification email Flow.
func NewCustomVerificationFlow(connection ses.Connection) *CustomVerificationFlow {
	return &CustomVerificationFlow{connection: connection}
}

// GetFlowType returns the stable verification send Flow identity.
func (*CustomVerificationFlow) GetFlowType() string { return CustomVerificationFlowType }

// GetPersistenceSchema registers the accepted verification message identity.
func (*CustomVerificationFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{customVerificationSentAttribute}}
}

// GetRPCs exposes the accepted message ID in Dex Web.
func (flow *CustomVerificationFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the accepted message ID, if available.
//
// dex:field attribute-key:ses-custom-verification-sent-email value-type:json editable:false description:"Accepted SES verification message ID"
func (*CustomVerificationFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	message, err := readCustomVerificationSentEmail(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"ses-custom-verification-sent-email": message}}, nil
}

// GetDexDisplay returns the accepted message ID, if available.
//
// dex:field attribute-key:ses-custom-verification-sent-email value-type:json editable:false description:"Accepted SES verification message ID"
func (*CustomVerificationFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	message, err := readCustomVerificationSentEmail(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"ses-custom-verification-sent-email": message}}, nil
}

func readCustomVerificationSentEmail(ctx dex.Context) (ses.SentEmail, error) {
	message, err := customVerificationSentAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return ses.SentEmail{}, nil
	}
	return message, err
}

// GetSteps registers start, provider send, and completion Steps.
func (flow *CustomVerificationFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startCustomVerificationSend{}),
		dex.DefineStep(ses.NewSendCustomVerificationEmailStep(ses.SendCustomVerificationEmailStepConfig[ses.SendCustomVerificationEmailInput]{
			StepType: customVerificationStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Amazon SES", Explanation: "Send one custom verification email."},
			Connection:  flow.connection, MapToOperationInput: mapToCustomVerificationInput,
			Sent: sdkgo.GoTo(completeCustomVerificationSend{}),
		})),
		dex.DefineStep(completeCustomVerificationSend{}),
	}
}

func mapToCustomVerificationInput(input ses.SendCustomVerificationEmailInput) ses.SendCustomVerificationEmailInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the recipient and custom verification template name to SES."
type startCustomVerificationSend struct{ dex.StepDefaults }

func (startCustomVerificationSend) GetStepType() string { return "StartSendCustomVerificationEmail" }
func (startCustomVerificationSend) WaitFor(dex.Context, ses.SendCustomVerificationEmailInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startCustomVerificationSend) Execute(_ dex.Context, input ses.SendCustomVerificationEmailInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[ses.SendCustomVerificationEmailInput](customVerificationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with SES's accepted verification message ID."
type completeCustomVerificationSend struct {
	dex.StepDefaultsNoWaitFor[ses.SendCustomVerificationEmailResult]
}

func (completeCustomVerificationSend) GetStepType() string {
	return "CompleteSendCustomVerificationEmail"
}
func (completeCustomVerificationSend) Execute(ctx dex.Context, result ses.SendCustomVerificationEmailResult) (*dex.StepDecision, error) {
	if err := customVerificationSentAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CustomVerificationFlow)(nil)
