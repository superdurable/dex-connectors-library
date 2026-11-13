// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package buttonapproval demonstrates Slack button messages and the buttonClicked Trigger.
package buttonapproval

import (
	"errors"
	"fmt"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/slack/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	ConnectionName       = "slack-button-approval"
	StartTriggerBinding  = "slack-button-approval-start"
	ButtonTriggerBinding = "slack-button-approval-decision"
	// ApproveActionID and RejectActionID are the button action IDs that the posted message carries and the
	// button Trigger filter accepts. They are code constants, so a user never types them.
	ApproveActionID        = "approve"
	RejectActionID         = "reject"
	postButtonsStepType    = "PostSlackApprovalButtons"
	postDecisionStepType   = "PostSlackDecision"
	approvalRequestText    = "Approve this request?"
	approvedDecisionFormat = "Approved by <@%s>."
	rejectedDecisionFormat = "Rejected by <@%s>."
)

var errApprovalNotReady = errors.New("Slack approval buttons are still being posted")

var approvalStateAttribute = dex.DefineAttribute[ApprovalState]("slack-button-approval-state")

// Status is the approval's lifecycle position.
type Status string

const (
	StatusPostingButtons     Status = "postingButtons"
	StatusWaitingForDecision Status = "waitingForDecision"
	StatusPostingDecision    Status = "postingDecision"
	StatusCompleted          Status = "completed"
)

// Decision is the clicked button's outcome.
type Decision string

const (
	DecisionApproved Decision = "approved"
	DecisionRejected Decision = "rejected"
)

// Input identifies the Slack thread whose root message started the approval.
type Input struct {
	EventID         string `json:"eventId"`
	TeamID          string `json:"teamId"`
	ChannelID       string `json:"channelId"`
	ThreadTimestamp string `json:"threadTimestamp"`
	RequesterUserID string `json:"requesterUserId"`
}

// ApprovalState is the Flow's durable approval record.
type ApprovalState struct {
	Input                  Input    `json:"input"`
	Status                 Status   `json:"status"`
	ButtonMessageTimestamp string   `json:"buttonMessageTimestamp,omitempty"`
	Decision               Decision `json:"decision,omitempty"`
	DecisionEventID        string   `json:"decisionEventId,omitempty"`
	DeciderUserID          string   `json:"deciderUserId,omitempty"`
}

// ReceiveButtonDecisionInput is the click that the button Trigger passes to the RPC.
type ReceiveButtonDecisionInput struct {
	EventID          string `json:"eventId"`
	ActionID         string `json:"actionId"`
	UserID           string `json:"userId"`
	MessageTimestamp string `json:"messageTimestamp"`
}

// DecisionResult reports whether the click decided the approval.
type DecisionResult struct {
	Accepted  bool   `json:"accepted"`
	Duplicate bool   `json:"duplicate"`
	Status    Status `json:"status"`
}

type ButtonApprovalFlow struct {
	dex.FlowDefaults
	connection slack.Connection
}

func NewFlow(connection slack.Connection) *ButtonApprovalFlow {
	return &ButtonApprovalFlow{connection: connection}
}

func (flow *ButtonApprovalFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(initializeApproval{}),
		dex.DefineStep(slack.NewPostButtonMessageStep(slack.PostButtonMessageStepConfig[ApprovalState]{
			StepType:       postButtonsStepType,
			ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "request", GroupLabel: "Approval request", Explanation: "Reply in the Slack thread with Approve and Reject buttons.",
			},
			Connection: flow.connection,
			MapToOperationInput: func(state ApprovalState) slack.PostButtonMessageInput {
				return slack.PostButtonMessageInput{
					ChannelID: state.Input.ChannelID, ThreadTimestamp: state.Input.ThreadTimestamp, Text: approvalRequestText,
					Buttons: []slack.MessageButton{
						{ActionID: ApproveActionID, Text: "Approve", Value: state.Input.ThreadTimestamp, Style: slack.ButtonStylePrimary},
						{ActionID: RejectActionID, Text: "Reject", Value: state.Input.ThreadTimestamp, Style: slack.ButtonStyleDanger},
					},
				}
			},
			Sent: sdkgo.GoTo(buttonsPosted{}),
		})),
		dex.DefineStep(buttonsPosted{}),
		dex.DefineStep(slack.NewPostThreadReplyStep(slack.PostThreadReplyStepConfig[ApprovalState]{
			StepType:       postDecisionStepType,
			ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "decision", GroupLabel: "Decision", Explanation: "Reply in the Slack thread with the decision and who made it.",
			},
			Connection: flow.connection,
			MapToOperationInput: func(state ApprovalState) slack.PostThreadReplyInput {
				format := approvedDecisionFormat
				if state.Decision == DecisionRejected {
					format = rejectedDecisionFormat
				}
				return slack.PostThreadReplyInput{
					ChannelID: state.Input.ChannelID, ThreadTimestamp: state.Input.ThreadTimestamp,
					Text: fmt.Sprintf(format, state.DeciderUserID),
				}
			},
			Sent: sdkgo.GoTo(decisionPosted{}),
		})),
		dex.DefineStep(decisionPosted{}),
	}
}

