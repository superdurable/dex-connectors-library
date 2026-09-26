// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package threadreply demonstrates Gmail Trigger, Query, RPC, and Mutation APIs.
package threadreply

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"

	gmail "github.com/superdurable/dex-connectors-library/connectors/google/gmail"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	ConnectionName       = "gmail-inbox"
	StartTriggerBinding  = "gmail-thread-reply-start"
	ReplyTriggerBinding  = "gmail-thread-reply-received"
	readMessageStepType  = "ReadReceivedEmail"
	replyMessageStepType = "ReplyToReceivedEmail"
)

var (
	threadStateAttribute = dex.DefineAttribute[ThreadState]("gmail-thread-reply-state")
	replyResultAttribute = dex.DefineAttribute[gmail.ReplyToMessageResult]("gmail-thread-reply-result")
)

type Status string

const (
	StatusWaitingForReply Status = "waitingForReply"
	StatusReplying        Status = "replying"
	StatusCompleted       Status = "completed"
)

type Input struct {
	EventID      string `json:"eventId"`
	PrimaryEmail string `json:"primaryEmail"`
	MessageID    string `json:"messageId"`
	ThreadID     string `json:"threadId"`
}

type ThreadState struct {
	Input          Input         `json:"input"`
	RootMessage    gmail.Message `json:"rootMessage"`
	ReplyEventID   string        `json:"replyEventId,omitempty"`
	ReplyMessageID string        `json:"replyMessageId,omitempty"`
	Status         Status        `json:"status"`
}

type ReplyResult struct {
	Accepted  bool   `json:"accepted"`
	Duplicate bool   `json:"duplicate"`
	Status    Status `json:"status"`
}

type ReceiveEmailReplyInput struct {
	EventID   string `json:"eventId"`
	MessageID string `json:"messageId"`
}

type Flow struct {
	dex.FlowDefaults
	connection gmail.Connection
}

func NewFlow(connection gmail.Connection) *Flow {
	return &Flow{connection: connection}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(initializeThread{}),
		dex.DefineStep(gmail.NewGetMessageStep(gmail.GetMessageStepConfig[Input]{
			StepType: readMessageStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "gmail", GroupLabel: "Gmail", Explanation: "Read the received Gmail message that started the Flow."},
			Connection:  flow.connection,
			MapToOperationInput: func(input Input) gmail.GetMessageInput {
				return gmail.GetMessageInput{MessageID: input.MessageID}
			},
			Read: sdkgo.GoTo(messageLoaded{}),
		})),
		dex.DefineStep(messageLoaded{}),
		dex.DefineStep(gmail.NewReplyToMessageStep(gmail.ReplyToMessageStepConfig[ThreadState]{
			StepType: replyMessageStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "gmail", GroupLabel: "Gmail", Explanation: "Reply after the received email Trigger invokes the typed RPC."},
			Connection:  flow.connection,
			MapToOperationInput: func(state ThreadState) gmail.ReplyToMessageInput {
				return gmail.ReplyToMessageInput{MessageID: state.ReplyMessageID, TextBody: "Processing complete."}
			},
			Sent:            sdkgo.GoTo(replySent{}),
			ResultAttribute: &replyResultAttribute,
		})),
		dex.DefineStep(replySent{}),
	}
}

func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.ReceiveEmailReply, &dex.RPCOptions{LockAttributes: []dex.AttributeLock{dex.LockAttribute(threadStateAttribute)}}),
		dex.DefineRPC(flow.GetThreadStatus, &dex.RPCOptions{LockAttributes: []dex.AttributeLock{dex.LockAttribute(threadStateAttribute)}}),
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

func (flow *Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{threadStateAttribute, replyResultAttribute}}
}

