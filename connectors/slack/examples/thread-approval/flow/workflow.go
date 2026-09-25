// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package threadapproval demonstrates Slack Trigger, Query, RPC, and Mutation APIs.
package threadapproval

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/superdurable/dex-connectors-library/connectors/slack"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	ConnectionName         = "slack-workspace"
	StartTriggerBinding    = "slack-thread-approval-start"
	ReplyTriggerBinding    = "slack-thread-approval-reply"
	readThreadStepType     = "ReadSlackThread"
	postCompletionStepType = "PostSlackCompletion"
)

var (
	threadStateAttribute = dex.DefineAttribute[ThreadState]("slack-thread-approval-state")
	postReplyResult      = dex.DefineAttribute[sdkgo.MutationResult[slack.PostMessageOutput]]("slack-thread-approval-post-reply-result")
)

type Status string

const (
	StatusWaitingForReply Status = "waitingForReply"
	StatusPostingReply    Status = "postingReply"
	StatusNeedsRecovery   Status = "needsRecovery"
	StatusCompleted       Status = "completed"
)

type Input struct {
	EventID         string `json:"eventId"`
	TeamID          string `json:"teamId"`
	ChannelID       string `json:"channelId"`
	ThreadTimestamp string `json:"threadTimestamp"`
}

type ThreadState struct {
	Input          Input           `json:"input"`
	Messages       []slack.Message `json:"messages"`
	Status         Status          `json:"status"`
	ReplyUserID    string          `json:"replyUserId,omitempty"`
	FailureMessage string          `json:"failureMessage,omitempty"`
}

type ReplyResult struct {
	Accepted  bool   `json:"accepted"`
	Duplicate bool   `json:"duplicate"`
	Status    Status `json:"status"`
}

type Flow struct {
	dex.FlowDefaults
	connection      slack.Connection
	replyTriggerRPC *sdkgo.TriggerRPC[slack.MessageEvent, ReplyResult]
}

func NewFlow(connection slack.Connection) *Flow {
	flow := &Flow{connection: connection}
	flow.replyTriggerRPC = sdkgo.MustNewTriggerRPC(sdkgo.TriggerRPCConfig[slack.MessageEvent, ReplyResult]{
		Definition: flow.ReceiveThreadReply, ProcessedEventIDsAttributeName: "slack-thread-approval-processed-reply-event-ids",
		HandleEvent: flow.handleThreadReply, DuplicateEvent: flow.handleDuplicateThreadReply,
		Options: &dex.RPCOptions{LockAttributes: []dex.AttributeLock{dex.LockAttribute(threadStateAttribute)}},
	})
	return flow
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(slack.NewListThreadMessagesStep(slack.ListThreadMessagesStepConfig[Input]{
			StepType:       readThreadStepType,
			ConnectionName: ConnectionName,
			Presentation: sdkgo.StepPresentation{
				GroupID: "slack", GroupLabel: "Slack", Explanation: "Read the messages in the newly created Slack thread.",
			},
			Connection: flow.connection,
			BuildInput: func(input Input) (slack.ListThreadMessagesInput, error) {
				return slack.ListThreadMessagesInput{ChannelID: input.ChannelID, ThreadTimestamp: input.ThreadTimestamp, PageSize: 15}, nil
			},
			Read: sdkgo.GoTo(threadLoaded{}), Rejected: sdkgo.GoTo(threadReadFailed{}),
			Defect: sdkgo.GoTo(threadReadFailed{}),
		})),
		dex.DefineStep(threadLoaded{}),
		dex.DefineStep(threadReadFailed{}),
		dex.DefineStep(slack.NewPostThreadReplyStep(slack.PostThreadReplyStepConfig[ThreadState]{
			StepType:       postCompletionStepType,
			ConnectionName: ConnectionName,
			Presentation: sdkgo.StepPresentation{
				GroupID: "slack", GroupLabel: "Slack", Explanation: "Reply to the Slack thread after the configured reply Trigger invokes the RPC.",
			},
			Connection: flow.connection,
			BuildInput: func(state ThreadState) (slack.PostThreadReplyInput, error) {
				return slack.PostThreadReplyInput{
					ChannelID: state.Input.ChannelID, ThreadTimestamp: state.Input.ThreadTimestamp,
					Text: fmt.Sprintf("Processing complete (approved by <@%s>)", state.ReplyUserID),
				}, nil
			},
			Sent: sdkgo.GoTo(completionPosted{}), Rejected: sdkgo.GoTo(completionNeedsRecovery{}),
			Uncertain: sdkgo.GoTo(completionNeedsRecovery{}), Defect: sdkgo.GoTo(completionNeedsRecovery{}),
			ResultAttribute: &postReplyResult,
		})),
		dex.DefineStep(completionPosted{}),
		dex.DefineStep(completionNeedsRecovery{}),
	}
}

