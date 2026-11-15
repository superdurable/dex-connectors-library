// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package newconversation demonstrates the Re:amaze conversationCreated Trigger: every conversation that
// is not spam, created in the watched channel, starts one Flow, which reads the conversation back with its
// newest messages and records an intake summary.
package newconversation

import (
	"errors"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/reamaze/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity each new conversation starts.
	FlowType = "ReamazeNewConversation"
	// ConnectionName is the static Dex Web connection for the Re:amaze brand.
	ConnectionName = "reamaze-brand"
	// ConversationCreatedTriggerBinding is the stable conversationCreated binding that starts this Flow.
	ConversationCreatedTriggerBinding = "new-conversations"

	recordNewConversationStepType   = "RecordNewConversation"
	readNewConversationStepType     = "ReadNewConversation"
	completeNewConversationStepType = "CompleteNewConversation"
)

var (
	newConversationAttribute = dex.DefineAttribute[Input]("reamaze-new-conversation")
	intakeAttribute          = dex.DefineAttribute[Intake]("reamaze-new-conversation-intake")
)

// Input is one conversationCreated delivery.
type Input struct {
	// TriggerEventID is the stable Trigger event ID that also names the Flow.
	TriggerEventID string `json:"triggerEventId"`
	// Created is the delivered event.
	Created reamaze.ConversationCreatedEvent `json:"created"`
}

// Intake is the Flow result and the value of its intake Attribute.
type Intake struct {
	// ConversationID is the new conversation's slug.
	ConversationID string `json:"conversationId"`
	// Channel is the conversation's channel slug.
	Channel string `json:"channel"`
	// Subject is the conversation subject.
	Subject string `json:"subject,omitempty"`
	// RequesterEmail is the customer's email, or empty for a contact without one.
	RequesterEmail string `json:"requesterEmail,omitempty"`
	// Status is the conversation's Re:amaze status when the Flow read it back.
	Status reamaze.ConversationStatus `json:"status"`
	// MessageCount is the number of messages and notes read back, at most the default message limit.
	MessageCount int `json:"messageCount"`
}

// Flow records one new Re:amaze conversation.
type Flow struct {
	dex.FlowDefaults
	connection reamaze.Connection
}

// NewFlow binds the Re:amaze Connection at registration time.
func NewFlow(connection reamaze.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and Re:amaze connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordNewConversation{}),
		dex.DefineStep(reamaze.NewGetConversationStep(reamaze.GetConversationStepConfig[Input]{
			StepType: readNewConversationStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "intake", GroupLabel: "Intake",
				Explanation: "Read the new conversation back with its newest messages, so the intake reflects its current state.",
			},
			Connection: flow.connection, MapToOperationInput: MapToGetConversationInput,
			Found: sdkgo.GoTo(completeNewConversation{}),
		})),
		dex.DefineStep(completeNewConversation{}),
	}
}

// GetConnectorTriggerBindings declares the conversationCreated binding and its channel entry for Dex Web.
func (*Flow) GetConnectorTriggerBindings() []sdkgo.TriggerBindingDefinition {
	return []sdkgo.TriggerBindingDefinition{
		reamaze.DefineConversationCreatedTriggerBinding(reamaze.ConversationCreatedTriggerBindingConfig{
			ConnectionName: ConnectionName, BindingName: ConversationCreatedTriggerBinding,
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
				ID: "watchedChannel", UnitID: reamaze.UIUnitChannelSlugEntry, Label: "Watched channel",
				Description: "Enter the slug of the Re:amaze channel whose new conversations start this Flow, such as support; find it in the slug field of GET https://{brand}.reamaze.io/api/v1/channels or under Settings > Channels in Re:amaze. Leave it blank to start a Flow for new conversations in every channel of the brand. The first start records existing conversations as a baseline and starts no Flow for them. Restart the Worker after saving; a changed channel records a new baseline.",
				Bindings:    []sdkgo.ConnectorUIBinding{{Port: reamaze.UIChannelSlugEntryPortChannel, JSONPointer: "/channel"}},
			}}},
		}),
	}
}

// GetRPCs returns the summary and display RPCs used by Dex Web.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the delivery and intake Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{newConversationAttribute, intakeAttribute}}
}

// GetDexSummary returns the delivered conversation and its intake.
//
// dex:field attribute-key:reamaze-new-conversation value-type:json editable:false description:"New conversation"
// dex:field attribute-key:reamaze-new-conversation-intake value-type:json editable:false description:"Intake summary"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	created, intake, err := inspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"reamaze-new-conversation": created, "reamaze-new-conversation-intake": intake,
	}}, nil
}

