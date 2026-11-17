// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package conversationtriage

import (
	"github.com/superdurable/dex-connectors-library/connectors/front/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const sendNewMessageStepType = "SendNewFrontMessage"

var newMessageRequestAttribute = dex.DefineAttribute[front.SendNewMessageInput]("front-new-message-request")

// NewMessageFlow sends a new Front message from a channel address.
type NewMessageFlow struct {
	dex.FlowDefaults
	connection front.Connection
}

// NewNewMessageFlow binds the Front connection at registration time.
func NewNewMessageFlow(connection front.Connection) *NewMessageFlow {
	return &NewMessageFlow{connection: connection}
}

// GetFlowType returns the stable example Flow identity.
func (*NewMessageFlow) GetFlowType() string { return "FrontNewMessage" }

// GetPersistenceSchema registers the request for Dex Web.
func (*NewMessageFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{newMessageRequestAttribute}}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *NewMessageFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:front-new-message-request value-type:json editable:false description:"Front channel, recipient, and message request"
// GetDexSummary shows the submitted request.
func (*NewMessageFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, err := newMessageRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"front-new-message-request": request}}, nil
}

// dex:field attribute-key:front-new-message-request value-type:json editable:false description:"Front channel, recipient, and message request"
// GetDexDisplay shows the submitted request.
func (*NewMessageFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, err := newMessageRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"front-new-message-request": request}}, nil
}

// GetSteps defines the new-message send path.
func (flow *NewMessageFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startNewMessage{}),
		dex.DefineStep(front.NewSendNewMessageStep(front.SendNewMessageStepConfig[front.SendNewMessageInput]{
			StepType: sendNewMessageStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "front-send", GroupLabel: "Front send", Explanation: "Send a new message from the selected channel address."},
			MapToOperationInput: func(input front.SendNewMessageInput) front.SendNewMessageInput { return input },
			Sent:                sdkgo.GoTo(completeNewMessage{}),
		})),
		dex.DefineStep(completeNewMessage{}),
	}
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Start a new Front message using the supplied channel and recipient."
type startNewMessage struct{ dex.StepDefaults }

func (startNewMessage) GetStepType() string { return "StartNewMessage" }
func (startNewMessage) WaitFor(dex.Context, front.SendNewMessageInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startNewMessage) Execute(ctx dex.Context, input front.SendNewMessageInput) (*dex.StepDecision, error) {
	if err := newMessageRequestAttribute.Set(ctx, input); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[front.SendNewMessageInput](sendNewMessageStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete after Front accepts the new message and returns its UID."
type completeNewMessage struct {
	dex.StepDefaultsNoWaitFor[front.SendNewMessageResult]
}

func (completeNewMessage) GetStepType() string { return "CompleteNewMessage" }
func (completeNewMessage) Execute(_ dex.Context, result front.SendNewMessageResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*NewMessageFlow)(nil)
