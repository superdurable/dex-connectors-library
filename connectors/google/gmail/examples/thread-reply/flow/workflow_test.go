// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package threadreply

import (
	"testing"
	"time"

	gmail "github.com/superdurable/dex-connectors-library/connectors/google/gmail"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestRootAndReplyResolveTheSameFlowID(t *testing.T) {
	root := sdkgo.TriggerEvent[gmail.MessageEvent]{
		ID: "root-message", OccurredAt: time.Unix(1, 0),
		Payload: gmail.MessageEvent{PrimaryEmail: "owner@example.com", MessageID: "root-message", ThreadID: "thread-1"},
	}
	reply := sdkgo.TriggerEvent[gmail.MessageEvent]{
		ID: "reply-message", OccurredAt: time.Unix(2, 0),
		Payload: gmail.MessageEvent{PrimaryEmail: "owner@example.com", MessageID: "reply-message", ThreadID: "thread-1", IsReply: true},
	}
	rootFlowID := ResolveFlowID(root)
	replyFlowID := ResolveFlowID(reply)
	if rootFlowID != replyFlowID {
		t.Fatalf("Flow IDs differ: root %q, reply %q", rootFlowID, replyFlowID)
	}
	input := MapToFlowInput(root)
	if input.EventID != root.ID || input.MessageID != "root-message" {
		t.Fatalf("start input = %+v", input)
	}
}

func TestFlowDeclaresIndependentTriggerBindings(t *testing.T) {
	bindings := (&Flow{}).GetConnectorTriggerBindings()
	if len(bindings) != 2 {
		t.Fatalf("binding count = %d", len(bindings))
	}
	if bindings[0].BindingName != StartTriggerBinding || bindings[0].Definition.Trigger.TriggerName != "messageReceived" {
		t.Fatalf("start binding = %+v", bindings[0])
	}
	if bindings[1].BindingName != ReplyTriggerBinding || bindings[1].Definition.Trigger.TriggerName != "replyReceived" {
		t.Fatalf("reply binding = %+v", bindings[1])
	}
}

func TestApplicationTriggerFiltersSenderTextAndMessageShape(t *testing.T) {
	startFilter, err := NewStartTriggerFilter(gmail.MessageReceivedTriggerConfiguration{
		MessageMatcher: gmail.MessageMatcher{MessageContains: "approval request", SenderEmails: []string{"sender@example.com"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	replyFilter, err := NewReplyTriggerFilter(gmail.ReplyReceivedTriggerConfiguration{
		ReplyMatcher: gmail.MessageMatcher{MessageContains: "approved", SenderEmails: []string{"approver@example.com"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	root := sdkgo.TriggerEvent[gmail.MessageEvent]{ID: "root-message", Payload: gmail.MessageEvent{
		PrimaryEmail: "owner@example.com", MessageID: "root-message", ThreadID: "thread-1",
		From: "Sender <SENDER@example.com>", Subject: "Approval Request", IsReply: false,
	}}
	reply := sdkgo.TriggerEvent[gmail.MessageEvent]{ID: "reply-message", Payload: gmail.MessageEvent{
		PrimaryEmail: "owner@example.com", MessageID: "reply-message", ThreadID: "thread-1",
		From: "Approver <approver@example.com>", Subject: "Re: Approval", Snippet: "APPROVED", IsReply: true,
	}}
	assertFilterResult(t, startFilter, root, true)
	assertFilterResult(t, startFilter, reply, false)
	assertFilterResult(t, replyFilter, root, false)
	assertFilterResult(t, replyFilter, reply, true)

	wrongSender := reply
	wrongSender.Payload.From = "other@example.com"
	assertFilterResult(t, replyFilter, wrongSender, false)
	wrongText := reply
	wrongText.Payload.Snippet = "denied"
	assertFilterResult(t, replyFilter, wrongText, false)
}

func assertFilterResult(
	t *testing.T,
	filter sdkgo.TriggerFilter[gmail.MessageEvent],
	event sdkgo.TriggerEvent[gmail.MessageEvent],
	expected bool,
) {
	t.Helper()
	actual := filter(event)
	if actual != expected {
		t.Fatalf("filter result = %t, want %t for %+v", actual, expected, event.Payload)
	}
}
