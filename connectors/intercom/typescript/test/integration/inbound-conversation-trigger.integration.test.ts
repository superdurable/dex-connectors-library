// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from "node:crypto";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import type { Client } from "@superdurable/dex";
import type { ProviderRequest, ProviderResponse } from "@superdurable/connectors-sdk/providerhttp";
import { expect, it } from "vitest";
import { runInboundConversationApplication } from "../../examples/inbound-conversation/app.js";
import { ConnectionName, FlowIDPrefix, InboundTriggerBinding, type InboundConversationFlow, type InboundConversationOutcome } from "../../examples/inbound-conversation/flow.js";
import { TopicConversationUserCreated, TopicConversationUserReplied } from "../../src/index.js";
import { conversationJSON, newRecordingLogger, newTriggerProject, notificationBody, signNotification, testAccessToken } from "../trigger_support.js";

const dexAddress = () => {
  const address = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!address) throw new Error("set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server");
  return address;
};

/** Stand-in for the Intercom REST API that requires the bearer token and API version and records each read. */
function newIntercomAPI() {
  const reads: string[] = [];
  const send = async (request: ProviderRequest): Promise<ProviderResponse> => {
    const url = new URL(request.url);
    const headers = Object.fromEntries(Object.entries(request.headers ?? {}).map(([name, value]) => [name.toLowerCase(), String(value)]));
    if (headers.authorization !== `Bearer ${testAccessToken}` || headers["intercom-version"] !== "2.16") return { status: 401, headers: {}, body: Buffer.from('{"type":"error.list"}') };
    const match = /^\/conversations\/([0-9]+)$/.exec(url.pathname);
    if (request.method !== "GET" || !match) return { status: 404, headers: {}, body: Buffer.from('{"type":"error.list"}') };
    reads.push(match[1]!);
    const item = JSON.parse(conversationJSON(match[1]!)) as Record<string, unknown>;
    item.conversation_parts = { type: "conversation_part.list", total_count: 0, conversation_parts: [] };
    return { status: 200, headers: { "content-type": "application/json" }, body: Buffer.from(JSON.stringify(item)) };
  };
  return { send, reads };
}

async function post(url: string, body: Buffer, signature: string): Promise<number> {
  const response = await fetch(url, { method: "POST", headers: { "Content-Type": "application/json", "X-Hub-Signature": signature }, body: new Uint8Array(body) });
  await response.arrayBuffer();
  return response.status;
}

async function waitForFlowToExist(client: Client, flowId: string): Promise<void> {
  const deadline = Date.now() + 30_000;
  for (;;) {
    try { await client.describeFlow(flowId); return; }
    catch (error) { if (Date.now() > deadline) throw error; }
    await new Promise(resolve => setTimeout(resolve, 100));
  }
}

it("starts one Flow per signed new conversation through the durable inbox and ignores forgeries and other topics", async () => {
  const conversationId = String(100_000_000 + Math.floor(Math.random() * 899_999_999));
  const project = await newTriggerProject(ConnectionName, { [InboundTriggerBinding]: { topics: [TopicConversationUserCreated, TopicConversationUserReplied] } });
  const intercom = newIntercomAPI();
  const logger = newRecordingLogger();
  const directory = await mkdtemp(join(tmpdir(), "dex-intercom-inbound-"));
  const stop = new AbortController();
  let resolveReady!: (ready: { client: Client; flow: InboundConversationFlow; webhookURL: string }) => void;
  const ready = new Promise<{ client: Client; flow: InboundConversationFlow; webhookURL: string }>(resolve => { resolveReady = resolve; });
  const running = runInboundConversationApplication(project, {
    serverAddress: dexAddress(), webhookHost: "127.0.0.1", webhookPort: 0, blobCacheDirectory: directory, logger, intercomOptions: { send: intercom.send }, onReady: resolveReady,
  }, stop.signal);
  try {
    const { client, webhookURL } = await ready;
    const notificationId = `notif_${randomUUID()}`;
    const created = notificationBody(notificationId, TopicConversationUserCreated, conversationJSON(conversationId));
    expect(await post(webhookURL, created, signNotification(created, "forged-secret")), "a forged signature is refused").toBe(400);
    await expect.poll(() => post(webhookURL, created, signNotification(created)), { timeout: 30_000 }).toBe(200);
    expect(await post(webhookURL, created, signNotification(created)), "a redelivery is acknowledged").toBe(200);
    const replied = notificationBody(`notif_${randomUUID()}`, TopicConversationUserReplied, conversationJSON(conversationId));
    expect(await post(webhookURL, replied, signNotification(replied)), "the binding records the reply, which the Flow filter skips").toBe(200);
    const ping = notificationBody(`notif_${randomUUID()}`, "ping", '{"type":"ping","message":"hello"}');
    expect(await post(webhookURL, ping, signNotification(ping))).toBe(200);

    const flowId = `${FlowIDPrefix}${conversationId}`;
    await waitForFlowToExist(client, flowId);
    const completed = await client.waitForFlow(flowId, 60_000);
    expect(completed.status, completed.errorMessage).toBe("completed");
    expect(completed.singleOutput<InboundConversationOutcome>()).toEqual({
      conversationId, notificationId, conversationState: "open", customerEmail: "jane@acme.example.com", partCount: 0,
    });
    await expect.poll(() => logger.find("trigger event delivered", { event_id: notificationId, duplicate: true }).length, { timeout: 30_000 }).toBe(1);
    await expect.poll(() => logger.find("trigger event skipped: filtered").length, { timeout: 30_000 }).toBe(1);
    await expect.poll(async () => (await project.triggerInbox({ connectorId: "intercom", connectionName: ConnectionName, triggerName: "conversationEvent", bindingName: InboundTriggerBinding }).pending()).length).toBe(0);
    expect(intercom.reads).toEqual([conversationId]);
    expect(JSON.stringify(logger.records)).not.toMatch(/SENTINEL/);
  } finally {
    stop.abort();
    await running;
    await rm(directory, { recursive: true, force: true });
  }
}, 120_000);
