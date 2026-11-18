// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { BodyTooLargeError, ProviderStalledError, ProviderTransportError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ReauthorizationRequiredError, SecretString, type Call, type CredentialProvider, type Failure, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, ObjectNotFoundError } from '@superdurable/connectors-sdk/projectconfig';
import type { Config, Credentials } from './generated/models.js';
import { AddNoteOperation, CreateSharedDraftOperation, GetConversationOperation, ListConversationsOperation, ListInboxesOperation, UpdateConversationOperation } from './operations.js';

/** Provider sender for replay and local testing. */
export type HiverRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Optional local fake and transport dependencies. */
export interface HiverClientOptions {
  /** Local Hiver-compatible base URL. */ apiBaseURL?: string;
  /** Provider HTTP sender. */ send?: HiverRequestSender;
  /** Receipt and retry clock. */ now?: () => Date;
}
/** One classified Hiver request. */
export type HiverExchange = { kind: 'success'; response: ProviderResponse } | { kind: 'notFound' | 'rateLimited' | 'notSent' | 'unavailable' | 'rejected' | 'invalid' | 'defect'; failure: Failure; retryAfterMs: number; status?: number; response?: ProviderResponse };
/** A resolved API key, or a credential failure that is terminal or may clear on retry. */
export type HiverCredentialResult = { kind: 'resolved'; key: string } | { kind: 'terminal' | 'transient' };
/** One HTTP request's payload. */
export interface HiverRequest {
  method: string; path: string; query?: URLSearchParams; json?: unknown; form?: ReadonlyArray<readonly [string, string]>; correlationCallId?: string;
  /** Read a GET refusal body in full, as a write does, because the answer decides a write. */ isPreWriteLookup?: boolean;
}

/** Statuses that prove Hiver applied nothing when their body reads cleanly without a created ID; operations.ts keeps the same set. */
const definiteRefusalStatuses: ReadonlySet<number> = new Set([400, 401, 403, 404, 411, 413, 414, 415, 422]);

/** Hiver's largest list page, which every conversationReceived poll request asks for. */
export const MaximumPageSize = 100;
/** The fewest pages one conversationReceived poll reads: the list's first page plus one sweep page. */
export const MinimumPollPagesPerPoll = 2;
/** The most pages one conversationReceived poll reads, keeping it within the poll kit's 500 events per page. */
export const MaximumPollPagesPerPoll = 5;
const defaultPollIntervalMs = 300_000;
const defaultPollPagesPerPoll = 3;

/**
 * Hiver v1 client with connection-wide request spacing. pollIntervalMs and pollPagesPerPoll configure the
 * conversationReceived Trigger: the delay between polls of each binding and the list pages one poll reads.
 * Omitted or zero values keep the manifest defaults of five minutes and three pages.
 */
export class HiverClient {
  /** The conversationReceived Trigger's delay between polls in milliseconds; at least 1000. */
  readonly pollIntervalMs: number;
  /** Pages of 100 conversations one conversationReceived poll reads, from 2 to 5. */
  readonly pollPagesPerPoll: number;
  private readonly baseURL: string;
  private readonly credentials: CredentialProvider<Credentials>;
  private readonly send: HiverRequestSender;
  private readonly maxResponseBytes: number;
  private readonly intervalMs: number;
  private readonly now: () => Date;
  private nextSlot = 0;
  private heldUntil = 0;
  private rateLimitPenalty = 0;
  private slotQueue: Promise<void> = Promise.resolve();

  /** Validate connection settings and bind the credential source. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: HiverClientOptions = {}) {
    if (!credentials) throw new TypeError('Hiver credential provider is required');
    this.baseURL = validateBaseURL(options.apiBaseURL ?? 'https://api2.hiverhq.com/v1');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
    this.maxResponseBytes = config.maxResponseBytes || 4_194_304;
    this.intervalMs = config.requestIntervalMilliseconds || 1000;
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes < 1) throw new TypeError('Hiver response limit must be positive');
    if (!Number.isSafeInteger(this.intervalMs) || this.intervalMs < 1 || this.intervalMs > 60_000) throw new TypeError('Hiver request interval must be between 1 and 60000 milliseconds');
    this.pollIntervalMs = config.pollIntervalMs || defaultPollIntervalMs;
    if (!Number.isFinite(this.pollIntervalMs) || this.pollIntervalMs < 1000) throw new TypeError('Hiver pollInterval must be at least one second');
    this.pollPagesPerPoll = config.pollPagesPerPoll || defaultPollPagesPerPoll;
    if (!Number.isSafeInteger(this.pollPagesPerPoll) || this.pollPagesPerPoll < MinimumPollPagesPerPoll || this.pollPagesPerPoll > MaximumPollPagesPerPoll) {
      throw new TypeError(`Hiver pollPagesPerPoll must be from ${MinimumPollPagesPerPoll} to ${MaximumPollPagesPerPoll}`);
    }
  }

  /** The client clock, used for receipts, Retry-After, and Trigger observation times. */
  currentTime(): Date { return this.now(); }