func (*Flow) GetConnectorTriggerBindings() []sdkgo.TriggerBindingDefinition {
	return []sdkgo.TriggerBindingDefinition{
		gmail.DefineMessageReceivedTriggerBinding(gmail.MessageReceivedTriggerBindingConfig{
			ConnectionName: ConnectionName, BindingName: StartTriggerBinding,
		}),
		gmail.DefineReplyReceivedTriggerBinding(gmail.ReplyReceivedTriggerBindingConfig{
			ConnectionName: ConnectionName, BindingName: ReplyTriggerBinding,
		}),
	}
}

func (flow *Flow) ReceiveEmailReply(ctx dex.Context, input ReceiveEmailReplyInput) (*dex.RPCResult[ReplyResult], error) {
	state, err := threadStateAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if state.ReplyEventID == input.EventID {
		return &dex.RPCResult[ReplyResult]{Output: ReplyResult{Duplicate: true, Status: state.Status}}, nil
	}
	if state.Status != StatusWaitingForReply {
		return &dex.RPCResult[ReplyResult]{Output: ReplyResult{Status: state.Status}}, nil
	}
	state.Status = StatusReplying
	state.ReplyEventID = input.EventID
	state.ReplyMessageID = input.MessageID
	if err := threadStateAttribute.Set(ctx, state); err != nil {
		return nil, err
	}
	return &dex.RPCResult[ReplyResult]{
		Output:    ReplyResult{Accepted: true, Status: state.Status},
		NextSteps: []dex.StepMovement{dex.MovementOf(sdkgo.StepRef[ThreadState](replyMessageStepType), state)},
	}, nil
}

func (*Flow) GetThreadStatus(ctx dex.Context, _ dex.None) (*dex.RPCResult[ThreadState], error) {
	state, err := threadStateAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[ThreadState]{Output: state}, nil
}

// dex:field attribute-key:gmail-thread-reply-state value-type:json editable:false description:"Gmail thread status"
// dex:field attribute-key:gmail-thread-reply-result value-type:object editable:false description:"Gmail completion reply result"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	state, replyResult, err := gmailThreadInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"gmail-thread-reply-state":  state,
		"gmail-thread-reply-result": replyResult,
	}}, nil
}

// dex:field attribute-key:gmail-thread-reply-state value-type:json editable:false description:"Gmail thread details"
// dex:field attribute-key:gmail-thread-reply-result value-type:object editable:false description:"Gmail completion reply provider result"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	state, replyResult, err := gmailThreadInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"gmail-thread-reply-state":  state,
		"gmail-thread-reply-result": replyResult,
	}}, nil
}

func gmailThreadInspection(ctx dex.Context) (ThreadState, gmail.ReplyToMessageResult, error) {
	state, err := threadStateAttribute.Get(ctx)
	if err != nil {
		return ThreadState{}, gmail.ReplyToMessageResult{}, err
	}
	replyResult, err := optionalReplyResult(ctx)
	if err != nil {
		return ThreadState{}, gmail.ReplyToMessageResult{}, err
	}
	return state, replyResult, nil
}

func optionalReplyResult(ctx dex.Context) (gmail.ReplyToMessageResult, error) {
	result, err := replyResultAttribute.Get(ctx)
	var missingAttribute *dex.AttributeNotFoundError
	if errors.As(err, &missingAttribute) {
		return gmail.ReplyToMessageResult{}, nil
	}
	return result, err
}

type readMessageOutput = gmail.GetMessageResult

// dex:group group-id:reply group-label:"Reply"
// dex:explanation text:"Persist the Gmail thread identity before reading its root message."
type initializeThread struct {
	dex.StepDefaultsNoWaitFor[Input]
}

func (initializeThread) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(threadStateAttribute)}}
}

func (initializeThread) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	if err := threadStateAttribute.Set(ctx, ThreadState{Input: input}); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[Input](readMessageStepType), input), nil
}

// dex:group group-id:reply group-label:"Reply"
// dex:explanation text:"Store the received Gmail message before waiting for a reply Trigger."
type messageLoaded struct {
	dex.StepDefaultsNoWaitFor[readMessageOutput]
}