func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.replyTriggerRPC.Definition(), flow.replyTriggerRPC.DefaultOptions()),
		dex.DefineRPC(flow.GetThreadStatus, &dex.RPCOptions{LockAttributes: []dex.AttributeLock{dex.LockAttribute(threadStateAttribute)}}),
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

func (flow *Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{
		threadStateAttribute, postReplyResult, flow.replyTriggerRPC.PersistenceAttribute(),
	}}
}

func (*Flow) GetConnectorTriggerBindings() []sdkgo.TriggerBindingDefinition {
	return []sdkgo.TriggerBindingDefinition{
		slack.DefineChannelThreadCreatedTriggerBinding(slack.ChannelThreadCreatedTriggerBindingConfig{
			ConnectionName: ConnectionName, BindingName: StartTriggerBinding,
		}),
		slack.DefineThreadReplyCreatedTriggerBinding(slack.ThreadReplyCreatedTriggerBindingConfig{
			ConnectionName: ConnectionName, BindingName: ReplyTriggerBinding,
		}),
	}
}

func (flow *Flow) ReceiveThreadReply(
	ctx dex.Context,
	event sdkgo.TriggerEvent[slack.MessageEvent],
) (*dex.RPCResult[ReplyResult], error) {
	result, err := flow.replyTriggerRPC.Handle(ctx, event)
	if err != nil {
		return nil, err
	}
	if result.Output.Accepted && !result.Output.Duplicate {
		state, err := threadStateAttribute.Get(ctx)
		if err != nil {
			return nil, err
		}
		return &dex.RPCResult[ReplyResult]{
			Output: result.Output,
			NextSteps: []dex.StepMovement{
				dex.MovementOf(sdkgo.StepRef[ThreadState](postCompletionStepType), state),
			},
		}, nil
	}
	return result, nil
}

func (flow *Flow) ReplyTriggerRPC() *sdkgo.TriggerRPC[slack.MessageEvent, ReplyResult] {
	return flow.replyTriggerRPC
}

func (*Flow) handleThreadReply(
	ctx dex.Context,
	event sdkgo.TriggerEvent[slack.MessageEvent],
) (*dex.RPCResult[ReplyResult], error) {
	state, err := threadStateAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	accepted := state.Status == StatusWaitingForReply
	if accepted {
		state.Status = StatusPostingReply
		state.ReplyUserID = event.Payload.UserID
		state.FailureMessage = ""
		if err := threadStateAttribute.Set(ctx, state); err != nil {
			return nil, err
		}
	}
	return &dex.RPCResult[ReplyResult]{Output: ReplyResult{Accepted: accepted, Status: state.Status}}, nil
}

func (*Flow) handleDuplicateThreadReply(
	ctx dex.Context,
	_ sdkgo.TriggerEvent[slack.MessageEvent],
) (*dex.RPCResult[ReplyResult], error) {
	state, err := threadStateAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[ReplyResult]{Output: ReplyResult{Duplicate: true, Status: state.Status}}, nil
}

