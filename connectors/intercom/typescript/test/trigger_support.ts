// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHmac } from "node:crypto";
import { once } from "node:events";
import { createServer, type IncomingMessage, type Server, type ServerResponse } from "node:http";
import type { TriggerLogLevel, TriggerLogger } from "@superdurable/connectors-sdk";
import { ConnectionStore, CredentialMaterial, InMemoryObjectStore, LoadedProject } from "@superdurable/connectors-sdk/projectconfig";

/** Client secret that signs test notifications; it must never reach a record. */
export const testClientSecret = "SENTINEL-CLIENT-SECRET";
/** Access token that must never reach a record. */
export const testAccessToken = "SENTINEL-ACCESS-TOKEN";
/** Conversation ID every test notification carries. */
export const testConversationId = "215472658213";

/** One captured record. */
export interface RecordedLog { level: TriggerLogLevel; message: string; attributes: Record<string, unknown> }

/** Logger that keeps every record. */
export function newRecordingLogger(): TriggerLogger & { records: RecordedLog[]; find(message: string, attributes?: Record<string, unknown>): RecordedLog[] } {
  const records: RecordedLog[] = [];
  return {
    records, isEnabled: () => true,
    log: (level, message, attributes) => { records.push({ level, message, attributes: { ...attributes } }); },
    find: (message, attributes = {}) => records.filter(record => record.message === message && Object.entries(attributes).every(([key, value]) => record.attributes[key] === value)),
  };
}

/** A conversation item as Intercom 2.16 sends it in a notification. */
export function conversationJSON(conversationId = testConversationId, state = "open", email = "jane@acme.example.com"): string {
  return JSON.stringify({
    type: "conversation", id: conversationId, title: "Refund question", state, open: state !== "closed", read: false,
    created_at: 1767229200, updated_at: 1767229300, waiting_since: 1767229300, snoozed_until: null,
    admin_assignee_id: null, team_assignee_id: 0,
    source: { type: "conversation", delivered_as: "customer_initiated", subject: "", body: "<p>SENTINEL-MESSAGE-TEXT</p>", author: { type: "user", id: "5ba6", name: "Jane", email } },
    contacts: { type: "contact.list", contacts: [{ type: "contact", id: "5ba6", external_id: "jane-1" }] },
    tags: { type: "tag.list", tags: [] },
  });
}

/** A notification envelope as Intercom sends it, with the Go tests' fields. */
export function notificationBody(notificationId: string, topic: string, item: string, createdAt = 1767229300): Buffer {
  return Buffer.from(`{"type":"notification_event","app_id":"ecahpwf5","id":"${notificationId}","topic":"${topic}","data":{"type":"notification_event_data","item":${item}},"links":{},`
    + `"delivery_status":"pending","delivery_attempts":1,"delivered_at":0,"first_sent_at":1767229301,"created_at":${createdAt},"self":null}`);
}

/** Sign a body as Intercom does, with lower-case hex. */
export function signNotification(body: Uint8Array, secret = testClientSecret): string {
  return `sha1=${createHmac("sha1", secret).update(body).digest("hex")}`;
}

/** A loaded project with one Intercom connection, its credential, and conversationEvent bindings. */
export async function newTriggerProject(connectionName: string, bindings: Record<string, Record<string, unknown>>, credential: Record<string, string> = { access_token: testAccessToken, client_secret: testClientSecret }): Promise<LoadedProject> {
  const objects = new InMemoryObjectStore();
  const scope = { projectId: `intercom-${connectionName}`, kind: "live" } as const;
  await new ConnectionStore(objects, scope).replaceCredential({ connectorId: "intercom", connectionName }, 0, new CredentialMaterial(credential, undefined, "apiKey"));
  return new LoadedProject({
    schemaVersion: "connectors.dex.dev/project-configuration/v1alpha2", scope, revision: 1, operationConfigurations: [],
    connections: [{ connectorId: "intercom", connectionName, modulePath: "intercom", provider: "intercom", configuration: { region: "eu" } }],
    triggerBindings: Object.entries(bindings).map(([bindingName, configuration]) => ({ connectorId: "intercom", connectionName, triggerName: "conversationEvent", bindingName, configuration })),
  }, undefined, objects, undefined, undefined);
}

/** A loopback HTTP server for one request listener, closed by close. */
export class LoopbackServer {
  private constructor(private readonly server: Server, readonly url: string) {}

  /** Listen on a free loopback port. */
  static async start(listener: (request: IncomingMessage, response: ServerResponse) => Promise<void> | void): Promise<LoopbackServer> {
    const server = createServer((request, response) => { void listener(request, response); });
    server.listen(0, "127.0.0.1");
    await once(server, "listening");
    const address = server.address();
    if (!address || typeof address === "string") throw new Error("loopback server address is unavailable");
    return new LoopbackServer(server, `http://127.0.0.1:${address.port}/webhooks/intercom`);
  }

  /** Send one request and return its status. */
  async send(method: string, body?: Uint8Array, signature?: string): Promise<number> {
    const headers: Record<string, string> = { "Content-Type": "application/json" };
    if (signature) headers["X-Hub-Signature"] = signature;
    const response = await fetch(this.url, { method, headers, ...(body ? { body: new Uint8Array(body) } : {}) });
    await response.arrayBuffer();
    return response.status;
  }

  /** Stop listening. */
  close(): Promise<void> { return new Promise(resolve => this.server.close(() => resolve())); }
}
