// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package threadapproval demonstrates Slack Trigger, Query, RPC, and Mutation APIs.
package threadapproval

import (
	"errors"
	"fmt"
	"strings"

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
	threadStateAttribute     = dex.DefineAttribute[ThreadState]("slack-thread-approval-state")
	postReplyResultAttribute = dex.DefineAttribute[slack.PostThreadReplyResult]("slack-thread-approval-post-reply-result")
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
	ReplyEventID   string          `json:"replyEventId,omitempty"`
	ReplyUserID    string          `json:"replyUserId,omitempty"`
	FailureMessage string          `json:"failureMessage,omitempty"`
}

type ReplyResult struct {
	Accepted  bool   `json:"accepted"`
	Duplicate bool   `json:"duplicate"`
	Status    Status `json:"status"`
}

type ReceiveThreadReplyInput struct {
	EventID string `json:"eventId"`
	UserID  string `json:"userId"`
}

type Flow struct {
	dex.FlowDefaults
	connection slack.Connection
}

func NewFlow(connection slack.Connection) *Flow {
	return &Flow{connection: connection}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(initializeThread{}),
		dex.DefineStep(slack.NewListThreadMessagesStep(slack.ListThreadMessagesStepConfig[Input]{
			StepType:       readThreadStepType,
			ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "slack", GroupLabel: "Slack", Explanation: "Read the messages in the newly created Slack thread.",
			},
			Connection: flow.connection,
			MapToOperationInput: func(input Input) slack.ListThreadMessagesInput {
				return slack.ListThreadMessagesInput{ChannelID: input.ChannelID, ThreadTimestamp: input.ThreadTimestamp, PageSize: 15}
			},
			Read: sdkgo.GoTo(threadLoaded{}), ProviderRejected: sdkgo.GoTo(threadReadFailed{}),
			InvalidResponse: sdkgo.GoTo(threadReadFailed{}), Defect: sdkgo.GoTo(threadReadFailed{}),
		})),
		dex.DefineStep(threadLoaded{}),
		dex.DefineStep(threadReadFailed{}),
		dex.DefineStep(slack.NewPostThreadReplyStep(slack.PostThreadReplyStepConfig[ThreadState]{
			StepType:       postCompletionStepType,
			ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "slack", GroupLabel: "Slack", Explanation: "Reply to the Slack thread after the configured reply Trigger invokes the RPC.",
			},
			Connection: flow.connection,
			MapToOperationInput: func(state ThreadState) slack.PostThreadReplyInput {
				return slack.PostThreadReplyInput{
					ChannelID: state.Input.ChannelID, ThreadTimestamp: state.Input.ThreadTimestamp,
					Text: "Processing complete.",
				}
			},
			Sent: sdkgo.GoTo(completionPosted{}), ProviderRejected: sdkgo.GoTo(completionNeedsRecovery{}),
			Uncertain: sdkgo.GoTo(completionNeedsRecovery{}), Defect: sdkgo.GoTo(completionNeedsRecovery{}),
			ResultAttribute: &postReplyResultAttribute,
		})),
		dex.DefineStep(completionPosted{}),
		dex.DefineStep(completionNeedsRecovery{}),
	}
}

func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.ReceiveThreadReply, &dex.RPCOptions{LockAttributes: []dex.AttributeLock{dex.LockAttribute(threadStateAttribute)}}),
		dex.DefineRPC(flow.GetThreadStatus, &dex.RPCOptions{LockAttributes: []dex.AttributeLock{dex.LockAttribute(threadStateAttribute)}}),
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

func (flow *Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{threadStateAttribute, postReplyResultAttribute}}
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
	input ReceiveThreadReplyInput,
) (*dex.RPCResult[ReplyResult], error) {
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
	state.Status = StatusPostingReply
	state.ReplyEventID = input.EventID
	state.ReplyUserID = input.UserID
	state.FailureMessage = ""
	if err := threadStateAttribute.Set(ctx, state); err != nil {
		return nil, err
	}
	return &dex.RPCResult[ReplyResult]{
		Output: ReplyResult{Accepted: true, Status: state.Status},
		NextSteps: []dex.StepMovement{
			dex.MovementOf(sdkgo.StepRef[ThreadState](postCompletionStepType), state),
		},
	}, nil
}

func (*Flow) GetThreadStatus(ctx dex.Context, _ dex.None) (*dex.RPCResult[ThreadState], error) {
	state, err := threadStateAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[ThreadState]{Output: state}, nil
}

// dex:field attribute-key:slack-thread-approval-state value-type:json editable:false description:"Slack thread status"
// dex:field attribute-key:slack-thread-approval-post-reply-result value-type:object editable:false description:"Slack completion reply result"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	state, postReplyResult, err := slackThreadInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"slack-thread-approval-state":             state,
		"slack-thread-approval-post-reply-result": postReplyResult,
	}}, nil
}

// dex:field attribute-key:slack-thread-approval-state value-type:json editable:false description:"Slack thread details"
// dex:field attribute-key:slack-thread-approval-post-reply-result value-type:object editable:false description:"Slack completion reply provider result"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	state, postReplyResult, err := slackThreadInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"slack-thread-approval-state":             state,
		"slack-thread-approval-post-reply-result": postReplyResult,
	}}, nil
}

