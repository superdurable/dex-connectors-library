// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFile } from "node:fs/promises";
import { afterEach, expect, it } from "vitest";
import { SecretString, TriggerDeliveryAbortedError, newTriggerContext, type TriggerEvent, type TriggerRunner, type TriggerTarget } from "@superdurable/connectors-sdk";
import { ObjectNotFoundError } from "@superdurable/connectors-sdk/projectconfig";
import {
  ConversationEventTriggerConfigurationSchema, IntercomClient, TopicConversationAdminClosed, TopicConversationRatingAdded, TopicConversationUserCreated,
  TopicConversationUserReplied, conversationEventTopics, newConnection, newConversationEventTrigger, newConversationEventWebhookHandler,
  newProjectConversationEventEndpointRunner, type ConversationEvent, type ConversationEventTriggerConfiguration, type Credentials,
} from "../src/index.js";
import {
  LoopbackServer, conversationJSON, newRecordingLogger, newTriggerProject, notificationBody, signNotification, testAccessToken, testClientSecret, testConversationId,
} from "./trigger_support.js";

const connectionReference = { provider: "intercom", name: "intercom-support-inbox" };
const cleanups: Array<() => Promise<void>> = [];
afterEach(async () => { for (const cleanup of cleanups.splice(0).reverse()) await cleanup(); });

function staticCredentials(credentials: Credentials) {
  return { resolve: async () => credentials };
}
const signingCredentials = staticCredentials({ accessToken: new SecretString(testAccessToken), clientSecret: new SecretString(testClientSecret) });

/** A target that records every delivered event and resolves waiters. */
function newReceivingTarget(): TriggerTarget<ConversationEvent> & { events: Array<TriggerEvent<ConversationEvent>>; next(): Promise<TriggerEvent<ConversationEvent>> } {
  const events: Array<TriggerEvent<ConversationEvent>> = [];
  let read = 0;
  return {
    events,
    async handleTrigger(_context, event) { events.push(event); },
    async next() {
      await expect.poll(() => events.length, { timeout: 5_000 }).toBeGreaterThan(read);
      return events[read++]!;
    },
  };
}

/** Run a runner until the test ends. */
function runUntilCleanup(runner: { run(context: ReturnType<typeof newTriggerContext>): Promise<void> }): void {
  const stop = new AbortController();
  const running = runner.run(newTriggerContext(stop.signal)).catch((error: unknown) => { if (!(error instanceof TriggerDeliveryAbortedError)) throw error; });
  cleanups.push(async () => { stop.abort(); await running; });
}

async function startStandaloneBinding(configuration: ConversationEventTriggerConfiguration, options: { webhookMaxBodyBytes?: number } = {}) {
  const client = new IntercomClient({ ...options }, signingCredentials, { logger: newRecordingLogger() });
  const connection = newConnection(client, connectionReference);
  const target = newReceivingTarget();
  const trigger: TriggerRunner = newConversationEventTrigger({ connection, connectionName: connectionReference.name, bindingName: "test-binding", configuration, target });
  const server = await LoopbackServer.start(newConversationEventWebhookHandler(connection));
  cleanups.push(() => server.close());
  return { server, target, trigger };
}

