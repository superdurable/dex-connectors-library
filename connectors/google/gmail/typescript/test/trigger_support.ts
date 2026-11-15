// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, StaticCredentialProvider, type TriggerLogAttributes, type TriggerLogLevel, type TriggerLogger } from '@superdurable/connectors-sdk';
import { ConnectionStore, CredentialMaterial, InMemoryObjectStore, LoadedProject, type OperationConfiguration } from '@superdurable/connectors-sdk/projectconfig';
import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GmailClient, GmailConnection, type GmailClientOptions } from '../src/index.js';

/** One message the fake Gmail API lists and reads. */
export interface FakeGmailMessage { id: string; threadId: string; from: string; subject: string; snippet?: string; inReplyTo?: string; references?: string; internalDate?: string }

/** Fake Gmail API: an inbox listed newest first, metadata reads, and scripted failures. */
export class FakeGmail {
  /** Inbox, newest first. */ inbox: FakeGmailMessage[] = [];
  /** Every request, in order. */ readonly requests: ProviderRequest[] = [];
  /** Status answered by the next list calls, consumed one per call. */ readonly listFailures: number[] = [];
  /** Message IDs whose next read answers this status. */ readonly readFailures = new Map<string, number>();
  /** Inbox list calls so far. */ listCount = 0;
  /** Decoded raw MIME messages posted to messages.send. */ readonly sent: string[] = [];
  /** Message IDs whose format=full reads wait for the promise, such as a Flow's getMessage Step. */ readonly fullReadGates = new Map<string, Promise<void>>();

  /** Add a message as the newest inbox entry. */
  receive(message: FakeGmailMessage): void { this.inbox.unshift(message); }

  /** The provider transport bound to this fake. */
  readonly transport = async (request: ProviderRequest): Promise<ProviderResponse> => {
    this.requests.push(request);
    const url = new URL(request.url);
    const json = (status: number, body: unknown): ProviderResponse => ({ status, headers: { 'content-type': 'application/json' }, body: Buffer.from(JSON.stringify(body)) });
    if (url.pathname === '/gmail/v1/users/me/messages/send' && request.method === 'POST') {
      const body = JSON.parse(Buffer.from(request.body ?? []).toString('utf8')) as { raw: string; threadId?: string };
      this.sent.push(Buffer.from(body.raw, 'base64url').toString('utf8'));
      return json(200, { id: `sent-${this.sent.length}`, threadId: body.threadId ?? `sent-thread-${this.sent.length}` });
    }
    if (url.pathname === '/gmail/v1/users/me/messages' && url.searchParams.get('q')?.startsWith('in:sent')) return json(200, {});
    if (url.pathname === '/gmail/v1/users/me/messages') {
      this.listCount++;
      const failure = this.listFailures.shift();
      if (failure) return json(failure, { error: { message: `query ${url.searchParams.get('q') ?? ''} failed` } });
      const page = this.inbox.slice(0, Number(url.searchParams.get('maxResults')));
      return json(200, page.length ? { messages: page.map(message => ({ id: message.id, threadId: message.threadId })) } : {});
    }
    const id = decodeURIComponent(url.pathname.slice('/gmail/v1/users/me/messages/'.length));
    if (url.searchParams.get('format') === 'full') await this.fullReadGates.get(id);
    const failure = this.readFailures.get(id);
    if (failure) { this.readFailures.delete(id); return json(failure, {}); }
    const message = this.inbox.find(candidate => candidate.id === id);
    if (!message) return json(404, {});
    const headers = [
      { name: 'From', value: message.from }, { name: 'Subject', value: message.subject }, { name: 'Message-ID', value: `<${message.id}@mail.example.com>` },
      ...(message.inReplyTo ? [{ name: 'In-Reply-To', value: message.inReplyTo }] : []), ...(message.references ? [{ name: 'References', value: message.references }] : []),
    ];
    return json(200, { id: message.id, threadId: message.threadId, snippet: message.snippet ?? '', internalDate: message.internalDate ?? '1767312000000', payload: { headers } });
  };