func slackThreadInspection(ctx dex.Context) (ThreadState, slack.PostThreadReplyResult, error) {
	state, err := threadStateAttribute.Get(ctx)
	if err != nil {
		return ThreadState{}, slack.PostThreadReplyResult{}, err
	}
	postReplyResult, err := optionalPostReplyResult(ctx)
	if err != nil {
		return ThreadState{}, slack.PostThreadReplyResult{}, err
	}
	return state, postReplyResult, nil
}

func optionalPostReplyResult(ctx dex.Context) (slack.PostThreadReplyResult, error) {
	result, err := postReplyResultAttribute.Get(ctx)
	var missingAttribute *dex.AttributeNotFoundError
	if errors.As(err, &missingAttribute) {
		return slack.PostThreadReplyResult{}, nil
	}
	return result, err
}

type threadQueryOutput = slack.ListThreadMessagesResult

// dex:group group-id:approval group-label:"Approval"
// dex:explanation text:"Persist the Slack thread identity before reading its messages."
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
	return dex.GoTo(sdkgo.StepRef[Input](readThreadStepType), input), nil
}

// dex:group group-id:approval group-label:"Approval"
// dex:explanation text:"Store the Slack thread messages before waiting for a matching reply Trigger."
type threadLoaded struct {
	dex.StepDefaultsNoWaitFor[threadQueryOutput]
}

func (threadLoaded) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(threadStateAttribute)}}
}

func (threadLoaded) Execute(ctx dex.Context, result threadQueryOutput) (*dex.StepDecision, error) {
	state, err := threadStateAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	state.Messages = result.Value.Messages
	state.Status = StatusWaitingForReply
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

func (threadReadFailed) Execute(_ dex.Context, result threadQueryOutput) (*dex.StepDecision, error) {
	return dex.ForceFail(failureMessage(result.Branch, result.Failure)), nil
}

type postReplyOutput = slack.PostThreadReplyResult

// dex:group group-id:slack group-label:"Slack"
// dex:explanation text:"Complete after Slack confirms the thread reply was posted."
type completionPosted struct {
	dex.StepDefaultsNoWaitFor[postReplyOutput]
}

func (completionPosted) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(threadStateAttribute)}}
}

func (completionPosted) Execute(ctx dex.Context, _ postReplyOutput) (*dex.StepDecision, error) {
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

// dex:group group-id:recovery group-label:"Recovery"
// dex:explanation text:"Pause after a terminal or uncertain Slack reply outcome for explicit recovery."
type completionNeedsRecovery struct {
	dex.StepDefaultsNoWaitFor[postReplyOutput]
}

func (completionNeedsRecovery) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(threadStateAttribute)}}
}

func (completionNeedsRecovery) Execute(ctx dex.Context, result postReplyOutput) (*dex.StepDecision, error) {
	state, err := threadStateAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	state.Status = StatusNeedsRecovery
	state.FailureMessage = failureMessage(result.Branch, result.Failure)
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

// NewStartTriggerFilter creates the application's Slack root-message admission rule.
func NewStartTriggerFilter(configuration slack.ChannelThreadCreatedTriggerConfiguration) (sdkgo.TriggerFilter[slack.MessageEvent], error) {
	if err := configuration.Validate(); err != nil {
		return nil, err
	}
	return newMessageTriggerFilter(configuration.ChannelID, configuration.ThreadTriggerMatcher, false), nil
}

// NewReplyTriggerFilter creates the application's Slack reply admission rule.
func NewReplyTriggerFilter(configuration slack.ThreadReplyCreatedTriggerConfiguration) (sdkgo.TriggerFilter[slack.MessageEvent], error) {
	if err := configuration.Validate(); err != nil {
		return nil, err
	}
	return newMessageTriggerFilter(configuration.ChannelID, configuration.ThreadReplyMatcher, true), nil
}

func ResolveFlowID(event sdkgo.TriggerEvent[slack.MessageEvent]) string {
	message := event.Payload
	return fmt.Sprintf("slack-thread-approval-%s-%s-%s", message.TeamID, message.ChannelID, message.ThreadTimestamp)
}

func MapToFlowInput(event sdkgo.TriggerEvent[slack.MessageEvent]) Input {
	payload := event.Payload
	return Input{
		EventID: event.ID, TeamID: payload.TeamID, ChannelID: payload.ChannelID, ThreadTimestamp: payload.ThreadTimestamp,
	}
}

func MapToReceiveThreadReplyInput(event sdkgo.TriggerEvent[slack.MessageEvent]) ReceiveThreadReplyInput {
	return ReceiveThreadReplyInput{EventID: event.ID, UserID: event.Payload.UserID}
}

func newMessageTriggerFilter(channelID string, matcher slack.MessageMatcher, requiresReply bool) sdkgo.TriggerFilter[slack.MessageEvent] {
	return func(event sdkgo.TriggerEvent[slack.MessageEvent]) bool {
		message := event.Payload
		isReply := message.ThreadTimestamp != "" && message.ThreadTimestamp != message.Timestamp
		if event.ID == "" || message.TeamID == "" || message.ChannelID != channelID || message.ThreadTimestamp == "" || message.UserID == "" || isReply != requiresReply {
			return false
		}
		if matcher.MessageContains != "" && !strings.Contains(strings.ToLower(message.Text), strings.ToLower(matcher.MessageContains)) {
			return false
		}
		if len(matcher.PosterUserIDs) == 0 {
			return true
		}
		for _, allowedUserID := range matcher.PosterUserIDs {
			if message.UserID == allowedUserID {
				return true
			}
		}
		return false
	}
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[ReceiveThreadReplyInput, ReplyResult] = (*Flow)(nil).ReceiveThreadReply
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
