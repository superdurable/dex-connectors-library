// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package threadapproval

import (
	"testing"
	"time"

	"github.com/superdurable/dex-connectors-library/connectors/slack"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestRootAndReplyResolveTheSameFlowID(t *testing.T) {
	root := sdkgo.TriggerEvent[slack.MessageEvent]{
		ID: "Ev-root", OccurredAt: time.Unix(1, 0),
		Payload: slack.MessageEvent{
			TeamID: "T1", ChannelID: "C1", Timestamp: "1.0", ThreadTimestamp: "1.0", UserID: "U1",
		},
	}
	reply := sdkgo.TriggerEvent[slack.MessageEvent]{
		ID: "Ev-reply", OccurredAt: time.Unix(2, 0),
		Payload: slack.MessageEvent{
			TeamID: "T1", ChannelID: "C1", Timestamp: "2.0", ThreadTimestamp: "1.0", UserID: "U2",
		},
	}
	rootFlowID, err := ResolveFlowID(root.Payload.ThreadIdentity())
	if err != nil {
		t.Fatal(err)
	}
	replyFlowID, err := ResolveFlowID(reply.Payload.ThreadIdentity())
	if err != nil {
		t.Fatal(err)
	}
	if rootFlowID != replyFlowID {
		t.Fatalf("Flow IDs differ: root %q, reply %q", rootFlowID, replyFlowID)
	}
	input, err := BuildStartInput(root)
	if err != nil {
		t.Fatal(err)
	}
	if input.EventID != root.ID || input.ThreadTimestamp != "1.0" {
		t.Fatalf("start input = %+v", input)
	}
}

func TestFlowDeclaresIndependentTriggerBindings(t *testing.T) {
	bindings := (&Flow{}).GetConnectorTriggerBindings()
	if len(bindings) != 2 {
		t.Fatalf("binding count = %d", len(bindings))
	}
	if bindings[0].BindingName != StartTriggerBinding || bindings[0].Definition.Trigger.TriggerName != "channelThreadCreated" {
		t.Fatalf("start binding = %+v", bindings[0])
	}
	if bindings[1].BindingName != ReplyTriggerBinding || bindings[1].Definition.Trigger.TriggerName != "threadReplyCreated" {
		t.Fatalf("reply binding = %+v", bindings[1])
	}
}

func TestApplicationTriggerFiltersChannelPosterTextAndMessageShape(t *testing.T) {
	startFilter, err := NewStartTriggerEventFilter(slack.ChannelThreadCreatedTriggerConfiguration{
		ChannelID: "C1",
		ThreadTriggerMatcher: slack.MessageMatcher{
			MessageContains: "request approval", PosterUserIDs: []string{"U1"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	replyFilter, err := NewReplyTriggerEventFilter(slack.ThreadReplyCreatedTriggerConfiguration{
		ChannelID: "C1",
		ThreadReplyMatcher: slack.MessageMatcher{
			MessageContains: "approve", PosterUserIDs: []string{"U2"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	root := sdkgo.TriggerEvent[slack.MessageEvent]{Payload: slack.MessageEvent{
		ChannelID: "C1", Timestamp: "1.0", ThreadTimestamp: "1.0", UserID: "U1", Text: "Request Approval for this order",
	}}
	reply := sdkgo.TriggerEvent[slack.MessageEvent]{Payload: slack.MessageEvent{
		ChannelID: "C1", Timestamp: "2.0", ThreadTimestamp: "1.0", UserID: "U2", Text: "APPROVE",
	}}
	assertFilterResult(t, startFilter, root, true)
	assertFilterResult(t, startFilter, reply, false)
	assertFilterResult(t, replyFilter, root, false)
	assertFilterResult(t, replyFilter, reply, true)

	wrongChannel := reply
	wrongChannel.Payload.ChannelID = "C2"
	assertFilterResult(t, replyFilter, wrongChannel, false)
	wrongPoster := reply
	wrongPoster.Payload.UserID = "U3"
	assertFilterResult(t, replyFilter, wrongPoster, false)
	wrongText := reply
	wrongText.Payload.Text = "deny"
	assertFilterResult(t, replyFilter, wrongText, false)
}

func assertFilterResult(
	t *testing.T,
	filter sdkgo.TriggerEventFilter[slack.MessageEvent],
	event sdkgo.TriggerEvent[slack.MessageEvent],
	expected bool,
) {
	t.Helper()
	actual, err := filter(event)
	if err != nil {
		t.Fatal(err)
	}
	if actual != expected {
		t.Fatalf("filter result = %t, want %t for %+v", actual, expected, event.Payload)
	}
}
