// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package textmessagedelivery

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/twilio/messaging/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const listMessagesFlowType = "TwilioMessageList"
const listMessagesStepType = "ListMessages"

var messagePageAttribute = dex.DefineAttribute[messaging.MessagePage]("twilio-message-page")

// MessageListRequest selects one page of Twilio messages.
type MessageListRequest messaging.ListMessagesInput

// MessageListFlow reads a page of message metadata.
type MessageListFlow struct {
	dex.FlowDefaults
	connection messaging.Connection
}

// NewMessageListFlow binds the Twilio connection.
func NewMessageListFlow(connection messaging.Connection) *MessageListFlow {
	return &MessageListFlow{connection: connection}
}

// GetFlowType returns the stable Flow identity.
func (*MessageListFlow) GetFlowType() string { return listMessagesFlowType }

// GetSteps registers the list request and result Steps.
func (flow *MessageListFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(prepareMessageList{}),
		dex.DefineStep(messaging.NewListMessagesStep(messaging.ListMessagesStepConfig[MessageListRequest]{
			StepType: listMessagesStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations: sdkgo.StepAnnotations{GroupID: "twilio-list", GroupLabel: "Twilio Messages", Explanation: "List message metadata without bodies."},
			MapToOperationInput: func(request MessageListRequest) messaging.ListMessagesInput {
				return messaging.ListMessagesInput(request)
			},
			Listed: sdkgo.GoTo(completeMessageList{}),
		})),
		dex.DefineStep(completeMessageList{}),
	}
}

// GetRPCs registers the summary and display views.
func (flow *MessageListFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the message page Attribute.
func (*MessageListFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{messagePageAttribute}}
}

// GetDexSummary returns the saved message page.
// dex:field attribute-key:twilio-message-page value-type:json editable:false description:"Message metadata page"
func (*MessageListFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readMessagePage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"twilio-message-page": page}}, nil
}

// GetDexDisplay returns the saved message page.
// dex:field attribute-key:twilio-message-page value-type:json editable:false description:"Message metadata page"
func (*MessageListFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readMessagePage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"twilio-message-page": page}}, nil
}

func readMessagePage(ctx dex.Context) (messaging.MessagePage, error) {
	page, err := messagePageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return messaging.MessagePage{}, nil
	}
	return page, err
}

// dex:group group-id:twilio-list-prepare group-label:"Prepare message list"
// dex:explanation text:"Pass the list filters to the Twilio connector Step."
type prepareMessageList struct{ dex.StepDefaults }

func (prepareMessageList) GetStepType() string { return "PrepareMessageList" }
func (prepareMessageList) WaitFor(dex.Context, MessageListRequest) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (prepareMessageList) Execute(_ dex.Context, request MessageListRequest) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[MessageListRequest](listMessagesStepType), request), nil
}

// dex:group group-id:twilio-list-complete group-label:"Complete message list"
// dex:explanation text:"Persist and return the Twilio message page."
type completeMessageList struct {
	dex.StepDefaultsNoWaitFor[messaging.ListMessagesResult]
}

func (completeMessageList) GetStepType() string { return "CompleteMessageList" }
func (completeMessageList) Execute(ctx dex.Context, result messaging.ListMessagesResult) (*dex.StepDecision, error) {
	if err := messagePageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*MessageListFlow)(nil)
