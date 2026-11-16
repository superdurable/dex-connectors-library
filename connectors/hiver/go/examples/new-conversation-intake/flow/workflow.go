// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package newconversationintake demonstrates the Hiver conversationReceived Trigger: every conversation that
// newly appears in the picked shared inbox starts one Flow, which reads the conversation's message IDs with
// getConversation and records them for triage.
package newconversationintake

import (
	"errors"
	"strings"
	"time"

	hiver "github.com/superdurable/dex-connectors-library/connectors/hiver/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity each new conversation starts.
	FlowType = "HiverNewConversationIntake"
	// ConnectionName is the static Dex Web connection for Hiver.
	ConnectionName = "hiver-support"
	// ConversationReceivedTriggerBinding is the stable conversationReceived binding that starts this Flow.
	ConversationReceivedTriggerBinding = "new-support-conversations"
	// FlowIDPrefix precedes the Trigger event ID in every Flow ID, so a redelivered conversation maps to one Flow.
	FlowIDPrefix = "hiver-new-conversation-"

	recordReceivedConversationStepType = "RecordReceivedConversation"
	readReceivedConversationStepType   = "ReadReceivedConversation"
	completeConversationIntakeStepType = "CompleteConversationIntake"
)

var (
	receivedConversationAttribute = dex.DefineAttribute[Input]("hiver-received-conversation")
	intakeOutcomeAttribute        = dex.DefineAttribute[IntakeOutcome]("hiver-conversation-intake")
)

// Input is one conversationReceived delivery.
type Input struct {
	// TriggerEventID is the stable Trigger event ID that also names the Flow.
	TriggerEventID string `json:"triggerEventId"`
	// Received is the delivered conversation.
	Received hiver.ConversationReceivedEvent `json:"received"`
	// DetectedAt is when the poll found the conversation; Hiver's list carries no timestamp.
	DetectedAt time.Time `json:"detectedAt"`
}

// IntakeOutcome is the Flow result and the value of its outcome Attribute.
type IntakeOutcome struct {
	// InboxID is the polled shared inbox.
	InboxID string `json:"inboxId"`
	// ConversationID is the new conversation.
	ConversationID string `json:"conversationId"`
	// Status is the conversation's Hiver status when the Flow read it.
	Status hiver.ConversationStatus `json:"status"`
	// PrivatePermalink opens the conversation in Hiver for a signed-in user.
	PrivatePermalink string `json:"privatePermalink,omitempty"`
	// Messages are the conversation's message IDs, which createSharedDraft accepts.
	Messages []hiver.ConversationMessage `json:"messages"`
}

// Flow records one new Hiver conversation.
type Flow struct {
	dex.FlowDefaults
	connection hiver.Connection
}

// NewFlow binds the Hiver Connection at registration time.
func NewFlow(connection hiver.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and Hiver connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordReceivedConversation{}),
		dex.DefineStep(hiver.NewGetConversationStep(hiver.GetConversationStepConfig[Input]{
			StepType: readReceivedConversationStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "intake", GroupLabel: "Intake",
				Explanation: "Read the new conversation from Hiver for its current status and message IDs.",
			},
			Connection: flow.connection, MapToOperationInput: MapToGetConversationInput,
			Found: sdkgo.GoTo(completeConversationIntake{}),
		})),
		dex.DefineStep(completeConversationIntake{}),
	}
}

// GetConnectorTriggerBindings declares the conversationReceived binding and its inbox picker for Dex Web.
func (*Flow) GetConnectorTriggerBindings() []sdkgo.TriggerBindingDefinition {
	return []sdkgo.TriggerBindingDefinition{
		hiver.DefineConversationReceivedTriggerBinding(hiver.ConversationReceivedTriggerBindingConfig{
			ConnectionName: ConnectionName, BindingName: ConversationReceivedTriggerBinding,
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
				ID: "sharedInbox", UnitID: hiver.UIUnitInboxPicker, Label: "Shared inbox", Required: true,
				Description: "Choose the Hiver shared inbox whose new conversations start this Flow. Load inboxes lists the account's shared inboxes with the connection's API key and saves the chosen inbox's ID. To enter the ID instead, a Hiver admin finds it as the id of the inbox in GET https://api2.hiverhq.com/v1/inboxes; it is a short string of digits such as 105902 and is not secret. Conversations already in the inbox when the Worker first polls never start Flows. Restart the Worker after saving; choosing another inbox records a new baseline. It cannot be blank.",
				Bindings:    []sdkgo.ConnectorUIBinding{{Port: hiver.UIInboxPickerPortInboxID, JSONPointer: "/inboxId"}},
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
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{receivedConversationAttribute, intakeOutcomeAttribute}}
}

// GetDexSummary returns the delivered conversation and the recorded intake.
//
// dex:field attribute-key:hiver-received-conversation value-type:json editable:false description:"Delivered Hiver conversation"
// dex:field attribute-key:hiver-conversation-intake value-type:json editable:false description:"Recorded conversation intake"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	received, outcome, err := intakeInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"hiver-received-conversation": received, "hiver-conversation-intake": outcome,
	}}, nil
}