  /** Bind listInboxes. */ listInboxes(): ListInboxesOperation { return new ListInboxesOperation(this); }
  /** Bind listConversations. */ listConversations(): ListConversationsOperation { return new ListConversationsOperation(this); }
  /** Bind getConversation. */ getConversation(): GetConversationOperation { return new GetConversationOperation(this); }
  /** Bind updateConversation. */ updateConversation(): UpdateConversationOperation { return new UpdateConversationOperation(this); }
  /** Bind addNote. */ addNote(): AddNoteOperation { return new AddNoteOperation(this); }
  /** Bind createSharedDraft. */ createSharedDraft(): CreateSharedDraftOperation { return new CreateSharedDraftOperation(this); }

  /** Make a secret-free receipt for this attempt. */
  receipt(call: Call, response?: ProviderResponse, objectId = ''): ReceiptDraft {
    const header = response?.headers['x-request-id'];
    const requestId = Array.isArray(header) ? header[0] : header;
    return { callId: call.id, provider: 'hiver', observedAt: this.now().toISOString(),
      ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}),
      ...(requestId && /^[A-Za-z0-9._:-]{1,128}$/.test(requestId) ? { providerRequestId: requestId } : {}),
      ...(objectId ? { providerObjectId: objectId } : {}) };
  }

  /** Resolve and validate one API key before a request. */
  async resolveKey(call: Call): Promise<string | undefined> {
    const result = await this.resolveKeyOrFailure(call);
    return result.kind === 'resolved' ? result.key : undefined;
  }

  /**
   * Resolve one API key, or report whether its failure is terminal: a revoked, absent, or malformed
   * credential. Any other resolution error is transient and may clear on a later attempt.
   */
  async resolveKeyOrFailure(call: Call): Promise<HiverCredentialResult> {
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch (error) {
      return { kind: error instanceof ReauthorizationRequiredError || error instanceof ObjectNotFoundError || error instanceof ConnectionNotSelectedError || error instanceof CredentialBrokerRejectedError ? 'terminal' : 'transient' };
    }
    if (!(credentials?.apiKey instanceof SecretString)) return { kind: 'terminal' };
    const key = credentials.apiKey.reveal();
    return key && isHeaderSafeCredential(key) ? { kind: 'resolved', key } : { kind: 'terminal' };
  }

  /** Reserve a spaced request slot, honoring cancellation and rate-limit holds. */
  async waitForSlot(signal: AbortSignal, deadline = Infinity): Promise<boolean> {
    let release: () => void = () => {};
    const previous = this.slotQueue;
    this.slotQueue = new Promise<void>(resolve => { release = resolve; });
    await previous;
    try {
      for (;;) {
        const now = Date.now();
        const slot = Math.max(now, this.nextSlot, this.heldUntil);
        this.nextSlot = slot + this.intervalMs;
        if (slot >= deadline || signal.aborted) return false;
        if (slot > now && !await wait(slot - now, signal)) return false;
        if (Date.now() >= this.heldUntil) return !signal.aborted;
      }
    } finally { release(); }
  }

  /** Send one authenticated request and classify the status or transport. */
  async exchange(call: Call, operation: string, key: string, request: HiverRequest, signal = call.context.cancellationSignal, hasSlot = false): Promise<HiverExchange> {
    if (!hasSlot && !await this.waitForSlot(signal)) return this.failure('notSent', operation, 'AVAILABILITY', "the wait for the connection's next Hiver request slot ended; no request was sent");
    const headers: Record<string, string> = { Authorization: `Bearer ${key}`, Accept: 'application/json' };
    if (request.correlationCallId) headers['X-Dex-Call-ID'] = request.correlationCallId;
    let body: Uint8Array | undefined;
    if (request.json !== undefined) { body = Buffer.from(JSON.stringify(request.json)); headers['Content-Type'] = 'application/json'; }
    if (request.form) { const multipart = encodeMultipart(request.form); body = multipart.body; headers['Content-Type'] = multipart.contentType; }
    const query = request.query?.toString();
    const url = this.baseURL + request.path + (query ? `?${query}` : '');
    const isWrite = request.method !== 'GET' || request.isPreWriteLookup === true;
    let response: ProviderResponse;
    // A write reads its whole error body, because only a readable refusal is definite.
    let isErrorBodyUnreadable = false;
    try {
      response = await this.send({ url, method: request.method, headers, ...(body ? { body } : {}), maxResponseBytes: this.maxResponseBytes,
        ...(isWrite ? {} : { maxErrorResponseBytes: 0, ignoreErrorBodyReadFailure: true }), timeoutMs: 12_000, signal,
        addressPolicy: 'privateNetworkHTTP' });
    } catch (error) {
      const errorStatus = error instanceof BodyTooLargeError || error instanceof ProviderTransportError || error instanceof ProviderStalledError ? error.status : undefined;
      const errorHeaders = error instanceof BodyTooLargeError || error instanceof ProviderTransportError || error instanceof ProviderStalledError ? error.headers : undefined;
      const isErrorStatus = errorStatus !== undefined && errorStatus > 0 && (errorStatus < 200 || errorStatus >= 300);
      if (error instanceof BodyTooLargeError) {
        if (error.status !== undefined && error.status >= 200 && error.status < 300) return this.failure('invalid', operation, 'RESPONSE_TOO_LARGE', 'Hiver response exceeds the configured maxResponseBytes limit', 0, error.status);
        if (error.status !== undefined && error.headers) { response = { status: error.status, headers: error.headers, body: Buffer.alloc(0) }; isErrorBodyUnreadable = true; }
        else return this.failure('unavailable', operation, 'TRANSPORT', 'Hiver response could not be read');
      } else if (error instanceof ProviderTransportError && error.isProvablyNotSent) return this.failure('notSent', operation, 'TRANSPORT', 'Hiver could not be reached; no request was sent');
      else if (isErrorStatus && errorHeaders) {
        // The status still classifies a 429 or 5xx; a refusal status becomes unknown below.
        response = { status: errorStatus, headers: errorHeaders, body: Buffer.alloc(0) };
        isErrorBodyUnreadable = true;
      } else if ((error instanceof ProviderTransportError || error instanceof ProviderStalledError) && error.status !== undefined && error.status > 0 && error.headers) {
        // Keep the status so a write Hiver accepted is never resent.
        return this.failure('unavailable', operation, 'TRANSPORT', 'Hiver response could not be read', 0, error.status, { status: error.status, headers: error.headers, body: Buffer.alloc(0) });
      } else return this.failure('unavailable', operation, 'TRANSPORT', 'Hiver request failed before a response arrived');
    }
    const status = response.status;
    const retryAfterMs = parseRetryAfter(String(response.headers['retry-after'] ?? ''), this.now());
    if (status === 429) {
      this.rateLimitPenalty = Math.min(Math.max(this.rateLimitPenalty * 2, 4 * this.intervalMs), 60_000);
      this.heldUntil = Math.max(this.heldUntil, Date.now() + Math.min(Math.max(retryAfterMs, this.rateLimitPenalty), 60_000));
      this.nextSlot = Math.max(this.nextSlot, this.heldUntil);
      return this.failure('rateLimited', operation, 'RATE_LIMIT', 'Hiver rate limited the request (HTTP 429): one request per second or the daily request limit was exceeded', retryAfterMs, status, response);
    }
    this.rateLimitPenalty = 0;
    if (status === 408 || status >= 500) return this.failure('unavailable', operation, 'AVAILABILITY', `Hiver could not complete the request (HTTP ${status})`, retryAfterMs, status, response);
    // A credential-reflecting refusal body is untrusted, so it is treated like an unreadable one.
    if (status < 200 || status >= 300) isErrorBodyUnreadable ||= key !== '' && Buffer.from(response.body).includes(Buffer.from(key));
    if (isWrite && isErrorBodyUnreadable && definiteRefusalStatuses.has(status)) {
      return this.failure('unavailable', operation, 'TRANSPORT', `Hiver answered HTTP ${status}, but its oversized or unreadable body may hide an accepted write`, 0, status, response);
    }
    if (status === 404) return this.failure('notFound', operation, 'NOT_FOUND', 'Hiver found no such resource (HTTP 404)', 0, status, response);
    if (status >= 300 && status < 400) return this.failure('rejected', operation, 'PROTOCOL', `Hiver redirected the request (HTTP ${status}), which the connector never follows`, 0, status, response);
    if (status < 200 || status >= 300) {
      const kind: Failure['kind'] = status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : status === 409 ? 'CONFLICT' : status === 400 || status === 422 ? 'VALIDATION' : 'PROVIDER_REJECTION';
      return this.failure('rejected', operation, kind, `Hiver rejected the request (HTTP ${status})`, 0, status, response);
    }
    if (Buffer.from(response.body).includes(Buffer.from(key))) return this.failure('invalid', operation, 'PROTOCOL', 'Hiver response reflected the connection credential', 0, status, response);
    return { kind: 'success', response };
  }

  private failure(kind: Exclude<HiverExchange['kind'], 'success'>, operation: string, failureKind: Failure['kind'], message: string, retryAfterMs = 0, status?: number, response?: ProviderResponse): HiverExchange {
    return { kind, failure: { kind: failureKind, provider: 'hiver', operation, message }, retryAfterMs, ...(status !== undefined ? { status } : {}), ...(response ? { response } : {}) };
  }
}

function wait(milliseconds: number, signal: AbortSignal): Promise<boolean> {
  return new Promise(resolve => {
    if (signal.aborted) { resolve(false); return; }
    const timer = setTimeout(() => { signal.removeEventListener('abort', abort); resolve(true); }, milliseconds);
    const abort = () => { clearTimeout(timer); resolve(false); };
    signal.addEventListener('abort', abort, { once: true });
  });
}
function encodeMultipart(fields: ReadonlyArray<readonly [string, string]>): { body: Uint8Array; contentType: string } {
  const boundary = `dex-hiver-${crypto.randomUUID()}`;
  const chunks = fields.map(([name, value]) => `--${boundary}\r\nContent-Disposition: form-data; name="${name}"\r\n\r\n${value}\r\n`);
  return { body: Buffer.from(chunks.join('') + `--${boundary}--\r\n`), contentType: `multipart/form-data; boundary=${boundary}` };
}