func (flow *ButtonApprovalFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.ReceiveButtonDecision, &dex.RPCOptions{LockAttributes: []dex.AttributeLock{dex.LockAttribute(approvalStateAttribute)}}),
		dex.DefineRPC(flow.GetApprovalStatus, &dex.RPCOptions{LockAttributes: []dex.AttributeLock{dex.LockAttribute(approvalStateAttribute)}}),
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

func (flow *ButtonApprovalFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{approvalStateAttribute}}
}

func (*ButtonApprovalFlow) GetConnectorTriggerBindings() []sdkgo.TriggerBindingDefinition {
	return []sdkgo.TriggerBindingDefinition{
		slack.DefineChannelThreadCreatedTriggerBinding(slack.ChannelThreadCreatedTriggerBindingConfig{
			ConnectionName: ConnectionName, BindingName: StartTriggerBinding,
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{
				{ID: "channel", UnitID: slack.UIUnitChannelPicker, Label: "Approval channel", Description: "Select the joined Slack channel whose new root messages start an approval; the picker stores its stable channel ID, and the Flow replies there with Approve and Reject buttons.", Required: true, Bindings: []sdkgo.ConnectorUIBinding{{Port: slack.UIChannelPickerPortChannelID, JSONPointer: "/channelId"}}},
				{ID: "message", UnitID: slack.UIUnitTextInput, Label: "Request message contains", Description: "Enter plain text that the root message must contain, such as request approval; matching ignores case, and blank matches any message content.", Bindings: []sdkgo.ConnectorUIBinding{{Port: slack.UITextInputPortText, JSONPointer: "/threadTriggerMatcher/messageContains"}}},
				{ID: "members", UnitID: slack.UIUnitMemberPicker, Label: "Members allowed to request", Description: "Select workspace members whose root messages may start an approval; leave empty to allow any member posting in the selected channel.", Bindings: []sdkgo.ConnectorUIBinding{{Port: slack.UIMemberPickerPortMemberIDs, JSONPointer: "/threadTriggerMatcher/posterUserIds"}}},
			}},
		}),
		slack.DefineButtonClickedTriggerBinding(slack.ButtonClickedTriggerBindingConfig{
			ConnectionName: ConnectionName, BindingName: ButtonTriggerBinding,
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{
				{ID: "channel", UnitID: slack.UIUnitChannelPicker, Label: "Approval channel", Description: "Select the same joined Slack channel as the request Trigger; clicks on Approve and Reject in other channels are ignored, and the picker stores the stable channel ID.", Required: true, Bindings: []sdkgo.ConnectorUIBinding{{Port: slack.UIChannelPickerPortChannelID, JSONPointer: "/channelId"}}},
				{ID: "approvers", UnitID: slack.UIUnitMemberPicker, Label: "Members allowed to decide", Description: "Select the workspace members whose Approve or Reject click decides; a click by anyone else is ignored, and an empty selection lets any member who can see the message decide.", Bindings: []sdkgo.ConnectorUIBinding{{Port: slack.UIMemberPickerPortMemberIDs, JSONPointer: "/clickerUserIds"}}},
			}},
		}),
	}
}

