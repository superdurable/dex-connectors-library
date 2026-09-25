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
