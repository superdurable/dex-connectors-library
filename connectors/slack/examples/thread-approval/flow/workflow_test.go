// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package threadapproval

import (
	"testing"
	"time"

	"github.com/superdurable/dex-connectors-library/connectors/slack"
	connector "github.com/superdurable/dex-connectors-library/sdk/go"
)

func TestRootAndReplyResolveTheSameFlowID(t *testing.T) {
	root := connector.TriggerEvent[slack.MessageEvent]{
		ID: "Ev-root", OccurredAt: time.Unix(1, 0),
		Payload: slack.MessageEvent{
			TeamID: "T1", ChannelID: "C1", Timestamp: "1.0", ThreadTimestamp: "1.0", UserID: "U1",
		},
	}
	reply := connector.TriggerEvent[slack.MessageEvent]{
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
