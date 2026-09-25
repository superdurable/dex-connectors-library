// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package threadreply demonstrates Gmail Trigger, Query, RPC, and Mutation APIs.
package threadreply

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

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
	replyResultAttribute = dex.DefineAttribute[sdkgo.MutationResult[gmail.SendMessageOutput]]("gmail-thread-reply-result")
)

type Status string

const (
	StatusWaitingForReply Status = "waitingForReply"
	StatusReplying        Status = "replying"
	StatusNeedsRecovery   Status = "needsRecovery"
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
	FailureMessage string        `json:"failureMessage,omitempty"`
}

type ReplyResult struct {
	Accepted  bool   `json:"accepted"`
	Duplicate bool   `json:"duplicate"`
	Status    Status `json:"status"`
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
		dex.DefineStartStep(gmail.NewGetMessageStep(gmail.GetMessageStepConfig[Input]{
			StepType: readMessageStepType, ConnectionName: ConnectionName,
			Presentation: sdkgo.StepPresentation{GroupID: "gmail", GroupLabel: "Gmail", Explanation: "Read the received Gmail message that started the Flow."},
			Connection:   flow.connection,
			BuildInput: func(input Input) (gmail.GetMessageInput, error) {
				return gmail.GetMessageInput{MessageID: input.MessageID}, nil
			},
			Read: sdkgo.GoTo(messageLoaded{}), NotFound: sdkgo.GoTo(messageReadFailed{}),
			Rejected: sdkgo.GoTo(messageReadFailed{}), Defect: sdkgo.GoTo(messageReadFailed{}),
		})),
		dex.DefineStep(messageLoaded{}),
		dex.DefineStep(messageReadFailed{}),
		dex.DefineStep(gmail.NewReplyToMessageStep(gmail.ReplyToMessageStepConfig[ThreadState]{
			StepType: replyMessageStepType, ConnectionName: ConnectionName,
			Presentation: sdkgo.StepPresentation{GroupID: "gmail", GroupLabel: "Gmail", Explanation: "Reply after the received email Trigger invokes the typed RPC."},
			Connection:   flow.connection,
			BuildInput: func(state ThreadState) (gmail.ReplyToMessageInput, error) {
				return gmail.ReplyToMessageInput{MessageID: state.ReplyMessageID, TextBody: "Processing complete"}, nil
			},
			Sent: sdkgo.GoTo(replySent{}), Rejected: sdkgo.GoTo(replyNeedsRecovery{}),
			Uncertain: sdkgo.GoTo(replyNeedsRecovery{}), Defect: sdkgo.GoTo(replyNeedsRecovery{}),
			ResultAttribute: &replyResultAttribute,
		})),
		dex.DefineStep(replySent{}),
		dex.DefineStep(replyNeedsRecovery{}),
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
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{
		threadStateAttribute, replyResultAttribute,
	}}
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

func (flow *Flow) ReceiveEmailReply(ctx dex.Context, event sdkgo.TriggerEvent[gmail.MessageEvent]) (*dex.RPCResult[ReplyResult], error) {
	state, err := threadStateAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if state.ReplyEventID == event.ID {
		return &dex.RPCResult[ReplyResult]{Output: ReplyResult{Duplicate: true, Status: state.Status}}, nil
	}
	if state.Status != StatusWaitingForReply {
		return &dex.RPCResult[ReplyResult]{Output: ReplyResult{Status: state.Status}}, nil
	}
	state.Status = StatusReplying
	state.ReplyEventID = event.ID
	state.ReplyMessageID = event.Payload.MessageID
	state.FailureMessage = ""
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
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	state, err := threadStateAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"gmail-thread-reply-state": state}}, nil
}

// dex:field attribute-key:gmail-thread-reply-state value-type:json editable:false description:"Gmail thread details"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	state, err := threadStateAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"gmail-thread-reply-state": state}}, nil
}

type readMessageOutput = gmail.GetMessageStepOutput[Input]

// dex:group group-id:reply group-label:"Reply"
// dex:explanation text:"Store the received Gmail message before waiting for a reply Trigger."
type messageLoaded struct {
	dex.StepDefaultsNoWaitFor[readMessageOutput]
}

func (messageLoaded) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(threadStateAttribute)}}
}

func (messageLoaded) Execute(ctx dex.Context, output readMessageOutput) (*dex.StepDecision, error) {
	state := ThreadState{Input: output.Input, RootMessage: output.Result.Value, Status: StatusWaitingForReply}
	if err := threadStateAttribute.Set(ctx, state); err != nil {
		return nil, err
	}
	return dex.DeadEnd(), nil
}

// dex:group group-id:recovery group-label:"Recovery"
// dex:explanation text:"Fail when Gmail cannot provide the message that started the Flow."
type messageReadFailed struct {
	dex.StepDefaultsNoWaitFor[readMessageOutput]
}

func (messageReadFailed) Execute(_ dex.Context, output readMessageOutput) (*dex.StepDecision, error) {
	return dex.ForceFail(failureMessage(output.Result.Branch, output.Result.Failure)), nil
}

type replyMessageOutput = gmail.ReplyToMessageStepOutput[ThreadState]

// dex:group group-id:gmail group-label:"Gmail"
// dex:explanation text:"Complete after Gmail confirms the thread reply was sent."
type replySent struct {
	dex.StepDefaultsNoWaitFor[replyMessageOutput]
}

func (replySent) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(threadStateAttribute)}}
}

func (replySent) Execute(ctx dex.Context, output replyMessageOutput) (*dex.StepDecision, error) {
	state := output.Input
	state.Status = StatusCompleted
	if err := threadStateAttribute.Set(ctx, state); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(state), nil
}

// dex:group group-id:recovery group-label:"Recovery"
// dex:explanation text:"Pause after a rejected or uncertain Gmail reply for explicit recovery."
type replyNeedsRecovery struct {
	dex.StepDefaultsNoWaitFor[replyMessageOutput]
}

func (replyNeedsRecovery) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(threadStateAttribute)}}
}

func (replyNeedsRecovery) Execute(ctx dex.Context, output replyMessageOutput) (*dex.StepDecision, error) {
	state := output.Input
	state.Status = StatusNeedsRecovery
	state.FailureMessage = failureMessage(output.Result.Branch, output.Result.Failure)
	if err := threadStateAttribute.Set(ctx, state); err != nil {
		return nil, err
	}
	return dex.DeadEnd(), nil
}

func failureMessage(branch sdkgo.BranchID, failure *sdkgo.Failure) string {
	if failure == nil {
		return string(branch)
	}
	return fmt.Sprintf("%s: %s", branch, failure.Message)
}

func ResolveFlowID(identity gmail.ThreadIdentity) (string, error) {
	if identity.PrimaryEmail == "" || identity.ThreadID == "" {
		return "", fmt.Errorf("Gmail thread identity is incomplete")
	}
	digest := sha256.Sum256([]byte(identity.PrimaryEmail + "\x00" + identity.ThreadID))
	return "gmail-thread-reply-" + hex.EncodeToString(digest[:16]), nil
}

func BuildStartInput(event sdkgo.TriggerEvent[gmail.MessageEvent]) (Input, error) {
	payload := event.Payload
	return Input{EventID: event.ID, PrimaryEmail: payload.PrimaryEmail, MessageID: payload.MessageID, ThreadID: payload.ThreadID}, nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[sdkgo.TriggerEvent[gmail.MessageEvent], ReplyResult] = (*Flow)(nil).ReceiveEmailReply
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