// GetDexDisplay returns the delivered conversation and the intake read back from Re:amaze.
//
// dex:field attribute-key:reamaze-new-conversation value-type:json editable:false description:"Brand, channel, and conversation as the poll listed it"
// dex:field attribute-key:reamaze-new-conversation-intake value-type:json editable:false description:"Status and message count read back from Re:amaze"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	created, intake, err := inspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"reamaze-new-conversation": created, "reamaze-new-conversation-intake": intake,
	}}, nil
}

func inspection(ctx dex.Context) (Input, Intake, error) {
	created, err := optionalAttribute(ctx, newConversationAttribute)
	if err != nil {
		return Input{}, Intake{}, err
	}
	intake, err := optionalAttribute(ctx, intakeAttribute)
	return created, intake, err
}

// optionalAttribute returns the zero value while a running Flow has not set the Attribute yet.
func optionalAttribute[T any](ctx dex.Context, attribute dex.Attribute[T]) (T, error) {
	value, err := attribute.Get(ctx)
	var missingAttribute *dex.AttributeNotFoundError
	if errors.As(err, &missingAttribute) {
		var zero T
		return zero, nil
	}
	return value, err
}

// ResolveFlowID names one Flow per conversation; a redelivered conversation finds its existing Flow.
func ResolveFlowID(event sdkgo.TriggerEvent[reamaze.ConversationCreatedEvent]) string {
	return "reamaze-new-conversation-" + event.ID
}

// MapToFlowInput keeps the Trigger event ID beside the delivered conversation.
func MapToFlowInput(event sdkgo.TriggerEvent[reamaze.ConversationCreatedEvent]) Input {
	return Input{TriggerEventID: event.ID, Created: event.Payload}
}

// AcceptCustomerConversation is the application's admission rule: conversations Re:amaze or staff marked
// as spam start no Flow. The Trigger lists every view, so archived conversations still arrive.
func AcceptCustomerConversation(event sdkgo.TriggerEvent[reamaze.ConversationCreatedEvent]) bool {
	status := event.Payload.Conversation.Status
	return status != reamaze.ConversationStatusSpam && status != reamaze.ConversationStatusAISpam
}

// MapToGetConversationInput reads the new conversation by its slug.
func MapToGetConversationInput(input Input) reamaze.GetConversationInput {
	return reamaze.GetConversationInput{ConversationID: input.Created.Conversation.ID}
}

// dex:group group-id:intake group-label:"Intake"
// dex:explanation text:"Record the delivered conversation, then read it back from Re:amaze."
type recordNewConversation struct {
	dex.StepDefaults
}

func (recordNewConversation) GetStepType() string { return recordNewConversationStepType }

// WaitFor skips immediately because the Trigger starts the Flow at this Step.
func (recordNewConversation) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordNewConversation) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	if err := validateInput(input); err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	if err := newConversationAttribute.Set(ctx, input); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[Input](readNewConversationStepType), input), nil
}

// dex:group group-id:intake group-label:"Intake"
// dex:explanation text:"Record the intake summary from the conversation read back and complete the Flow."
type completeNewConversation struct {
	dex.StepDefaultsNoWaitFor[reamaze.GetConversationResult]
}

func (completeNewConversation) GetStepType() string { return completeNewConversationStepType }

func (completeNewConversation) Execute(ctx dex.Context, result reamaze.GetConversationResult) (*dex.StepDecision, error) {
	intake := NewIntake(result.Value)
	if err := intakeAttribute.Set(ctx, intake); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(intake), nil
}

// NewIntake summarizes a conversation read back from Re:amaze.
func NewIntake(details reamaze.ConversationDetails) Intake {
	intake := Intake{
		ConversationID: details.Conversation.ID, Channel: details.Conversation.Channel.Slug, Subject: details.Conversation.Subject,
		Status: details.Conversation.Status, MessageCount: len(details.Messages),
	}
	if details.Conversation.Requester != nil {
		intake.RequesterEmail = details.Conversation.Requester.Email
	}
	return intake
}

func validateInput(input Input) error {
	switch {
	case strings.TrimSpace(input.TriggerEventID) == "":
		return errors.New("triggerEventId is required")
	case strings.TrimSpace(input.Created.Brand) == "" || strings.TrimSpace(input.Created.Conversation.ID) == "":
		return errors.New("created.brand and created.conversation.id are required")
	}
	return nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