  /** Reads of message metadata, by message ID, in order. */
  reads(): string[] {
    return this.requests.filter(request => request.method === 'GET').map(request => new URL(request.url).pathname).filter(path => path !== '/gmail/v1/users/me/messages').map(path => path.split('/').at(-1)!);
  }
}

/** A root message from Ada. */
export function rootMessage(id: string, threadId = id, overrides: Partial<FakeGmailMessage> = {}): FakeGmailMessage {
  return { id, threadId, from: 'Ada <ada@example.com>', subject: 'Order question', snippet: 'Please check order 1042', ...overrides };
}

/** A reply in threadId. */
export function replyMessage(id: string, threadId: string, overrides: Partial<FakeGmailMessage> = {}): FakeGmailMessage {
  return { id, threadId, from: 'Ada <ada@example.com>', subject: 'Re: Order question', snippet: 'Approved, thanks', inReplyTo: `<${threadId}@mail.example.com>`, ...overrides };
}

/** Connection reference of the tests. */
export const ConnectionReference = { provider: 'google', name: 'gmail-inbox' } as const;

/** A Gmail connection on the fake with static credentials. */
export function fakeConnection(gmail: FakeGmail, options: GmailClientOptions = {}): GmailConnection {
  const credentials = new StaticCredentialProvider([[ConnectionReference, { accessToken: new SecretString('SENTINEL-ACCESS'), primaryEmail: 'owner@example.com' }]]);
  const client = new GmailClient({ endpoint: 'http://127.0.0.1:9/gmail/v1', pollIntervalMs: 1000 }, credentials, { transport: gmail.transport, ...options });
  return new GmailConnection(client, ConnectionReference);
}

/** One record a RecordingLogger received. */
export interface RecordedTriggerLog { level: TriggerLogLevel; message: string; attributes: TriggerLogAttributes }

/** Records every Trigger record at every level. */
export class RecordingLogger implements TriggerLogger {
  readonly records: RecordedTriggerLog[] = [];
  isEnabled(): boolean { return true; }
  log(level: TriggerLogLevel, message: string, attributes: TriggerLogAttributes): void { this.records.push({ level, message, attributes }); }
  /** Records with this message whose attributes include attributes. */
  find(message: string, attributes: TriggerLogAttributes = {}): RecordedTriggerLog[] {
    return this.records.filter(record => record.message === message && Object.entries(attributes).every(([key, value]) => record.attributes[key] === value));
  }
}

/** A project with one Gmail connection, stored credentials, and the given Trigger bindings and operation configurations. */
export async function gmailProject(
  triggerBindings: Array<{ triggerName: string; bindingName: string; configuration: Record<string, unknown> }>,
  endpoint = 'http://127.0.0.1:9/gmail/v1',
  operationConfigurations: OperationConfiguration[] = [],
): Promise<LoadedProject> {
  const objects = new InMemoryObjectStore();
  const scope = { projectId: 'gmail-triggers', kind: 'live' } as const;
  await new ConnectionStore(objects, scope).replaceCredential({ connectorId: 'gmail', connectionName: ConnectionReference.name }, 0, new CredentialMaterial({
    auth_method: 'google-oauth', oauth_client_id: 'client', oauth_client_secret: 'SENTINEL-CLIENT-SECRET', access_token: 'SENTINEL-ACCESS', refresh_token: 'SENTINEL-REFRESH', primary_email: 'owner@example.com',
  }, new Date(Date.now() + 3_600_000), 'google-oauth'));
  return new LoadedProject({
    schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha2', scope, revision: 1, operationConfigurations,
    connections: [{ connectorId: 'gmail', connectionName: ConnectionReference.name, modulePath: '@superdurable/connector-google-gmail', provider: 'google', configuration: { endpoint, pollInterval: 1_000_000_000 } }],
    triggerBindings: triggerBindings.map(binding => ({ connectorId: 'gmail', connectionName: ConnectionReference.name, ...binding })),
  }, undefined, objects, undefined, undefined);
}