// ReceiveButtonDecision records the first Approve or Reject click on the posted button message and schedules
// the decision reply. Later clicks, a redelivery of the deciding click, and clicks on another message change
// nothing.
func (flow *ButtonApprovalFlow) ReceiveButtonDecision(
	ctx dex.Context,
	input ReceiveButtonDecisionInput,
) (*dex.RPCResult[DecisionResult], error) {
	state, err := approvalStateAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	decided, result, err := decideButtonClick(state, input)
	if err != nil {
		return nil, err
	}
	if !result.Accepted {
		return &dex.RPCResult[DecisionResult]{Output: result}, nil
	}
	if err := approvalStateAttribute.Set(ctx, decided); err != nil {
		return nil, err
	}
	return &dex.RPCResult[DecisionResult]{
		Output: result,
		NextSteps: []dex.StepMovement{
			dex.MovementOf(sdkgo.StepRef[ApprovalState](postDecisionStepType), decided),
		},
	}, nil
}

// decideButtonClick returns the state after input and whether input decided the approval. It returns
// errApprovalNotReady while the buttons are still being posted, so the Trigger retries the click.
func decideButtonClick(state ApprovalState, input ReceiveButtonDecisionInput) (ApprovalState, DecisionResult, error) {
	if state.DecisionEventID == input.EventID {
		return state, DecisionResult{Duplicate: true, Status: state.Status}, nil
	}
	if state.Status == StatusPostingButtons {
		return state, DecisionResult{}, errApprovalNotReady
	}
	// A button with the same action and value on another message, such as a copy, does not decide.
	if state.Status != StatusWaitingForDecision || input.MessageTimestamp != state.ButtonMessageTimestamp {
		return state, DecisionResult{Status: state.Status}, nil
	}
	state.Decision = DecisionApproved
	if input.ActionID == RejectActionID {
		state.Decision = DecisionRejected
	}
	state.Status = StatusPostingDecision
	state.DecisionEventID = input.EventID
	state.DeciderUserID = input.UserID
	return state, DecisionResult{Accepted: true, Status: state.Status}, nil
}

func (*ButtonApprovalFlow) GetApprovalStatus(ctx dex.Context, _ dex.None) (*dex.RPCResult[ApprovalState], error) {
	state, err := approvalStateAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[ApprovalState]{Output: state}, nil
}

// dex:field attribute-key:slack-button-approval-state value-type:json editable:false description:"Slack button approval status"
func (*ButtonApprovalFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	state, err := approvalStateAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"slack-button-approval-state": state}}, nil
}

// dex:field attribute-key:slack-button-approval-state value-type:json editable:false description:"Slack button approval details"
func (*ButtonApprovalFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	state, err := approvalStateAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"slack-button-approval-state": state}}, nil
}

// dex:group group-id:request group-label:"Approval request"
// dex:explanation text:"Persist the Slack thread identity before posting the approval buttons."
type initializeApproval struct {
	dex.StepDefaultsNoWaitFor[Input]
}

func (initializeApproval) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(approvalStateAttribute)}}
}

func (initializeApproval) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	state := ApprovalState{Input: input, Status: StatusPostingButtons}
	if err := approvalStateAttribute.Set(ctx, state); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[ApprovalState](postButtonsStepType), state), nil
}

type postButtonsOutput = slack.PostButtonMessageResult

// dex:group group-id:request group-label:"Approval request"
// dex:explanation text:"Record the posted button message, then wait for an Approve or Reject click."
type buttonsPosted struct {
	dex.StepDefaultsNoWaitFor[postButtonsOutput]
}

func (buttonsPosted) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(approvalStateAttribute)}}
}

func (buttonsPosted) Execute(ctx dex.Context, result postButtonsOutput) (*dex.StepDecision, error) {
	state, err := approvalStateAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	state.ButtonMessageTimestamp = result.Value.Message.Timestamp
	state.Status = StatusWaitingForDecision
	if err := approvalStateAttribute.Set(ctx, state); err != nil {
		return nil, err
	}
	return dex.DeadEnd(), nil
}

type postDecisionOutput = slack.PostThreadReplyResult

// dex:group group-id:decision group-label:"Decision"
// dex:explanation text:"Complete after Slack confirms the decision reply was posted."
type decisionPosted struct {
	dex.StepDefaultsNoWaitFor[postDecisionOutput]
}

