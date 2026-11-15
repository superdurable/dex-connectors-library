// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package messagereceived demonstrates the Twilio messageReceived Trigger: every inbound SMS, MMS, or
// WhatsApp message to the configured Twilio number starts one Flow, which reads the message back and
// records its current status.
package messagereceived

import (
	"errors"
	"strings"

	messaging "github.com/superdurable/dex-connectors-library/connectors/twilio/messaging/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity each inbound message starts.
	FlowType = "TwilioInboundMessage"
	// ConnectionName is the static Dex Web connection for Twilio Messaging.
	ConnectionName = "twilio-messaging"
	// MessageReceivedTriggerBinding is the stable messageReceived binding that starts this Flow.
	MessageReceivedTriggerBinding = "inbound-messages"

	recordInboundMessageStepType   = "RecordInboundMessage"
	readInboundMessageStepType     = "ReadInboundMessage"
	completeInboundMessageStepType = "CompleteInboundMessage"
)

var (
	inboundMessageAttribute        = dex.DefineAttribute[Input]("twilio-inbound-message")
	inboundMessageOutcomeAttribute = dex.DefineAttribute[InboundMessageOutcome]("twilio-inbound-message-outcome")
)

// Input is one messageReceived delivery.
type Input struct {
	// TriggerEventID is the stable Trigger event ID that also names the Flow.
	TriggerEventID string `json:"triggerEventId"`
	// Received is the delivered inbound message.
	Received messaging.MessageReceivedEvent `json:"received"`
}

// InboundMessageOutcome is the Flow result and the value of its outcome Attribute.
type InboundMessageOutcome struct {
	// MessageSID is the inbound message.
	MessageSID string `json:"messageSid"`
	// From is the sender who texted the Twilio number.
	From string `json:"from"`
	// To is the Twilio number or WhatsApp sender that received it.
	To string `json:"to"`
	// IsOptOut reports a STOP-family keyword, which Twilio's default opt-out handling already honors.
	IsOptOut bool `json:"isOptOut"`
	// Current is the message read back from Twilio with getMessage.
	Current messaging.Message `json:"current"`
}

// Flow records one inbound Twilio message.
type Flow struct {
	dex.FlowDefaults
	connection messaging.Connection
}

// NewFlow binds the Twilio Messaging Connection at registration time.
func NewFlow(connection messaging.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and Twilio connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordInboundMessage{}),
		dex.DefineStep(messaging.NewGetMessageStep(messaging.GetMessageStepConfig[Input]{
			StepType: readInboundMessageStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "inbound", GroupLabel: "Inbound message",
				Explanation: "Read the inbound message back from Twilio, so the record holds its current status.",
			},
			Connection: flow.connection, MapToOperationInput: MapToGetMessageInput,
			Found: sdkgo.GoTo(completeInboundMessage{}),
		})),
		dex.DefineStep(completeInboundMessage{}),
	}
}

// GetConnectorTriggerBindings declares the messageReceived binding and its receiving number for Dex Web.
func (*Flow) GetConnectorTriggerBindings() []sdkgo.TriggerBindingDefinition {
	return []sdkgo.TriggerBindingDefinition{
		messaging.DefineMessageReceivedTriggerBinding(messaging.MessageReceivedTriggerBindingConfig{
			ConnectionName: ConnectionName, BindingName: MessageReceivedTriggerBinding,
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
				ID: "receivingNumber", UnitID: messaging.UIUnitReceivingAddressInput, Label: "Receiving number",
				Description: "Enter the Twilio number, such as +14155550100, or WhatsApp sender, such as whatsapp:+14155550100, whose inbound messages start this Flow; copy it from https://console.twilio.com/us1/develop/phone-numbers/manage/incoming. Leave it blank to start a Flow for every inbound message of the account. The first start records existing messages as a baseline and starts no Flow for them. Restart the Worker after saving; a changed number records a new baseline.",
				Bindings:    []sdkgo.ConnectorUIBinding{{Port: messaging.UIReceivingAddressInputPortTo, JSONPointer: "/to"}},
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

// GetPersistenceSchema registers the delivery and outcome Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{inboundMessageAttribute, inboundMessageOutcomeAttribute}}
}

// GetDexSummary returns the delivered message and outcome.
//
// dex:field attribute-key:twilio-inbound-message value-type:json editable:false description:"Delivered inbound message"
// dex:field attribute-key:twilio-inbound-message-outcome value-type:json editable:false description:"Recorded inbound message"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	received, outcome, err := inboundMessageInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"twilio-inbound-message":         received,
		"twilio-inbound-message-outcome": outcome,
	}}, nil
}

