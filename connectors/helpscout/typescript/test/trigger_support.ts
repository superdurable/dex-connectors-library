// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHmac } from 'node:crypto';
import { once } from 'node:events';
import { createServer, type IncomingMessage, type Server, type ServerResponse } from 'node:http';
import { SecretString, StaticCredentialProvider, type TriggerEvent, type TriggerLogLevel, type TriggerLogger, type TriggerTarget } from '@superdurable/connectors-sdk';
import { HelpScoutClient, type HelpScoutClientOptions } from '../src/client.js';
import { newConnection, type Config, type Connection, type ConversationEvent, type Credentials } from '../src/index.js';

/** Webhook secret that must never reach a response, an event, or a record. */
export const sentinelWebhookSecret = 'SENTINEL-WEBHOOK-SECRET';
/** The tests' connection. */
export const testConnection = { provider: 'helpscout', name: 'helpscout-support' };
/** The receipt clock of every test client. */
export const fixedNow = new Date('2026-10-01T08:00:00Z');

/** A connection whose credential holds webhookSecret; a blank secret stores none. */
export function newTestConnection(webhookSecret: string, config: Config = {}, options: HelpScoutClientOptions = {}): Connection {
  const credentials: Credentials = {
    appId: 'inert-app-id', appSecret: new SecretString('inert-app-secret'), accessToken: new SecretString('inert-access-token'),
    ...(webhookSecret ? { webhookSecret: new SecretString(webhookSecret) } : {}),
  };
  return newConnection(new HelpScoutClient(config, new StaticCredentialProvider([[testConnection, credentials]]), { now: () => fixedNow, ...options }), testConnection);
}

/** The base64 HMAC-SHA1 of the raw body, as Help Scout signs it. */
export function helpScoutSignature(secret: string, body: string): string {
  return createHmac('sha1', secret).update(body).digest('base64');
}

/** A v2 conversation webhook body, as Help Scout sends it. */
export function conversationJSON(id: number, status: string, mailboxId: number, ...tags: string[]): string {
  return JSON.stringify({
    id, number: 12, threads: 1, type: 'email', status, state: 'published', subject: 'Double charge on order 88213', mailboxId,
    createdAt: '2026-09-30T11:58:00Z', primaryCustomer: { id: 238604, type: 'customer', first: 'Jane', last: 'Smith', email: 'jane@acme.example.com' },
    tags: tags.map(tag => ({ tag })),
  });
}

/** A loopback server for a node:http request listener, closed by closeAll. */
export class LoopbackServers {
  readonly #servers: Server[] = [];

  async serve(listener: (request: IncomingMessage, response: ServerResponse) => Promise<void>): Promise<string> {
    const server = createServer((request, response) => { void listener(request, response); });
    this.#servers.push(server);
    server.listen(0, '127.0.0.1');
    await once(server, 'listening');
    const address = server.address();
    if (!address || typeof address === 'string') throw new Error('loopback server address is unavailable');
    return `http://127.0.0.1:${address.port}/webhooks/helpscout`;
  }

  async closeAll(): Promise<void> {
    await Promise.all(this.#servers.splice(0).map(server => new Promise<void>(resolve => { server.closeAllConnections(); server.close(() => resolve()); })));
  }
}

/** One delivery's answer. */
export interface DeliveryAnswer { status: number; text: string }

/** Post body as Help Scout does, signed with secret; a blank event or secret omits its header. */
export async function deliver(url: string, event: string, body: string, secret: string, signature = secret ? helpScoutSignature(secret, body) : ''): Promise<DeliveryAnswer> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };
  if (event) headers['X-HelpScout-Event'] = event;
  if (signature) headers['X-HelpScout-Signature'] = signature;
  const response = await fetch(url, { method: 'POST', headers, body });
  return { status: response.status, text: await response.text() };
}

/** A target that records each delivered event, optionally failing. */
export class RecordingTarget implements TriggerTarget<ConversationEvent> {
  readonly events: Array<TriggerEvent<ConversationEvent>> = [];
  #waiters: Array<() => void> = [];
  constructor(private readonly failure?: () => Error) {}

  async handleTrigger(_context: unknown, event: TriggerEvent<ConversationEvent>): Promise<void> {
    if (this.failure) throw this.failure();
    this.events.push(event);
    for (const wake of this.#waiters.splice(0)) wake();
  }

  /** Resolve with the event at index once it is delivered, or reject after five seconds. */
  async eventAt(index: number): Promise<TriggerEvent<ConversationEvent>> {
    const deadline = Date.now() + 5_000;
    while (this.events.length <= index) {
      if (Date.now() >= deadline) throw new Error('the verified event was not delivered');
      await new Promise<void>(resolve => { const timer = setTimeout(resolve, 50); this.#waiters.push(() => { clearTimeout(timer); resolve(); }); });
    }
    return this.events[index]!;
  }
}

/** A logger that keeps every record. */
export function newRecordingLogger(): TriggerLogger & { records: Array<{ level: TriggerLogLevel; message: string; attributes: Record<string, unknown> }> } {
  const records: Array<{ level: TriggerLogLevel; message: string; attributes: Record<string, unknown> }> = [];
  return { records, isEnabled: () => true, log: (level, message, attributes) => { records.push({ level, message, attributes: { ...attributes } }); } };
}

/** Poll until isDone holds, for at most five seconds. */
export async function eventually(isDone: () => boolean | Promise<boolean>, message: string): Promise<void> {
  const deadline = Date.now() + 5_000;
  while (!(await isDone())) {
    if (Date.now() >= deadline) throw new Error(message);
    await new Promise(resolve => setTimeout(resolve, 5));
  }
}
