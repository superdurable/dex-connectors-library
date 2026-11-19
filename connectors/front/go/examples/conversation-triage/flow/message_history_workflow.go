// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package conversationtriage

import (
	"github.com/superdurable/dex-connectors-library/connectors/front/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	getHistoryMessageStepType   = "GetHistoryMessage"
	listHistoryMessagesStepType = "ListHistoryMessages"
	listHistoryCommentsStepType = "ListHistoryComments"
)

// MessageHistoryRequest identifies the message and conversation to inspect.
type MessageHistoryRequest struct {
	// MessageID is the Front msg_ identifier to read.
	MessageID string `json:"messageId"`
	// ConversationID is the Front cnv_ identifier whose history to read.
	ConversationID string `json:"conversationId"`
}

var messageHistoryRequestAttribute = dex.DefineAttribute[MessageHistoryRequest]("front-message-history-request")

// MessageHistoryFlow reads one message and its conversation's message and comment history.
type MessageHistoryFlow struct {
	dex.FlowDefaults
	connection front.Connection
}

// NewMessageHistoryFlow binds the Front connection at registration time.
func NewMessageHistoryFlow(connection front.Connection) *MessageHistoryFlow {
	return &MessageHistoryFlow{connection: connection}
}

// GetFlowType returns the stable example Flow identity.
func (*MessageHistoryFlow) GetFlowType() string { return "FrontMessageHistory" }

// GetPersistenceSchema registers the request used by later read steps.
func (*MessageHistoryFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{messageHistoryRequestAttribute}}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *MessageHistoryFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:front-message-history-request value-type:json editable:false description:"Selected Front message and conversation IDs"
// GetDexSummary shows the selected IDs.
func (*MessageHistoryFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, err := messageHistoryRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"front-message-history-request": request}}, nil
}

// dex:field attribute-key:front-message-history-request value-type:json editable:false description:"Selected Front message and conversation IDs"
// GetDexDisplay shows the selected IDs.
func (*MessageHistoryFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, err := messageHistoryRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"front-message-history-request": request}}, nil
}

// GetSteps defines the message and comment read path.
func (flow *MessageHistoryFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startMessageHistory{}),
		dex.DefineStep(front.NewGetMessageStep(front.GetMessageStepConfig[MessageHistoryRequest]{
			StepType: getHistoryMessageStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations: sdkgo.StepAnnotations{GroupID: "front-read", GroupLabel: "Front reads", Explanation: "Read the selected Front message."},
			MapToOperationInput: func(request MessageHistoryRequest) front.GetMessageInput {
				return front.GetMessageInput{MessageID: request.MessageID}
			},
			Found: sdkgo.GoTo(afterHistoryMessage{}),
		})),
		dex.DefineStep(afterHistoryMessage{}),
		dex.DefineStep(front.NewListConversationMessagesStep(front.ListConversationMessagesStepConfig[front.ListConversationMessagesInput]{
			StepType: listHistoryMessagesStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "front-read", GroupLabel: "Front reads", Explanation: "Read one bounded page of conversation messages."},
			MapToOperationInput: func(input front.ListConversationMessagesInput) front.ListConversationMessagesInput { return input },
			Listed:              sdkgo.GoTo(afterHistoryMessages{}),
		})),
		dex.DefineStep(afterHistoryMessages{}),
		dex.DefineStep(front.NewListConversationCommentsStep(front.ListConversationCommentsStepConfig[front.ListConversationCommentsInput]{
			StepType: listHistoryCommentsStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "front-read", GroupLabel: "Front reads", Explanation: "Read the conversation's internal comments."},
			MapToOperationInput: func(input front.ListConversationCommentsInput) front.ListConversationCommentsInput { return input },
			Listed:              sdkgo.GoTo(completeMessageHistory{}),
		})),
		dex.DefineStep(completeMessageHistory{}),
	}
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Start a Front message history read with the selected IDs."
type startMessageHistory struct{ dex.StepDefaults }

func (startMessageHistory) GetStepType() string { return "StartMessageHistory" }
func (startMessageHistory) WaitFor(dex.Context, MessageHistoryRequest) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startMessageHistory) Execute(ctx dex.Context, input MessageHistoryRequest) (*dex.StepDecision, error) {
	if err := messageHistoryRequestAttribute.Set(ctx, input); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[MessageHistoryRequest](getHistoryMessageStepType), input), nil
}

// dex:group group-id:front-read group-label:"Front reads"
// dex:explanation text:"Save the selected message and list its conversation's messages."
type afterHistoryMessage struct {
	dex.StepDefaultsNoWaitFor[front.GetMessageResult]
}

func (afterHistoryMessage) GetStepType() string { return "AfterHistoryMessage" }
func (afterHistoryMessage) Execute(ctx dex.Context, _ front.GetMessageResult) (*dex.StepDecision, error) {
	request, err := messageHistoryRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[front.ListConversationMessagesInput](listHistoryMessagesStepType), front.ListConversationMessagesInput{ConversationID: request.ConversationID, PageSize: 25}), nil
}

// dex:group group-id:front-read group-label:"Front reads"
// dex:explanation text:"List internal comments after the bounded message page."
type afterHistoryMessages struct {
	dex.StepDefaultsNoWaitFor[front.ListConversationMessagesResult]
}

func (afterHistoryMessages) GetStepType() string { return "AfterHistoryMessages" }
func (afterHistoryMessages) Execute(ctx dex.Context, _ front.ListConversationMessagesResult) (*dex.StepDecision, error) {
	request, err := messageHistoryRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[front.ListConversationCommentsInput](listHistoryCommentsStepType), front.ListConversationCommentsInput{ConversationID: request.ConversationID}), nil
}

// dex:group group-id:front-read group-label:"Front reads"
// dex:explanation text:"Complete the read after Front returns the internal comments."
type completeMessageHistory struct {
	dex.StepDefaultsNoWaitFor[front.ListConversationCommentsResult]
}

func (completeMessageHistory) GetStepType() string { return "CompleteMessageHistory" }
func (completeMessageHistory) Execute(_ dex.Context, result front.ListConversationCommentsResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*MessageHistoryFlow)(nil)