it("verifies, decodes, and filters conversation notifications at parity with Go", async () => {
  const { server, target, trigger } = await startStandaloneBinding({ topics: [TopicConversationUserCreated, TopicConversationUserReplied] });
  const probe = notificationBody("notif_probe", TopicConversationUserCreated, conversationJSON());
  expect(await server.send("POST", probe, signNotification(probe))).toBe(503);
  runUntilCleanup(trigger);
  await expect.poll(async () => server.send("POST", probe, signNotification(probe)), { timeout: 5_000 }).toBe(200);
  expect((await target.next()).id).toBe("notif_probe");

  expect(await server.send("HEAD")).toBe(200);
  expect(await server.send("GET")).toBe(405);
  const created = notificationBody("notif_ccd8a4d0-f965-11e3-a367-c779cae3e1b3", TopicConversationUserCreated, conversationJSON());
  expect(await server.send("POST", created, signNotification(created).toUpperCase().replace("SHA1=", "sha1="))).toBe(200);
  const event = await target.next();
  expect(event.id).toBe("notif_ccd8a4d0-f965-11e3-a367-c779cae3e1b3");
  expect(event.occurredAt).toEqual(new Date(1767229300 * 1000));
  expect(event.payload).toMatchObject({ notificationId: event.id, topic: TopicConversationUserCreated, workspaceId: "ecahpwf5", notifiedAt: "2026-01-01T01:01:40Z" });
  expect(event.payload.conversation).toMatchObject({ id: testConversationId, state: "open", isOpen: true, contacts: [{ id: "5ba6", externalId: "jane-1" }] });
  expect(event.payload.conversation.source.author.email).toBe("jane@acme.example.com");
  expect(event.payload.conversation.source.body, "the event carries no message text").toBeUndefined();

  const malformed = Buffer.from('{"type":');
  const notNotification = Buffer.from('{"type":"conversation","id":"1"}');
  const numericID = Buffer.from(created.toString().replace('"id":"notif_ccd8a4d0-f965-11e3-a367-c779cae3e1b3"', '"id":7'));
  const ping = notificationBody("notif_ping", "ping", '{"type":"ping","message":"something"}');
  const contact = notificationBody("notif_contact", "contact.user.created", '{"type":"contact","id":"5ba6"}');
  const skipped = notificationBody("notif_closed", TopicConversationAdminClosed, conversationJSON(testConversationId, "closed"));
  const badItem = notificationBody("notif_bad_item", TopicConversationUserCreated, '{"type":"conversation","id":"abc"}');
  const pingWithInvalidData = Buffer.from('{"type":"notification_event","app_id":"ecahpwf5","id":"notif_ping_data","topic":"ping","data":"not-an-object","created_at":1767229300}');
  const cases: Array<[string, Buffer, string | undefined, number]> = [
    ["wrong secret", created, signNotification(created, "another-secret"), 400],
    ["missing signature", created, undefined, 400],
    ["sha256 signature", created, `sha256=${"a".repeat(64)}`, 400],
    ["tampered body", Buffer.from(created.toString().replace("jane@", "eve@")), signNotification(created), 400],
    ["malformed JSON", malformed, signNotification(malformed), 400],
    ["not a notification", notNotification, signNotification(notNotification), 400],
    ["numeric notification ID", numericID, signNotification(numericID), 400],
    ["non-numeric conversation ID", badItem, signNotification(badItem), 400],
    ["ping with non-object data, which Go fails to decode", pingWithInvalidData, signNotification(pingWithInvalidData), 400],
    ["ping", ping, signNotification(ping), 200],
    ["contact topic", contact, signNotification(contact), 200],
    ["topic the binding skips", skipped, signNotification(skipped), 200],
  ];
  for (const [name, body, signature, status] of cases) expect(await server.send("POST", body, signature), name).toBe(status);
  // Sources receive in arrival order, so this last accepted event proves none of the cases above was delivered.
  const last = notificationBody("notif_last", TopicConversationUserReplied, conversationJSON());
  expect(await server.send("POST", last, signNotification(last))).toBe(200);
  expect((await target.next()).id).toBe("notif_last");
  expect(target.events.map(delivered => delivered.id)).toEqual(["notif_probe", "notif_ccd8a4d0-f965-11e3-a367-c779cae3e1b3", "notif_last"]);
});

it("verifies only the first of repeated X-Hub-Signature headers, as Go's Header.Get does", async () => {
  const { server, target, trigger } = await startStandaloneBinding({});
  runUntilCleanup(trigger);
  const body = notificationBody("notif_repeated_header", TopicConversationUserCreated, conversationJSON());
  const send = async (first: string, second: string) => {
    const headers = new Headers({ "Content-Type": "application/json" });
    headers.append("X-Hub-Signature", first);
    headers.append("X-Hub-Signature", second);
    const response = await fetch(server.url, { method: "POST", headers, body: new Uint8Array(body) });
    await response.arrayBuffer();
    return response.status;
  };
  expect(await send(signNotification(body, "another-secret"), signNotification(body)), "a valid second value does not rescue a bad first one").toBe(400);
  await expect.poll(() => send(signNotification(body), signNotification(body, "another-secret")), { timeout: 5_000 }).toBe(200);
  expect((await target.next()).id).toBe("notif_repeated_header");
});

it("treats a zero webhookMaxBodyBytes as the manifest default and rejects a negative one", () => {
  expect(new IntercomClient({ webhookMaxBodyBytes: 0 }, signingCredentials).webhookMaxBodyBytes).toBe(2_097_152);
  expect(new IntercomClient({}, signingCredentials).webhookMaxBodyBytes).toBe(2_097_152);
  expect(() => new IntercomClient({ webhookMaxBodyBytes: -1 }, signingCredentials)).toThrow(/must be positive/);
});