func (*Flow) GetThreadStatus(ctx dex.Context, _ dex.None) (*dex.RPCResult[ThreadState], error) {
	state, err := threadStateAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[ThreadState]{Output: state}, nil
}

// dex:field attribute-key:slack-thread-approval-state value-type:json editable:false description:"Slack thread status"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	state, err := threadStateAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"slack-thread-approval-state": state}}, nil
}

// dex:field attribute-key:slack-thread-approval-state value-type:json editable:false description:"Slack thread details"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	state, err := threadStateAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"slack-thread-approval-state": state}}, nil
}

type threadQueryOutput = slack.ListThreadMessagesStepOutput[Input]

// dex:group group-id:approval group-label:"Approval"
// dex:explanation text:"Store the Slack thread messages before waiting for a matching reply Trigger."
type threadLoaded struct {
	dex.StepDefaultsNoWaitFor[threadQueryOutput]
}

func (threadLoaded) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(threadStateAttribute)}}
}

func (threadLoaded) Execute(ctx dex.Context, output threadQueryOutput) (*dex.StepDecision, error) {
	state := ThreadState{Input: output.Input, Messages: output.Result.Value.Messages, Status: StatusWaitingForReply}
	if err := threadStateAttribute.Set(ctx, state); err != nil {
		return nil, err
	}
	return dex.DeadEnd(), nil
}

// dex:group group-id:recovery group-label:"Recovery"
// dex:explanation text:"Fail when Slack cannot provide the initial thread messages."
type threadReadFailed struct {
	dex.StepDefaultsNoWaitFor[threadQueryOutput]
}

func (threadReadFailed) Execute(_ dex.Context, output threadQueryOutput) (*dex.StepDecision, error) {
	return dex.ForceFail(failureMessage(output.Result.Branch, output.Result.Failure)), nil
}

type postReplyOutput = slack.PostThreadReplyStepOutput[ThreadState]

// dex:group group-id:slack group-label:"Slack"
// dex:explanation text:"Complete after Slack confirms the thread reply was posted."
type completionPosted struct {
	dex.StepDefaultsNoWaitFor[postReplyOutput]
}

func (completionPosted) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(threadStateAttribute)}}
}

func (completionPosted) Execute(ctx dex.Context, output postReplyOutput) (*dex.StepDecision, error) {
	state := output.Input
	state.Status = StatusCompleted
	if err := threadStateAttribute.Set(ctx, state); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(state), nil
}

// dex:group group-id:recovery group-label:"Recovery"
// dex:explanation text:"Pause after a rejected or uncertain Slack reply for explicit recovery."
type completionNeedsRecovery struct {
	dex.StepDefaultsNoWaitFor[postReplyOutput]
}

func (completionNeedsRecovery) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(threadStateAttribute)}}
}

func (completionNeedsRecovery) Execute(ctx dex.Context, output postReplyOutput) (*dex.StepDecision, error) {
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

func ResolveFlowID(identity slack.ThreadIdentity) (string, error) {
	if identity.TeamID == "" || identity.ChannelID == "" || identity.RootTimestamp == "" {
		return "", fmt.Errorf("Slack thread identity is incomplete")
	}
	digest := sha256.Sum256([]byte(identity.TeamID + "\x00" + identity.ChannelID + "\x00" + identity.RootTimestamp))
	return "slack-thread-approval-" + hex.EncodeToString(digest[:16]), nil
}

func BuildStartInput(event sdkgo.TriggerEvent[slack.MessageEvent]) (Input, error) {
	payload := event.Payload
	return Input{
		EventID: event.ID, TeamID: payload.TeamID, ChannelID: payload.ChannelID, ThreadTimestamp: payload.ThreadTimestamp,
	}, nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[sdkgo.TriggerEvent[slack.MessageEvent], ReplyResult] = (*Flow)(nil).ReceiveThreadReply
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
