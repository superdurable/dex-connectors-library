// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, StaticCredentialProvider, type Call, type CredentialRefreshDriver } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GmailHTTPClient } from '../src/http.js';
import type { Credentials } from '../src/models.js';
import { GmailProvider } from '../src/provider.js';
import { GmailSender } from '../src/send.js';

/** Connection reference of the Sent Mail tests. */
export const SentMailConnection = { provider: 'google', name: 'gmail-inbox' } as const;
/** Stable connector Call ID and idempotency key of the Sent Mail tests. */
export const SentMailCallID = '01234567-89ab-5cde-8f01-23456789abcd';

/** One message header as Gmail returns it. */
export interface FakeGmailHeader { name: string; value: string }
/** One Sent Mail message with the headers Gmail stored on it. */
export interface FakeSentCopy { id: string; threadId: string; headers: FakeGmailHeader[] }

/**
 * How Gmail and the worker handle one messages.send POST: `accepted` answers it, `acceptedResponseLost` keeps
 * the Sent copy but loses the connection, `acceptedWorkerCrashed` keeps the Sent copy and never answers, as
 * when the worker dies after Gmail accepted, and `lost` loses the connection before Gmail accepted it.
 */
export type FakeSendOutcome = 'accepted' | 'acceptedResponseLost' | 'acceptedWorkerCrashed' | 'lost';

/** Dex context fields that derive the run-specific Message-ID. */
export interface SentMailContextFields { runId?: string; flowStartedAt?: Date | null }

const credentials: Credentials = { accessToken: new SecretString('access-token'), primaryEmail: 'owner@example.com' };
const driver: CredentialRefreshDriver<Credentials> = { refreshRequired: () => false, refresh: async () => { throw new Error('unexpected refresh'); } };

/**
 * Fake Gmail API that behaves like real Gmail on send: the Sent copy gets a Gmail-generated Message-Id and keeps
 * every other header, including X-Dex-Message-ID; search lists candidates newest first; metadata reads return
 * only the requested headers.
 */
export class FakeGmailSentMail {
  /** Sent Mail, newest first. */ readonly sentMail: FakeSentCopy[] = [];
  /** Decoded raw MIME messages that reached Gmail, in order. */ readonly postedMessages: string[] = [];
  /** Sent Mail search queries, in order. */ readonly searchQueries: string[] = [];
  /** Message IDs whose Sent Mail headers were read, in order. */ readonly headerReads: string[] = [];
  /** Requests of every kind. */ requests = 0;
  /** Reads of the reply source message. */ sourceReads = 0;
  readonly #sendOutcomes: FakeSendOutcome[];
  readonly #failingHeaderReads: ReadonlyMap<string, number>;

  /** Script one outcome per POST, accepted once exhausted, and a status for header reads of the given IDs. */
  constructor(options: { sendOutcomes?: FakeSendOutcome[]; failingHeaderReads?: ReadonlyMap<string, number> } = {}) {
    this.#sendOutcomes = [...(options.sendOutcomes ?? [])];
    this.#failingHeaderReads = options.failingHeaderReads ?? new Map();
  }

  /** Add a Sent Mail message, such as another run's copy, as the newest one. */
  addSentCopy(copy: FakeSentCopy): void { this.sentMail.unshift(copy); }

  /** A Gmail sender bound to this fake, as one worker process would build it. */
  sender(): GmailSender {
    const http = this.httpClient();
    return new GmailSender(http, new GmailProvider(http));
  }

  /** An authenticated Gmail HTTP client bound to this fake. */
  httpClient(): GmailHTTPClient {
    return new GmailHTTPClient({ endpoint: 'http://127.0.0.1:8080' }, new StaticCredentialProvider([[SentMailConnection, credentials]]), driver, request => this.#answer(request));
  }

  /** The credentials the fake's connection resolves. */
  credentials(): Credentials { return credentials; }

  /** POSTs that reached Gmail. */
  posts(): number { return this.postedMessages.length; }

  async #answer(request: ProviderRequest): Promise<ProviderResponse> {
    this.requests++;
    const url = new URL(request.url);
    if (request.method === 'POST') return this.#acceptSend(request);
    if (url.pathname === '/users/me/messages') return this.#listSentMail(url);
    const messageId = decodeURIComponent(url.pathname.slice('/users/me/messages/'.length));
    if (messageId === 'source-1') {
      this.sourceReads++;
      return json(200, { id: 'source-1', threadId: 'source-thread', internalDate: '1767312000000', payload: { headers: [
        { name: 'Message-ID', value: '<source@example.com>' }, { name: 'From', value: 'Ada <ada@example.com>' }, { name: 'Subject', value: 'Question' },
      ] } });
    }
    const copy = this.sentMail.find(candidate => candidate.id === messageId);
    if (url.searchParams.get('format') === 'minimal') return copy ? json(200, { id: copy.id, threadId: copy.threadId }) : json(404, {});
    this.headerReads.push(messageId);
    const failureStatus = this.#failingHeaderReads.get(messageId);
    if (failureStatus) return json(failureStatus, {});
    if (!copy) return json(404, { error: { code: 404 } });
    const requested = url.searchParams.getAll('metadataHeaders').map(name => name.toLowerCase());
    const headers = copy.headers.filter(header => requested.includes(header.name.toLowerCase()));
    return json(200, { id: copy.id, threadId: copy.threadId, labelIds: ['SENT'], payload: { mimeType: 'text/plain', headers } });
  }