// GetDexDisplay returns the delivered message and the message read back from Twilio.
//
// dex:field attribute-key:twilio-inbound-message value-type:json editable:false description:"Sender, receiving number, text, and status from the Messages list"
// dex:field attribute-key:twilio-inbound-message-outcome value-type:json editable:false description:"Current message read back from Twilio"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	received, outcome, err := inboundMessageInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"twilio-inbound-message":         received,
		"twilio-inbound-message-outcome": outcome,
	}}, nil
}

func inboundMessageInspection(ctx dex.Context) (Input, InboundMessageOutcome, error) {
	received, err := optionalAttribute(ctx, inboundMessageAttribute)
	if err != nil {
		return Input{}, InboundMessageOutcome{}, err
	}
	outcome, err := optionalAttribute(ctx, inboundMessageOutcomeAttribute)
	return received, outcome, err
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

// ResolveFlowID names one Flow per inbound message; a redelivered message finds its existing Flow.
func ResolveFlowID(event sdkgo.TriggerEvent[messaging.MessageReceivedEvent]) string {
	return "twilio-inbound-message-" + event.ID
}

// MapToFlowInput keeps the Trigger event ID beside the delivered message.
func MapToFlowInput(event sdkgo.TriggerEvent[messaging.MessageReceivedEvent]) Input {
	return Input{TriggerEventID: event.ID, Received: event.Payload}
}

// AcceptEveryInboundMessage admits every message; an application narrows it, for example by sender.
func AcceptEveryInboundMessage(sdkgo.TriggerEvent[messaging.MessageReceivedEvent]) bool { return true }

// MapToGetMessageInput reads the inbound message by its SID.
func MapToGetMessageInput(input Input) messaging.GetMessageInput {
	return messaging.GetMessageInput{MessageSID: input.Received.Message.SID}
}

// optOutKeywords are Twilio's default opt-out keywords, matched on the whole trimmed text.
var optOutKeywords = map[string]bool{
	"STOP": true, "STOPALL": true, "UNSUBSCRIBE": true, "CANCEL": true, "END": true, "QUIT": true, "REVOKE": true, "OPTOUT": true,
}

// IsOptOutText reports whether body is one of Twilio's default opt-out keywords.
func IsOptOutText(body string) bool {
	return optOutKeywords[strings.ToUpper(strings.TrimSpace(body))]
}

// dex:group group-id:inbound group-label:"Inbound message"
// dex:explanation text:"Record the delivered inbound message, then read it back from Twilio."
type recordInboundMessage struct {
	dex.StepDefaults
}

func (recordInboundMessage) GetStepType() string { return recordInboundMessageStepType }

// WaitFor skips immediately because the Trigger starts the Flow at this Step.
func (recordInboundMessage) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordInboundMessage) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	if err := validateInput(input); err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	if err := inboundMessageAttribute.Set(ctx, input); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[Input](readInboundMessageStepType), input), nil
}

// dex:group group-id:inbound group-label:"Inbound message"
// dex:explanation text:"Record the message read back from Twilio as the outcome and complete the Flow."
type completeInboundMessage struct {
	dex.StepDefaultsNoWaitFor[messaging.GetMessageResult]
}

func (completeInboundMessage) GetStepType() string { return completeInboundMessageStepType }

func (completeInboundMessage) Execute(ctx dex.Context, result messaging.GetMessageResult) (*dex.StepDecision, error) {
	received, err := inboundMessageAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	message := received.Received.Message
	outcome := InboundMessageOutcome{
		MessageSID: message.SID, From: message.From, To: message.To,
		IsOptOut: IsOptOutText(received.Received.Body), Current: result.Value,
	}
	if err := inboundMessageOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

func validateInput(input Input) error {
	message := input.Received.Message
	switch {
	case strings.TrimSpace(input.TriggerEventID) == "":
		return errors.New("triggerEventId is required")
	case strings.TrimSpace(message.SID) == "" || strings.TrimSpace(message.To) == "":
		return errors.New("received.message.sid and received.message.to are required")
	case message.Direction != messaging.InboundMessageDirection:
		return errors.New("received.message.direction must be inbound")
	}
	return nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