it("uses the receipt time when Intercom sends no created_at", async () => {
  const { server, target, trigger } = await startStandaloneBinding({});
  runUntilCleanup(trigger);
  const body = notificationBody("notif_untimed", TopicConversationRatingAdded, conversationJSON(), 0);
  const before = Date.now();
  await expect.poll(async () => server.send("POST", body, signNotification(body)), { timeout: 5_000 }).toBe(200);
  const event = await target.next();
  expect(event.occurredAt.getTime()).toBeGreaterThanOrEqual(before - 1);
  expect(event.payload.notifiedAt).toBe(event.occurredAt.toISOString().replace(/\.000Z$/, "Z").replace(/(\.\d*?)0+Z$/, "$1Z"));
});

it("asks Intercom to retry while it cannot verify or no binding runs", async () => {
  const created = notificationBody("notif_retry", TopicConversationUserCreated, conversationJSON());
  const project = await newTriggerProject(connectionReference.name, { "test-binding": {} }, { access_token: testAccessToken });
  const runner = newProjectConversationEventEndpointRunner(project, connectionReference.name, [{
    bindingName: "test-binding", target: { handleTrigger: async () => { throw new Error("no notification may be delivered without a client secret"); } },
  }], { logger: newRecordingLogger() });
  const server = await LoopbackServer.start((request, response) => runner.serveHTTP(request, response));
  cleanups.push(() => server.close());
  runUntilCleanup(runner);
  await expect.poll(() => runner.runningSourceCount(), { timeout: 5_000 }).toBe(1);
  expect(await server.send("HEAD")).toBe(200);
  expect(await server.send("POST", created, signNotification(created)), "a blank client_secret answers 503").toBe(503);
  expect(await project.triggerInbox({ connectorId: "intercom", connectionName: connectionReference.name, triggerName: "conversationEvent", bindingName: "test-binding" }).pending()).toEqual([]);

  const idle = await LoopbackServer.start(newConversationEventWebhookHandler(newConnection(new IntercomClient({}, signingCredentials), connectionReference)));
  cleanups.push(() => idle.close());
  expect(await idle.send("HEAD"), "HEAD succeeds before any binding runs").toBe(200);
  expect(await idle.send("POST", created, signNotification(created)), "no running binding answers 503").toBe(503);
});

it("rejects an oversized body before checking its signature", async () => {
  const { server } = await startStandaloneBinding({}, { webhookMaxBodyBytes: 64 });
  const body = notificationBody("notif_large", TopicConversationUserCreated, conversationJSON());
  expect(await server.send("POST", body, signNotification(body))).toBe(413);
});

it("accepts only supported, distinct conversation topics", () => {
  expect(ConversationEventTriggerConfigurationSchema.safeParse({}).success).toBe(true);
  expect(ConversationEventTriggerConfigurationSchema.safeParse({ topics: null }).success).toBe(true);
  expect(ConversationEventTriggerConfigurationSchema.safeParse({ topics: conversationEventTopics() }).success).toBe(true);
  for (const [name, topics] of Object.entries({
    ping: ["ping"], "ticket topic": ["ticket.created"], "deleted topic": ["conversation.deleted"],
    duplicate: [TopicConversationUserCreated, TopicConversationUserCreated], "different case": ["Conversation.User.Created"],
  })) expect(ConversationEventTriggerConfigurationSchema.safeParse({ topics }).success, name).toBe(false);
  expect(ConversationEventTriggerConfigurationSchema.safeParse({ topics: [], extra: true }).success).toBe(false);
  const topics = conversationEventTopics();
  topics[0] = "mutated";
  expect(conversationEventTopics()[0], "callers get a copy").toBe(TopicConversationUserCreated);
});