// GetDexDisplay returns the delivered conversation and the message IDs read from Hiver.
//
// dex:field attribute-key:hiver-received-conversation value-type:json editable:false description:"Inbox and conversation from the poll"
// dex:field attribute-key:hiver-conversation-intake value-type:json editable:false description:"Status and message IDs read back from Hiver"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	received, outcome, err := intakeInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"hiver-received-conversation": received, "hiver-conversation-intake": outcome,
	}}, nil
}

func intakeInspection(ctx dex.Context) (Input, IntakeOutcome, error) {
	received, err := optionalAttribute(ctx, receivedConversationAttribute)
	if err != nil {
		return Input{}, IntakeOutcome{}, err
	}
	outcome, err := optionalAttribute(ctx, intakeOutcomeAttribute)
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

// AcceptEveryConversation admits every new conversation; an application narrows it, for example to open ones.
func AcceptEveryConversation(sdkgo.TriggerEvent[hiver.ConversationReceivedEvent]) bool { return true }

// ResolveFlowID names one Flow per conversation; a redelivered conversation finds its existing Flow.
func ResolveFlowID(event sdkgo.TriggerEvent[hiver.ConversationReceivedEvent]) string {
	return FlowIDPrefix + event.ID
}

// MapToFlowInput keeps the Trigger event ID beside the delivered conversation.
func MapToFlowInput(event sdkgo.TriggerEvent[hiver.ConversationReceivedEvent]) Input {
	return Input{TriggerEventID: event.ID, Received: event.Payload, DetectedAt: event.OccurredAt}
}

// MapToGetConversationInput reads the delivered conversation from its inbox.
func MapToGetConversationInput(input Input) hiver.GetConversationInput {
	return hiver.GetConversationInput{InboxID: input.Received.InboxID, ConversationID: input.Received.Conversation.ID}
}

// dex:group group-id:intake group-label:"Intake"
// dex:explanation text:"Record the delivered conversation, then read it from Hiver."
type recordReceivedConversation struct {
	dex.StepDefaultsNoWaitFor[Input]
}

func (recordReceivedConversation) GetStepType() string { return recordReceivedConversationStepType }

func (recordReceivedConversation) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	if strings.TrimSpace(input.TriggerEventID) == "" || strings.TrimSpace(input.Received.InboxID) == "" || strings.TrimSpace(input.Received.Conversation.ID) == "" {
		return dex.ForceFail("a received conversation requires its Trigger event ID, inbox ID, and conversation ID"), nil
	}
	if err := receivedConversationAttribute.Set(ctx, input); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[Input](readReceivedConversationStepType), input), nil
}

// dex:group group-id:intake group-label:"Intake"
// dex:explanation text:"Record the conversation's status and message IDs as the outcome and complete the Flow."
type completeConversationIntake struct {
	dex.StepDefaultsNoWaitFor[hiver.GetConversationResult]
}

func (completeConversationIntake) GetStepType() string { return completeConversationIntakeStepType }

func (completeConversationIntake) Execute(ctx dex.Context, result hiver.GetConversationResult) (*dex.StepDecision, error) {
	conversation := result.Value.Conversation
	outcome := IntakeOutcome{
		InboxID: conversation.InboxID, ConversationID: conversation.ID, Status: conversation.Status,
		PrivatePermalink: conversation.PrivatePermalink, Messages: result.Value.Messages,
	}
	if err := intakeOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
