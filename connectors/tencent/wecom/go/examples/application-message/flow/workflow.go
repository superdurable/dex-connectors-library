// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package applicationmessage sends one WeCom self-built application message as a Dex Flow.
package applicationmessage

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/tencent/wecom/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "WeComApplicationMessage"
	// ConnectionName is the static Dex Web connection, configured with the application method.
	ConnectionName    = "wecom-app"
	operationStepType = "SendApplicationMessage"
)

var sentMessageAttribute = dex.DefineAttribute[wecom.SentApplicationMessage]("wecom-application-message")

// Flow sends one application message and completes with its msgid and any skipped recipients.
type Flow struct {
	dex.FlowDefaults
	connection wecom.Connection
}

// NewFlow binds the WeCom application connection at registration time.
func NewFlow(connection wecom.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{sentMessageAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(wecom.NewSendApplicationMessageStep(wecom.SendApplicationMessageStepConfig[wecom.SendApplicationMessageInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "WeCom", Explanation: "Send one application message to the selected members, departments, or tags."},
			Connection:          flow.connection,
			MapToOperationInput: mapToOperationInput,
			Sent:                sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:wecom-application-message value-type:json editable:false description:"Sent message"
// GetDexSummary shows the msgid and skipped recipients in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	sent, err := readSentMessage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"wecom-application-message": sent}}, nil
}

// dex:field attribute-key:wecom-application-message value-type:json editable:false description:"Sent message"
// GetDexDisplay shows the msgid and skipped recipients in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	sent, err := readSentMessage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"wecom-application-message": sent}}, nil
}

func readSentMessage(ctx dex.Context) (wecom.SentApplicationMessage, error) {
	sent, err := sentMessageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return wecom.SentApplicationMessage{}, nil
	}
	return sent, err
}

func mapToOperationInput(input wecom.SendApplicationMessageInput) wecom.SendApplicationMessageInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the message input to the WeCom application Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartApplicationMessage" }
func (startRequest) WaitFor(dex.Context, wecom.SendApplicationMessageInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input wecom.SendApplicationMessageInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[wecom.SendApplicationMessageInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Record the msgid and skipped recipients, then complete the Flow."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[wecom.SendApplicationMessageResult]
}

func (completeResult) GetStepType() string { return "CompleteApplicationMessage" }
func (completeResult) Execute(ctx dex.Context, result wecom.SendApplicationMessageResult) (*dex.StepDecision, error) {
	if err := sentMessageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
