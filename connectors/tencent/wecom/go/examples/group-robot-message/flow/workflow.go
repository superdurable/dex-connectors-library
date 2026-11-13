// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package grouprobotmessage posts one WeCom group robot message as a Dex Flow.
package grouprobotmessage

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/tencent/wecom/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "WeComGroupRobotMessage"
	// ConnectionName is the static Dex Web connection, configured with the group-robot method.
	ConnectionName    = "wecom-robot"
	operationStepType = "PostGroupRobotMessage"
)

var postedMessageAttribute = dex.DefineAttribute[wecom.SentGroupRobotMessage]("wecom-group-robot-message")

// Flow posts one message to the robot's group chat and completes with WeCom's confirmation.
type Flow struct {
	dex.FlowDefaults
	connection wecom.Connection
}

// NewFlow binds the WeCom group robot connection at registration time.
func NewFlow(connection wecom.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{postedMessageAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(wecom.NewSendGroupRobotMessageStep(wecom.SendGroupRobotMessageStepConfig[wecom.SendGroupRobotMessageInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "WeCom", Explanation: "Post one message to the group chat that owns the robot webhook."},
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

// dex:field attribute-key:wecom-group-robot-message value-type:json editable:false description:"Posted message"
// GetDexSummary shows the posted message type and size in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	posted, err := readPostedMessage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"wecom-group-robot-message": posted}}, nil
}

// dex:field attribute-key:wecom-group-robot-message value-type:json editable:false description:"Posted message"
// GetDexDisplay shows the posted message type and size in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	posted, err := readPostedMessage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"wecom-group-robot-message": posted}}, nil
}

func readPostedMessage(ctx dex.Context) (wecom.SentGroupRobotMessage, error) {
	posted, err := postedMessageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return wecom.SentGroupRobotMessage{}, nil
	}
	return posted, err
}

func mapToOperationInput(input wecom.SendGroupRobotMessageInput) wecom.SendGroupRobotMessageInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the message input to the WeCom group robot Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGroupRobotMessage" }
func (startRequest) WaitFor(dex.Context, wecom.SendGroupRobotMessageInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input wecom.SendGroupRobotMessageInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[wecom.SendGroupRobotMessageInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Record the confirmed post and complete the Flow."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[wecom.SendGroupRobotMessageResult]
}

func (completeResult) GetStepType() string { return "CompleteGroupRobotMessage" }
func (completeResult) Execute(ctx dex.Context, result wecom.SendGroupRobotMessageResult) (*dex.StepDecision, error) {
	if err := postedMessageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
