// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { once } from 'node:events';
import { createServer, type Server } from 'node:http';
import type { TriggerEvent, TriggerLogLevel, TriggerLogger, TriggerTarget } from '@superdurable/connectors-sdk';
import { CredentialBrokerClient, InMemoryObjectStore, LoadedProject, TriggerInbox } from '@superdurable/connectors-sdk/projectconfig';
import { RelaySignatureHeader, RelayTimestampHeader, signRelayedRequest } from '@superdurable/connectors-sdk/relaysignature';
import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import type { MessageEvent, MessageTriggerRunner, SlackSocketConnection } from '../src/index.js';

/** Message text that must never reach a record. */
export const sentinelText = 'SENTINEL-MESSAGE-TEXT';
/** Scope token that must never reach a record or error. */
export const relayScopeToken = 'SENTINEL-SCOPE-TOKEN';

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

/** A scripted Socket Mode connection. */
export class FakeSocket implements SlackSocketConnection {
  readonly acknowledged: string[] = [];
  readonly #inbound: unknown[] = [];
  #failure: Error | undefined;
  #wake: (() => void) | undefined;
  isClosed = false;

  push(message: unknown): void { this.#inbound.push(message); this.#wake?.(); }
  fail(message = 'connection reset'): void { this.#failure = new Error(message); this.#wake?.(); }

  async readMessage(readTimeoutMs: number): Promise<unknown> {
    const deadline = Date.now() + readTimeoutMs;
    for (;;) {
      if (this.#inbound.length > 0) return this.#inbound.shift();
      if (this.#failure || this.isClosed) throw this.#failure ?? new Error('connection closed');
      if (Date.now() >= deadline) throw new Error('read Slack Socket Mode envelope: read timeout');
      await new Promise<void>(resolve => { const timer = setTimeout(resolve, Math.max(1, deadline - Date.now())); this.#wake = () => { clearTimeout(timer); resolve(); }; });
      this.#wake = undefined;
    }
  }

  async writeMessage(value: unknown): Promise<void> {
    if (this.isClosed) throw new Error('connection closed');
    this.acknowledged.push((value as { envelope_id: string }).envelope_id);
  }

  close(): void { this.isClosed = true; this.#wake?.(); }
}

/** Fake Slack Web API: apps.connections.open answers with a ticket URL, or the configured error. */
export function newSlackSender(answer: () => { status: number; body: unknown } = () => ({ status: 200, body: { ok: true, url: 'wss://wss-primary.slack.com/link/?ticket=SENTINEL-TICKET' } })) {
  const requests: ProviderRequest[] = [];
  const send = async (request: ProviderRequest): Promise<ProviderResponse> => {
    requests.push(request);
    const { status, body } = answer();
    return { status, headers: {}, body: Buffer.from(JSON.stringify(body)) };
  };
  return { send, requests };
}

/** Events API envelope of a message. */
export function eventsEnvelope(envelopeId: string, eventId: string, message: Record<string, unknown>): Record<string, unknown> {
  return { envelope_id: envelopeId, type: 'events_api', payload: eventsPayload(eventId, message) };
}

/** Events API callback body of a message. */
export function eventsPayload(eventId: string, message: Record<string, unknown>): Record<string, unknown> {
  return { event_id: eventId, event_time: 1760227200, team_id: 'T1', event: { type: 'message', channel: 'C1', user: 'U1', ...message } };
}

/** Root message body. */
export function rootMessage(ts: string, extra: Record<string, unknown> = {}): Record<string, unknown> { return { ts, text: `${sentinelText} request approval`, ...extra }; }
/** Reply message body. */
export function replyMessage(ts: string, threadTs: string, extra: Record<string, unknown> = {}): Record<string, unknown> { return { ts, thread_ts: threadTs, user: 'U2', text: `${sentinelText} approved`, ...extra }; }

/** Targets that record deliveries by kind, optionally failing. */
export class RecordingRoutes {
  readonly deliveries: string[] = [];
  readonly events = new Map<string, TriggerEvent<MessageEvent>>();
  constructor(private readonly fail: (event: TriggerEvent<MessageEvent>) => Error | undefined = () => undefined) {}
  target(kind: string): TriggerTarget<MessageEvent> {
    return { handleTrigger: async (_context, event) => {
      const failure = this.fail(event);
      if (failure) throw failure;
      this.deliveries.push(`${kind}:${event.id}`);
      this.events.set(event.id, event);
    } };
  }
}

/** Host side of the relay: its credential broker and the project configuration for one shared Slack connection. */
export class RelayHost {
  readonly objects = new InMemoryObjectStore();
  credential: Record<string, string> = { bot_token: 'xoxb-SENTINEL-BOT', user_token: 'xoxp-SENTINEL-USER' };
  credentialStatus = 'READY';
  credentialReads = 0;
  readonly registrations: Array<{ authorization: string; body: unknown }> = [];
  readonly project: LoadedProject;
  #registrationAttempt = 0;

  constructor(
    private readonly answer: (attempt: number) => { status: number; body?: unknown } = () => ({ status: 200, body: { status: 'REGISTERED' } }),
    readonly names: { connectionName: string; startBinding: string; replyBinding: string; slackEndpoint: string; triggerBindings?: LoadedProject['configuration']['triggerBindings'] } = { connectionName: 'workspace', startBinding: 'start', replyBinding: 'reply', slackEndpoint: 'http://127.0.0.1:9/api' },
  ) {
    const endpoint = 'https://broker.example.test/internal/connection-credentials/read';
    const broker = new CredentialBrokerClient(endpoint, async () => relayScopeToken, async (url, init) => {
      const headers = new Headers(init?.headers);
      if (String(url).endsWith('/internal/connection-credentials/read')) {
        this.credentialReads++;
        if (this.credentialStatus !== 'READY') return Response.json({ status: this.credentialStatus });
        return Response.json({ status: 'READY', credential: this.credential, revision: 1 });
      }
      this.registrations.push({ authorization: headers.get('authorization') ?? '', body: JSON.parse(String(init?.body)) as unknown });
      const { status, body } = this.answer(++this.#registrationAttempt);
      return body === undefined ? new Response('', { status }) : Response.json(body, { status });
    });
    const configuration = {
      schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha3' as const, scope: { projectId: 'relay', kind: 'live' as const }, revision: 1, operationConfigurations: [],
      connections: [{ connectorId: 'slack', connectionName: names.connectionName, modulePath: 'slack', provider: 'slack', sharedConnectionId: 'conn-slack', configuration: { endpoint: names.slackEndpoint } }],
      triggerBindings: names.triggerBindings ?? [
        { connectorId: 'slack', connectionName: names.connectionName, triggerName: 'channelThreadCreated', bindingName: names.startBinding, configuration: { channelId: 'C1', threadTriggerMatcher: { messageContains: 'request' } } },
        { connectorId: 'slack', connectionName: names.connectionName, triggerName: 'threadReplyCreated', bindingName: names.replyBinding, configuration: { channelId: 'C1', threadReplyMatcher: { posterUserIds: ['U2'] } } },
      ],
      credentialBroker: { endpoint, tokenSecretRef: { key: `projects/relay/live/app-secrets/DEX_CREDENTIAL_BROKER_TOKEN/${'a'.repeat(32)}`, version: 'v1', digest: `sha256:${'b'.repeat(64)}` } },
    };
    this.project = new LoadedProject(configuration, undefined, this.objects, undefined, broker);
  }

  async pendingEventIds(): Promise<string[]> {
    const ids: string[] = [];
    for (const [triggerName, bindingName] of [['channelThreadCreated', this.names.startBinding], ['threadReplyCreated', this.names.replyBinding]] as const) {
      ids.push(...(await new TriggerInbox(this.objects, { projectId: 'relay', kind: 'live' }, { connectorId: 'slack', connectionName: this.names.connectionName, triggerName, bindingName }).pending()).map(event => event.id));
    }
    return ids;
  }
}

/** Serve a runner's relayed route on a loopback port. */
export async function serveRunner(runner: MessageTriggerRunner, servers: Server[]): Promise<string> {
  const server = createServer((request, response) => { void runner.serveHTTP(request, response); });
  servers.push(server);
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('server address unavailable');
  return `http://127.0.0.1:${address.port}`;
}

/** Post a relayed callback signed with token at signedAt. */
export async function postRelayed(url: string, body: unknown, signedAt = new Date(), token = relayScopeToken, headers: Record<string, string> = {}): Promise<number> {
  const raw = Buffer.from(typeof body === 'string' ? body : JSON.stringify(body));
  const response = await fetch(url, { method: 'POST', body: raw, headers: {
    [RelayTimestampHeader]: String(Math.floor(signedAt.getTime() / 1000)), [RelaySignatureHeader]: signRelayedRequest(token, signedAt, raw), ...headers,
  } });
  return response.status;
}
