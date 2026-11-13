// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package buttonapproval

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/slack/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestRootMessageAndButtonClickResolveTheSameFlowID(t *testing.T) {
	root := sdkgo.TriggerEvent[slack.MessageEvent]{ID: "Ev-root", Payload: slack.MessageEvent{
		TeamID: "T1", ChannelID: "C1", Timestamp: "1.0", ThreadTimestamp: "1.0", UserID: "U1", Text: "request approval",
	}}
	click := sdkgo.TriggerEvent[slack.ButtonClickEvent]{ID: "T1:C1:2.0:approve:3.0", Payload: slack.ButtonClickEvent{
		TeamID: "T1", ChannelID: "C1", MessageTimestamp: "2.0", ThreadTimestamp: "1.0", UserID: "U2", ActionID: ApproveActionID,
		Value: "1.0", ActionTimestamp: "3.0",
	}}
	require.Equal(t, "slack-button-approval-T1-C1-1.0", ResolveFlowID(root))
	require.Equal(t, ResolveFlowID(root), ResolveButtonFlowID(click))
	require.Equal(t, Input{EventID: "Ev-root", TeamID: "T1", ChannelID: "C1", ThreadTimestamp: "1.0", RequesterUserID: "U1"}, MapToFlowInput(root))
	require.Equal(t, ReceiveButtonDecisionInput{EventID: click.ID, ActionID: ApproveActionID, UserID: "U2", MessageTimestamp: "2.0"},
		MapToReceiveButtonDecisionInput(click))
}

func TestButtonDecisionAcceptsOnlyTheFirstClickOnThePostedMessage(t *testing.T) {
	waiting := ApprovalState{Status: StatusWaitingForDecision, ButtonMessageTimestamp: "2.0"}
	click := ReceiveButtonDecisionInput{EventID: "click-1", ActionID: RejectActionID, UserID: "U2", MessageTimestamp: "2.0"}

	// Matching action and value on another message, such as a copied button, never decides.
	foreign := click
	foreign.EventID, foreign.MessageTimestamp = "click-foreign", "9.0"
	state, result, err := decideButtonClick(waiting, foreign)
	require.NoError(t, err)
	require.Equal(t, DecisionResult{Status: StatusWaitingForDecision}, result)
	require.Equal(t, waiting, state)

	decided, result, err := decideButtonClick(waiting, click)
	require.NoError(t, err)
	require.Equal(t, DecisionResult{Accepted: true, Status: StatusPostingDecision}, result)
	require.Equal(t, ApprovalState{
		Status: StatusPostingDecision, ButtonMessageTimestamp: "2.0", Decision: DecisionRejected, DecisionEventID: "click-1", DeciderUserID: "U2",
	}, decided)

	_, result, err = decideButtonClick(decided, click)
	require.NoError(t, err)
	require.Equal(t, DecisionResult{Duplicate: true, Status: StatusPostingDecision}, result)
	later := click
	later.EventID, later.ActionID = "click-2", ApproveActionID
	state, result, err = decideButtonClick(decided, later)
	require.NoError(t, err)
	require.False(t, result.Accepted)
	require.Equal(t, DecisionRejected, state.Decision, "the first decision stands")

	_, _, err = decideButtonClick(ApprovalState{Status: StatusPostingButtons}, click)
	require.ErrorIs(t, err, errApprovalNotReady, "a click before the buttons are recorded retries")
}

func TestFlowDeclaresRequestAndButtonTriggerBindings(t *testing.T) {
	bindings := (&ButtonApprovalFlow{}).GetConnectorTriggerBindings()
	require.Len(t, bindings, 2)
	require.Equal(t, StartTriggerBinding, bindings[0].BindingName)
	require.Equal(t, "channelThreadCreated", bindings[0].Definition.Trigger.TriggerName)
	require.Equal(t, ButtonTriggerBinding, bindings[1].BindingName)
	require.Equal(t, "buttonClicked", bindings[1].Definition.Trigger.TriggerName)
	// The action IDs are code constants, so the button binding shows only the channel and the deciders.
	units := bindings[1].ConfigurationUI.Units
	require.Len(t, units, 2)
	for _, unit := range units {
		require.NotEmpty(t, unit.Description)
	}
	require.Equal(t, "/channelId", units[0].Bindings[0].JSONPointer)
	require.True(t, units[0].Required)
	require.Equal(t, "/clickerUserIds", units[1].Bindings[0].JSONPointer)
	require.False(t, units[1].Required, "an empty selection lets any member decide")
}

func TestButtonTriggerFilterAcceptsOnlyApproveAndRejectFromDeciders(t *testing.T) {
	filter, err := NewButtonTriggerFilter(slack.ButtonClickedTriggerConfiguration{ChannelID: "C1", ClickerUserIDs: []string{"U2"}})
	require.NoError(t, err)
	click := sdkgo.TriggerEvent[slack.ButtonClickEvent]{ID: "click", Payload: slack.ButtonClickEvent{
		TeamID: "T1", ChannelID: "C1", MessageTimestamp: "2.0", ThreadTimestamp: "1.0", UserID: "U2", ActionID: RejectActionID,
		Value: "1.0",
	}}
	require.True(t, filter(click))
	for name, change := range map[string]func(*slack.ButtonClickEvent){
		"other channel": func(event *slack.ButtonClickEvent) { event.ChannelID = "C2" },
		"other button":  func(event *slack.ButtonClickEvent) { event.ActionID = "details" },
		"other member":  func(event *slack.ButtonClickEvent) { event.UserID = "U3" },
		"missing team":  func(event *slack.ButtonClickEvent) { event.TeamID = "" },
		"missing value": func(event *slack.ButtonClickEvent) { event.Value = "" },
	} {
		changed := click
		change(&changed.Payload)
		require.False(t, filter(changed), name)
	}
	_, err = NewButtonTriggerFilter(slack.ButtonClickedTriggerConfiguration{})
	require.Error(t, err)
}

func TestStartTriggerFilterAcceptsOnlyMatchingRootMessages(t *testing.T) {
	filter, err := NewStartTriggerFilter(slack.ChannelThreadCreatedTriggerConfiguration{
		ChannelID: "C1", ThreadTriggerMatcher: slack.MessageMatcher{MessageContains: "request approval", PosterUserIDs: []string{"U1"}},
	})
	require.NoError(t, err)
	root := sdkgo.TriggerEvent[slack.MessageEvent]{ID: "Ev-root", Payload: slack.MessageEvent{
		TeamID: "T1", ChannelID: "C1", Timestamp: "1.0", ThreadTimestamp: "1.0", UserID: "U1", Text: "Please REQUEST APPROVAL",
	}}
	require.True(t, filter(root))
	reply := root
	reply.Payload.Timestamp = "1.5"
	require.False(t, filter(reply), "a reply does not start an approval")
	otherPoster := root
	otherPoster.Payload.UserID = "U9"
	require.False(t, filter(otherPoster))
}