it("lists the same topics as the Studio topic picker", async () => {
  const contents = await readFile(new URL("../../ui/src/topics.ts", import.meta.url), "utf8");
  expect([...contents.matchAll(/\{topic: "([^"]+)"/g)].map(match => match[1])).toEqual(conversationEventTopics());
});

const inboxKey = { connectorId: "intercom", connectionName: connectionReference.name, triggerName: "conversationEvent", bindingName: "test-binding" };

it("records each notification in the binding's durable inbox before acknowledging it", async () => {
  const project = await newTriggerProject(connectionReference.name, { "test-binding": { topics: [TopicConversationUserCreated] } });
  let releaseDelivery!: () => void;
  const deliveryReleased = new Promise<void>(resolve => { releaseDelivery = resolve; });
  const target = newReceivingTarget();
  const logger = newRecordingLogger();
  const runner = newProjectConversationEventEndpointRunner(project, connectionReference.name, [{
    bindingName: "test-binding", target: { async handleTrigger(context, event) { await deliveryReleased; await target.handleTrigger(context, event); } },
  }], { logger });
  const server = await LoopbackServer.start((request, response) => runner.serveHTTP(request, response));
  cleanups.push(() => server.close());
  expect(runner.runningSourceCount()).toBe(0);
  runUntilCleanup(runner);
  await expect.poll(() => runner.runningSourceCount(), { timeout: 5_000 }).toBe(1);

  const created = notificationBody("notif_recorded", TopicConversationUserCreated, conversationJSON());
  expect(await server.send("POST", created, signNotification(created))).toBe(200);
  const pending = await project.triggerInbox(inboxKey).pending();
  expect(pending.map(entry => entry.id), "the 200 came after the notification was recorded").toEqual(["notif_recorded"]);
  const stored = Buffer.from(pending[0]!.event).toString("utf8");
  expect(stored).not.toContain(testClientSecret);
  expect(stored).not.toContain(testAccessToken);
  releaseDelivery();
  expect((await target.next()).id).toBe("notif_recorded");
  await expect.poll(async () => (await project.triggerInbox(inboxKey).pending()).length).toBe(0);
  expect(JSON.stringify(logger.records)).not.toMatch(/SENTINEL/);
});

it("replays a recorded notification after a restart before receiving new ones", async () => {
  const project = await newTriggerProject(connectionReference.name, { "test-binding": {} });
  const failing = newProjectConversationEventEndpointRunner(project, connectionReference.name, [{
    bindingName: "test-binding", target: { handleTrigger: async () => { throw new Error("Dex is unreachable"); } },
  }], { logger: newRecordingLogger() });
  const firstServer = await LoopbackServer.start((request, response) => failing.serveHTTP(request, response));
  const stopFirst = new AbortController();
  const firstRun = failing.run(newTriggerContext(stopFirst.signal)).catch(() => undefined);
  await expect.poll(() => failing.runningSourceCount(), { timeout: 5_000 }).toBe(1);
  const created = notificationBody("notif_replayed", TopicConversationUserCreated, conversationJSON());
  expect(await firstServer.send("POST", created, signNotification(created))).toBe(200);
  stopFirst.abort();
  await firstRun;
  await firstServer.close();
  expect((await project.triggerInbox(inboxKey).pending()).map(entry => entry.id)).toEqual(["notif_replayed"]);

  const target = newReceivingTarget();
  const logger = newRecordingLogger();
  const restarted = newProjectConversationEventEndpointRunner(project, connectionReference.name, [{ bindingName: "test-binding", target }], { logger });
  runUntilCleanup(restarted);
  const replayed = await target.next();
  expect(replayed.id).toBe("notif_replayed");
  expect(replayed.payload.conversation.id).toBe(testConversationId);
  expect(replayed.occurredAt).toEqual(new Date(1767229300 * 1000));
  expect(logger.find("replaying pending trigger events")).toHaveLength(1);
});

it("rejects missing, repeated, unstored, or invalid bindings", async () => {
  const target: TriggerTarget<ConversationEvent> = { handleTrigger: async () => undefined };
  const project = await newTriggerProject(connectionReference.name, { "test-binding": { topics: [TopicConversationUserCreated] }, "invalid-binding": { topics: ["ticket.created"] } });
  const build = (routes: Parameters<typeof newProjectConversationEventEndpointRunner>[2]) => () => newProjectConversationEventEndpointRunner(project, connectionReference.name, routes);
  expect(build([]), "no routes").toThrow(/routes are required/);
  expect(build([{ bindingName: " ", target }]), "blank binding").toThrow(/binding name and target/);
  expect(build([{ bindingName: "test-binding" } as never]), "missing target").toThrow(/binding name and target/);
  expect(build([{ bindingName: "test-binding", target }, { bindingName: "test-binding", target }]), "duplicate").toThrow(/duplicated/);
  expect(build([{ bindingName: "another-binding", target }]), "unstored").toThrow(ObjectNotFoundError);
  expect(build([{ bindingName: "invalid-binding", target }]), "invalid topics").toThrow(/not a supported conversation topic/);
  expect(() => newProjectConversationEventEndpointRunner(undefined as never, connectionReference.name, [{ bindingName: "test-binding", target }])).toThrow();
  expect(() => newProjectConversationEventEndpointRunner(project, "unknown-connection", [{ bindingName: "test-binding", target }])).toThrow();
  const connection = newConnection(new IntercomClient({}, signingCredentials), connectionReference);
  expect(() => newConversationEventTrigger({ connection, connectionName: "other", bindingName: "b", configuration: {}, target })).toThrow(/must equal/);
  expect(() => newConversationEventTrigger({ connection, connectionName: connectionReference.name, bindingName: "b", configuration: { topics: ["ping"] }, target })).toThrow();
  expect(build([{ bindingName: "test-binding", target }])().runningSourceCount()).toBe(0);
});