func (decisionPosted) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(approvalStateAttribute)}}
}

func (decisionPosted) Execute(ctx dex.Context, _ postDecisionOutput) (*dex.StepDecision, error) {
	state, err := approvalStateAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	state.Status = StatusCompleted
	if err := approvalStateAttribute.Set(ctx, state); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(state), nil
}

// NewStartTriggerFilter creates the application's Slack root-message admission rule.
func NewStartTriggerFilter(configuration slack.ChannelThreadCreatedTriggerConfiguration) (sdkgo.TriggerFilter[slack.MessageEvent], error) {
	if err := configuration.Validate(); err != nil {
		return nil, err
	}
	matcher := configuration.ThreadTriggerMatcher
	return func(event sdkgo.TriggerEvent[slack.MessageEvent]) bool {
		message := event.Payload
		if event.ID == "" || message.TeamID == "" || message.ChannelID != configuration.ChannelID || message.UserID == "" ||
			message.ThreadTimestamp != message.Timestamp {
			return false
		}
		if matcher.MessageContains != "" && !strings.Contains(strings.ToLower(message.Text), strings.ToLower(matcher.MessageContains)) {
			return false
		}
		return len(matcher.PosterUserIDs) == 0 || containsString(matcher.PosterUserIDs, message.UserID)
	}, nil
}

// NewButtonTriggerFilter creates the application's admission rule for Approve and Reject clicks.
func NewButtonTriggerFilter(configuration slack.ButtonClickedTriggerConfiguration) (sdkgo.TriggerFilter[slack.ButtonClickEvent], error) {
	if err := configuration.Validate(); err != nil {
		return nil, err
	}
	return func(event sdkgo.TriggerEvent[slack.ButtonClickEvent]) bool {
		click := event.Payload
		if event.ID == "" || click.TeamID == "" || click.ChannelID != configuration.ChannelID || click.Value == "" || click.UserID == "" {
			return false
		}
		if click.ActionID != ApproveActionID && click.ActionID != RejectActionID {
			return false
		}
		return len(configuration.ClickerUserIDs) == 0 || containsString(configuration.ClickerUserIDs, click.UserID)
	}, nil
}

// ResolveFlowID names the approval after the thread its root message started.
func ResolveFlowID(event sdkgo.TriggerEvent[slack.MessageEvent]) string {
	message := event.Payload
	return approvalFlowID(message.TeamID, message.ChannelID, message.ThreadTimestamp)
}

// ResolveButtonFlowID finds the approval from the request's thread timestamp, which each posted button
// carries as its value.
func ResolveButtonFlowID(event sdkgo.TriggerEvent[slack.ButtonClickEvent]) string {
	click := event.Payload
	return approvalFlowID(click.TeamID, click.ChannelID, click.Value)
}

func approvalFlowID(teamID string, channelID string, threadTimestamp string) string {
	return fmt.Sprintf("slack-button-approval-%s-%s-%s", teamID, channelID, threadTimestamp)
}

func MapToFlowInput(event sdkgo.TriggerEvent[slack.MessageEvent]) Input {
	payload := event.Payload
	return Input{
		EventID: event.ID, TeamID: payload.TeamID, ChannelID: payload.ChannelID, ThreadTimestamp: payload.ThreadTimestamp,
		RequesterUserID: payload.UserID,
	}
}

func MapToReceiveButtonDecisionInput(event sdkgo.TriggerEvent[slack.ButtonClickEvent]) ReceiveButtonDecisionInput {
	return ReceiveButtonDecisionInput{
		EventID: event.ID, ActionID: event.Payload.ActionID, UserID: event.Payload.UserID, MessageTimestamp: event.Payload.MessageTimestamp,
	}
}

func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

var _ dex.Flow = (*ButtonApprovalFlow)(nil)
var _ dex.RPC[ReceiveButtonDecisionInput, DecisionResult] = (*ButtonApprovalFlow)(nil).ReceiveButtonDecision
var _ dex.RPC[dex.None, map[string]any] = (*ButtonApprovalFlow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*ButtonApprovalFlow)(nil).GetDexDisplay