func (messageLoaded) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(threadStateAttribute)}}
}

func (messageLoaded) Execute(ctx dex.Context, result readMessageOutput) (*dex.StepDecision, error) {
	state, err := threadStateAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	state.RootMessage = result.Value
	state.Status = StatusWaitingForReply
	if err := threadStateAttribute.Set(ctx, state); err != nil {
		return nil, err
	}
	return dex.DeadEnd(), nil
}

type replyMessageOutput = gmail.ReplyToMessageResult

// dex:group group-id:gmail group-label:"Gmail"
// dex:explanation text:"Complete after Gmail confirms the thread reply was sent."
type replySent struct {
	dex.StepDefaultsNoWaitFor[replyMessageOutput]
}

func (replySent) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(threadStateAttribute)}}
}

func (replySent) Execute(ctx dex.Context, _ replyMessageOutput) (*dex.StepDecision, error) {
	state, err := threadStateAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	state.Status = StatusCompleted
	if err := threadStateAttribute.Set(ctx, state); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(state), nil
}

// NewStartTriggerFilter creates the application's Gmail root-message admission rule.
func NewStartTriggerFilter(configuration gmail.MessageReceivedTriggerConfiguration) (sdkgo.TriggerFilter[gmail.MessageEvent], error) {
	if err := configuration.Validate(); err != nil {
		return nil, err
	}
	return newMessageTriggerFilter(configuration.MessageMatcher, false), nil
}

// NewReplyTriggerFilter creates the application's Gmail reply admission rule.
func NewReplyTriggerFilter(configuration gmail.ReplyReceivedTriggerConfiguration) (sdkgo.TriggerFilter[gmail.MessageEvent], error) {
	if err := configuration.Validate(); err != nil {
		return nil, err
	}
	return newMessageTriggerFilter(configuration.ReplyMatcher, true), nil
}

func ResolveFlowID(event sdkgo.TriggerEvent[gmail.MessageEvent]) string {
	return fmt.Sprintf("gmail-thread-reply-%s-%s", ConnectionName, event.Payload.ThreadID)
}

func MapToFlowInput(event sdkgo.TriggerEvent[gmail.MessageEvent]) Input {
	payload := event.Payload
	return Input{EventID: event.ID, PrimaryEmail: payload.PrimaryEmail, MessageID: payload.MessageID, ThreadID: payload.ThreadID}
}

func MapToReceiveEmailReplyInput(event sdkgo.TriggerEvent[gmail.MessageEvent]) ReceiveEmailReplyInput {
	return ReceiveEmailReplyInput{EventID: event.ID, MessageID: event.Payload.MessageID}
}

func newMessageTriggerFilter(matcher gmail.MessageMatcher, requiresReply bool) sdkgo.TriggerFilter[gmail.MessageEvent] {
	return func(event sdkgo.TriggerEvent[gmail.MessageEvent]) bool {
		message := event.Payload
		if event.ID == "" || message.PrimaryEmail == "" || message.MessageID == "" || message.ThreadID == "" || message.IsReply != requiresReply {
			return false
		}
		if matcher.MessageContains != "" {
			haystack := strings.ToLower(message.Subject + "\n" + message.Snippet)
			if !strings.Contains(haystack, strings.ToLower(matcher.MessageContains)) {
				return false
			}
		}
		if len(matcher.SenderEmails) == 0 {
			return true
		}
		sender, err := mail.ParseAddress(message.From)
		if err != nil {
			return false
		}
		for _, allowedSender := range matcher.SenderEmails {
			allowedAddress, err := mail.ParseAddress(allowedSender)
			if err == nil && strings.EqualFold(sender.Address, allowedAddress.Address) {
				return true
			}
		}
		return false
	}
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[ReceiveEmailReplyInput, ReplyResult] = (*Flow)(nil).ReceiveEmailReply
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
