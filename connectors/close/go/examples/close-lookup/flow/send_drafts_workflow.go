// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package closelookup

import (
	"github.com/superdurable/dex-connectors-library/connectors/close/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// SendDraftsFlowType is the stable identity for the message sending example.
const SendDraftsFlowType = "CloseSendDraftMessages"

const sendEmailDraftStepType = "SendCloseDraftEmail"
const sendSMSDraftStepType = "SendCloseDraftSMS"

var sentEmailAttribute = dex.DefineAttribute[close.EmailActivity]("close-sent-draft-email")
var selectedSMSDraftIDAttribute = dex.DefineAttribute[string]("close-selected-sms-draft-id")

// SendDraftsRequest identifies existing email and SMS drafts.
type SendDraftsRequest struct {
	// EmailID identifies a Close draft email.
	EmailID string `json:"emailId"`
	// SMSID identifies a Close draft SMS.
	SMSID string `json:"smsId"`
}

// SendDraftsOutcome contains the provider status for both messages.
type SendDraftsOutcome struct {
	// Email is the email moved to outbox or sent.
	Email close.EmailActivity `json:"email"`
	// Message is the SMS moved to outbox or sent.
	Message close.SMSActivity `json:"message"`
}

// SendDraftsFlow sends two existing drafts through their limited-tier Steps.
type SendDraftsFlow struct {
	dex.FlowDefaults
	connection close.Connection
}

// NewSendDraftsFlow binds the static Close connection.
func NewSendDraftsFlow(connection close.Connection) *SendDraftsFlow {
	return &SendDraftsFlow{connection: connection}
}

// GetFlowType returns the stable example Flow identity.
func (*SendDraftsFlow) GetFlowType() string { return SendDraftsFlowType }

// GetPersistenceSchema registers the email saved across the two send Steps.
func (*SendDraftsFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{sentEmailAttribute, selectedSMSDraftIDAttribute}}
}

// GetRPCs registers the read-only Dex Web views.
func (flow *SendDraftsFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the example's read-only summary.
func (*SendDraftsFlow) GetDexSummary(_ dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{}}, nil
}

// GetDexDisplay returns the example's read-only display.
func (*SendDraftsFlow) GetDexDisplay(_ dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{}}, nil
}

// GetSteps registers the start, message send, and completion Steps.
func (flow *SendDraftsFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startSendDrafts{}),
		dex.DefineStep(close.NewSendEmailDraftStep(close.SendEmailDraftStepConfig[SendDraftsRequest]{
			StepType: sendEmailDraftStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations: sdkgo.StepAnnotations{GroupID: "close-send-drafts", GroupLabel: "Send drafts", Explanation: "Move the selected email draft into Close outbox."},
			MapToOperationInput: func(input SendDraftsRequest) close.SendEmailDraftInput {
				return close.SendEmailDraftInput{EmailID: input.EmailID}
			},
			Sent: sdkgo.GoTo(afterSentEmail{}),
		})),
		dex.DefineStep(afterSentEmail{}),
		dex.DefineStep(close.NewSendSMSDraftStep(close.SendSMSDraftStepConfig[close.SendSMSDraftInput]{
			StepType: sendSMSDraftStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "close-send-drafts", GroupLabel: "Send drafts", Explanation: "Move the selected SMS draft into Close outbox."},
			MapToOperationInput: func(input close.SendSMSDraftInput) close.SendSMSDraftInput { return input },
			Sent:                sdkgo.GoTo(completeSendDrafts{}),
		})),
		dex.DefineStep(completeSendDrafts{}),
	}
}

// dex:group group-id:close-send-drafts group-label:"Send drafts"
// dex:explanation text:"Start sending the selected email and SMS drafts."
type startSendDrafts struct{ dex.StepDefaults }

func (startSendDrafts) GetStepType() string { return "StartCloseSendDraftMessages" }
func (startSendDrafts) WaitFor(dex.Context, SendDraftsRequest) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startSendDrafts) Execute(ctx dex.Context, input SendDraftsRequest) (*dex.StepDecision, error) {
	if err := selectedSMSDraftIDAttribute.Set(ctx, input.SMSID); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[SendDraftsRequest](sendEmailDraftStepType), input), nil
}

// dex:group group-id:close-send-drafts group-label:"Send drafts"
// dex:explanation text:"Save the email status and send the selected SMS draft."
type afterSentEmail struct {
	dex.StepDefaultsNoWaitFor[close.SendEmailDraftResult]
}

func (afterSentEmail) Execute(ctx dex.Context, result close.SendEmailDraftResult) (*dex.StepDecision, error) {
	if err := sentEmailAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	smsID, err := selectedSMSDraftIDAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[close.SendSMSDraftInput](sendSMSDraftStepType), close.SendSMSDraftInput{SMSID: smsID}), nil
}

// dex:group group-id:close-send-drafts group-label:"Send drafts"
// dex:explanation text:"Complete after both drafts enter Close outbox."
type completeSendDrafts struct {
	dex.StepDefaultsNoWaitFor[close.SendSMSDraftResult]
}

func (completeSendDrafts) GetStepType() string { return "CompleteCloseSendDraftMessages" }
func (completeSendDrafts) Execute(ctx dex.Context, result close.SendSMSDraftResult) (*dex.StepDecision, error) {
	email, err := sentEmailAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return dex.GracefulComplete(SendDraftsOutcome{Email: email, Message: result.Value}), nil
}

var _ dex.Flow = (*SendDraftsFlow)(nil)