  async #acceptSend(request: ProviderRequest): Promise<ProviderResponse> {
    const payload = JSON.parse(Buffer.from(request.body ?? []).toString('utf8')) as { raw: string; threadId?: string };
    const raw = Buffer.from(payload.raw, 'base64url').toString('utf8');
    const outcome = this.#sendOutcomes.shift() ?? 'accepted';
    if (outcome === 'lost') throw new ProviderTransportError(false);
    this.postedMessages.push(raw);
    const sequence = this.postedMessages.length;
    const copy: FakeSentCopy = { id: `gmail-${sequence}`, threadId: payload.threadId ?? `gmail-thread-${sequence}`, headers: sentCopyHeaders(raw, sequence) };
    this.addSentCopy(copy);
    if (outcome === 'acceptedResponseLost') throw new ProviderTransportError(false);
    // The worker died with the request in flight, so no response is ever seen.
    if (outcome === 'acceptedWorkerCrashed') return new Promise<never>(() => undefined);
    return json(200, { id: copy.id, threadId: copy.threadId });
  }

  #listSentMail(url: URL): ProviderResponse {
    const query = url.searchParams.get('q') ?? '';
    this.searchQueries.push(query);
    if (!query.startsWith('in:sent ')) return json(400, {});
    const recipient = / to:(\S+)/.exec(query)?.[1]?.toLowerCase();
    const matches = this.sentMail
      .filter(copy => !recipient || copy.headers.some(header => header.name.toLowerCase() === 'to' && header.value.toLowerCase().includes(recipient)))
      .slice(0, Number(url.searchParams.get('maxResults') ?? '100'));
    // Gmail omits the messages field when nothing matches.
    return json(200, matches.length
      ? { messages: matches.map(copy => ({ id: copy.id, threadId: copy.threadId })), resultSizeEstimate: matches.length }
      : { resultSizeEstimate: 0 });
  }
}

/** Return the value of the first raw MIME header with this name, ignoring case. */
export function rawMIMEHeader(raw: string, name: string): string | undefined {
  const headerBlock = raw.slice(0, raw.indexOf('\r\n\r\n'));
  return headerBlock.split('\r\n').find(line => line.toLowerCase().startsWith(`${name.toLowerCase()}: `))?.slice(name.length + 2);
}

/** A Dex call for one Gmail Mutation attempt, sharing the given checkpoint across attempts. */
export function sentMailOperationCall(operationId: string, fields: SentMailContextFields, attempt = 1, checkpoint: { value?: unknown } = {}): Call {
  return {
    id: SentMailCallID, idempotencyKey: SentMailCallID, connection: SentMailConnection,
    operation: { connectorId: 'gmail', operationId },
    context: {
      attempt, ...fields, cancellationSignal: new AbortController().signal, flowId: 'flow-1',
      hasLastHeartbeatValue: () => checkpoint.value !== undefined,
      getLastHeartbeatValue: () => checkpoint.value,
      recordHeartbeat: async (value: unknown) => { checkpoint.value = value; },
    },
  } as unknown as Call;
}

// Gmail replaces Message-ID on the Sent copy and keeps the other headers verbatim.
function sentCopyHeaders(raw: string, sequence: number): FakeGmailHeader[] {
  const headerBlock = raw.slice(0, raw.indexOf('\r\n\r\n'));
  return headerBlock.split('\r\n').map(line => {
    const separator = line.indexOf(': ');
    const header = { name: line.slice(0, separator), value: line.slice(separator + 2) };
    return header.name.toLowerCase() === 'message-id' ? { name: 'Message-Id', value: `<CAgmail-${sequence}@mail.gmail.com>` } : header;
  });
}

function json(status: number, body: unknown): ProviderResponse {
  return { status, headers: { 'content-type': 'application/json' }, body: Buffer.from(JSON.stringify(body)) };
}
