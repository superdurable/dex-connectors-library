// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from "vitest";
import type { TriggerEvent } from "@superdurable/connectors-sdk";
import { GeneratedMock } from "../src/mock.js";
import { TopicConversationUserCreated, TopicConversationUserReplied, type ConversationDetails, type ConversationEvent } from "../src/index.js";
import {
  ConnectionName, FlowIDPrefix, InboundConversationFlow, InboundTriggerBinding, acceptInboundConversation, chooseInboundConversationOutcome, mapToFlowInput,
  mapToGetConversationInput, resolveFlowId,
} from "../examples/inbound-conversation/flow.js";

function event(topic: string, conversationId = "215472658213"): TriggerEvent<ConversationEvent> {
  return {
    id: "notif_1", occurredAt: new Date("2026-01-01T01:01:40Z"),
    payload: {
      notificationId: "notif_1", topic, workspaceId: "ecahpwf5", notifiedAt: "2026-01-01T01:01:40Z",
      conversation: { id: conversationId, state: "open", isOpen: true, source: { author: { type: "user", email: "jane@acme.example.com" } }, createdAt: "2026-01-01T01:00:00Z", updatedAt: "2026-01-01T01:01:40Z" },
    },
  };
}

it("declares stable identities and the topic picker binding", () => {
  const flow = new InboundConversationFlow(new GeneratedMock(ConnectionName).connection());
  expect(flow.getFlowType()).toBe("IntercomInboundConversation");
  const [binding, ...others] = flow.getConnectorTriggerBindings();
  expect(others).toEqual([]);
  expect(binding).toMatchObject({ connectionName: ConnectionName, bindingName: InboundTriggerBinding, definition: { trigger: { connectorId: "intercom", triggerName: "conversationEvent" } } });
  const units = binding!.configurationUI!.units;
  expect(units.map(unit => [unit.id, unit.unitId, unit.required, unit.bindings])).toEqual([["topics", "conversationTopicPicker", false, [{ port: "topics", jsonPointer: "/topics" }]]]);
  expect(units[0]!.description).toMatch(/Configure > Webhooks/);
  expect(units[0]!.description).toMatch(/selecting none/);
  expect(() => new InboundConversationFlow(new GeneratedMock("intercom").connection())).toThrow(/static Intercom connection/);
});

it("admits only new conversations and derives one Flow per conversation", () => {
  expect(acceptInboundConversation(event(TopicConversationUserCreated))).toBe(true);
  expect(acceptInboundConversation(event(TopicConversationUserReplied))).toBe(false);
  expect(acceptInboundConversation(event(TopicConversationUserCreated, ""))).toBe(false);
  expect(resolveFlowId(event(TopicConversationUserCreated))).toBe(`${FlowIDPrefix}215472658213`);
  expect(mapToFlowInput(event(TopicConversationUserCreated))).toEqual({
    conversationId: "215472658213", notificationId: "notif_1", topic: TopicConversationUserCreated, workspaceId: "ecahpwf5", notifiedAt: "2026-01-01T01:01:40Z",
  });
  expect(mapToGetConversationInput({ conversationId: "215472658213" })).toEqual({ conversationId: "215472658213", latestPartLimit: 5 });
});

it("completes with the conversation state and the starting customer's email", () => {
  const details: ConversationDetails = { conversation: event(TopicConversationUserCreated).payload.conversation, latestParts: [], partCount: 2 };
  expect(chooseInboundConversationOutcome({ conversationId: "215472658213", notificationId: "notif_1" }, details)).toEqual({
    conversationId: "215472658213", notificationId: "notif_1", conversationState: "open", customerEmail: "jane@acme.example.com", partCount: 2,
  });
  const anonymous = { ...details, conversation: { ...details.conversation, source: { author: {} } } };
  expect(chooseInboundConversationOutcome({ conversationId: "215472658213" }, anonymous)).toMatchObject({ notificationId: "", customerEmail: "" });
});
