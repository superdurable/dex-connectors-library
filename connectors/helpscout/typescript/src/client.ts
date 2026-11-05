// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { resolveCredential, resolveCredentialAfterRejection, ReauthorizationRequiredError, type Call, type Failure, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { BodyTooLargeError, MaxErrorBodyBytes, ProviderTransportError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { Config, CredentialSource, Credentials } from './generated/models.js';
import { SearchConversationsOperation, GetConversationOperation, ReplyToConversationOperation, UpdateConversationOperation, FindCustomerByEmailOperation } from './operations.js';

const apiBaseURL = 'https://api.helpscout.net';
const defaultResponseBytes = 4_194_304;

/** Replace only the network transport and clock for deterministic provider tests. */
export interface HelpScoutClientOptions {
  /** Caller-owned HTTP sender; production uses the connector SDK sender. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Clock for receipts and provider Retry-After headers. */ now?: () => Date;
}

/** One response or sanitized transport failure from the Help Scout API. */
export type HelpScoutExchange =
  | { response: ProviderResponse; accessToken: string; isTooLarge?: boolean; isMalformed?: boolean }
  | { error: unknown };

/** Resolve the renewable credential before a provider request. */
export type CredentialResolution =
  | { kind: 'resolved'; credentials: Credentials; token: string }
  | { kind: 'terminal'; message: string }
  | { kind: 'retry'; message: string };

/** One Help Scout connection and its bounded provider transport. */
export class HelpScoutClient {
  readonly #credentials: CredentialSource;
  readonly #refreshDriver = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'Help Scout', auth: Authorization, credentialFields: CredentialFields });
  readonly #send: (request: ProviderRequest) => Promise<ProviderResponse>;
  readonly #now: () => Date;
  readonly #maxResponseBytes: number;

  /** Validate static settings and bind host-owned credentials. */
  constructor(config: Config, credentials: CredentialSource, options: HelpScoutClientOptions = {}) {
    if (!credentials) throw new TypeError('Help Scout credential provider is required');
    const limit = config.maxResponseBytes || defaultResponseBytes;
    if (!Number.isSafeInteger(limit) || limit < 1 || (config.webhookMaxBodyBytes !== undefined && (!Number.isSafeInteger(config.webhookMaxBodyBytes) || config.webhookMaxBodyBytes < 1))) throw new TypeError('Help Scout response limits must be positive');
    this.#credentials = credentials;
    this.#send = options.send ?? sendProviderRequest;
    this.#now = options.now ?? (() => new Date());
    this.#maxResponseBytes = limit;
  }

  /** Bind the search query. */ searchConversations() { return new SearchConversationsOperation(this); }
  /** Bind the conversation read query. */ getConversation() { return new GetConversationOperation(this); }
  /** Bind the fenced reply mutation. */ replyToConversation() { return new ReplyToConversationOperation(this); }
  /** Bind the repeat-safe update mutation. */ updateConversation() { return new UpdateConversationOperation(this); }
  /** Bind the customer lookup query. */ findCustomerByEmail() { return new FindCustomerByEmailOperation(this); }

  /** Obtain or refresh the access token without exposing credential material in failures. */
  async resolveCredentials(call: Call): Promise<CredentialResolution> {
    try {
      const credentials = await resolveCredential(this.#credentials, call, this.#refreshDriver, call.context.cancellationSignal);
      const token = credentials.accessToken?.reveal() ?? '';
      if (!isHeaderSafeCredential(token)) return { kind: 'terminal', message: 'the Help Scout access token is blank or contains characters a header cannot carry' };
      return { kind: 'resolved', credentials, token };
    } catch (error) {
      if (error instanceof ReauthorizationRequiredError) return { kind: 'terminal', message: "Help Scout rejected the App ID or App Secret; save the app's current credentials in Dex Web Connections" };
      return { kind: 'retry', message: 'the Help Scout access token could not be loaded or obtained yet' };
    }
  }

  /** Send a request once, refreshing a rejected access token only when the host supports it. */
  async exchange(call: Call, method: string, path: string, query?: Readonly<Record<string, string>>, payload?: unknown, allowRefresh = true): Promise<HelpScoutExchange | CredentialResolution> {
    const credential = await this.resolveCredentials(call);
    if (credential.kind !== 'resolved') return credential;
    return this.exchangeWithToken(call, credential.token, method, path, query, payload, allowRefresh);
  }

  /** Send with a token already resolved for a multi-request operation. */
  async exchangeWithToken(call: Call, accessToken: string, method: string, path: string, query?: Readonly<Record<string, string>>, payload?: unknown, allowRefresh = true): Promise<HelpScoutExchange> {
    const encodedQuery = query ? Object.entries(query).sort(([left], [right]) => left < right ? -1 : left > right ? 1 : 0).map(([name, value]) => `${goQueryEscape(name)}=${goQueryEscape(value)}`).join('&') : '';
    const url = `${apiBaseURL}${path}${encodedQuery ? `?${encodedQuery}` : ''}`;
    let body: Uint8Array | undefined;
    try { if (payload !== undefined) body = Buffer.from(JSON.stringify(payload)); }
    catch { return { error: new TypeError('Help Scout request could not be built') }; }
    let token = accessToken;
    for (let attempt = 0; attempt < 2; attempt++) {
      const request: ProviderRequest = {
        url, method, headers: { Authorization: `Bearer ${token}`, Accept: 'application/json', ...(body ? { 'Content-Type': 'application/json; charset=UTF-8' } : {}) },
        ...(body ? { body } : {}), maxResponseBytes: this.#maxResponseBytes, maxErrorResponseBytes: MaxErrorBodyBytes,
        ignoreErrorBodyReadFailure: true, addressPolicy: 'publicHTTPS', timeoutMs: 25_000,
        signal: call.context.cancellationSignal,
      };
      let exchange: { response: ProviderResponse; accessToken: string; isTooLarge?: boolean; isMalformed?: boolean };
      try { exchange = { response: await this.#send(request), accessToken: token }; }
      catch (error) {
        if (error instanceof BodyTooLargeError && error.status !== undefined && error.headers) exchange = { response: { status: error.status, headers: error.headers, body: new Uint8Array() }, accessToken: token, ...(error.status >= 200 && error.status < 300 ? { isTooLarge: true as const } : {}) };
        else return { error };
      }
      if (exchange.response.status >= 200 && exchange.response.status < 300 && token && Buffer.from(exchange.response.body).includes(Buffer.from(token))) return { ...exchange, isMalformed: true };
      if (exchange.response.status !== 401 || !allowRefresh || attempt > 0 || !('resolveAfterRejection' in this.#credentials)) return exchange;
      try {
        const replacement = await resolveCredentialAfterRejection(this.#credentials, call, this.#refreshDriver, call.context.cancellationSignal);
        token = replacement.accessToken?.reveal() ?? '';
        if (!isHeaderSafeCredential(token)) return exchange;
      } catch { return exchange; }
    }
    return { error: new TypeError('Help Scout authenticated request retry was exhausted') };
  }

  /** Capture a provider object ID without retaining response or credential bodies. */
  receipt(call: Call, objectId = 0, requestId = ''): ReceiptDraft {
    return { callId: call.id, idempotencyKey: call.idempotencyKey ?? '', provider: 'helpscout', providerRequestId: requestId, observedAt: this.#now().toISOString(), ...(objectId > 0 ? { providerObjectId: String(objectId) } : {}) };
  }

  /** Classify one non-successful HTTP response like the Go connector. */
  classifyFailure(operation: string, response: ProviderResponse, accessToken: string): { isRetry: boolean; retryAfterMs: number; failure: Failure; logRef: string } {
    const status = response.status;
    const summary = summarizeError(response.body, accessToken);
    let kind: Failure['kind'];
    let message: string;
    let isRetry = false;
    let retryAfterMs = 0;
    if (status === 429) {
      kind = 'RATE_LIMIT'; message = 'Help Scout rate limited the request (HTTP 429)'; isRetry = true;
      const seconds = Number(response.headers['x-ratelimit-retry-after']);
      retryAfterMs = Number.isSafeInteger(seconds) && seconds > 0 ? Math.min(seconds * 1000, 3_600_000) : parseRetryAfter(String(response.headers['retry-after'] ?? ''), this.#now());
    } else if (status === 408 || status >= 500) {
      kind = 'AVAILABILITY'; message = `Help Scout could not complete the request yet (HTTP ${status})`; isRetry = true;
    } else if (status === 401) {
      kind = 'AUTHENTICATION'; message = "Help Scout rejected the access token (HTTP 401); check that the app's owner is an active Help Scout user";
    } else if (status === 403) {
      kind = 'AUTHORIZATION'; message = `Help Scout denied access (HTTP 403), for example because the API is not enabled for the account, payment is missing, or the user cannot access the inbox${summary.fields}`;
    } else if (status === 404 || status === 410) {
      kind = 'NOT_FOUND'; message = `Help Scout found no such resource (HTTP ${status})`;
    } else if (status >= 300 && status < 400) {
      kind = 'PROTOCOL'; message = `Help Scout redirected the request (HTTP ${status}), which the connector never follows`;
    } else if (status === 409) {
      kind = 'CONFLICT'; message = `Help Scout reported a conflicting resource (HTTP 409)${summary.fields}`;
    } else if (status === 412 || status === 423) {
      kind = 'PROVIDER_REJECTION'; message = `Help Scout refused to change a locked conversation or customer (HTTP ${status}), such as one with 100 threads or one too old to update${summary.fields}`;
    } else if ([400, 422, 413, 415].includes(status)) {
      kind = 'VALIDATION'; message = `Help Scout rejected the request (HTTP ${status})${summary.fields}`;
    } else {
      kind = 'PROVIDER_REJECTION'; message = `Help Scout rejected the request (HTTP ${status})${summary.fields}`;
    }
    return { isRetry, retryAfterMs, failure: { kind, provider: 'helpscout', operation, message }, logRef: summary.logRef };
  }
}

function summarizeError(body: Uint8Array, accessToken: string): { logRef: string; fields: string } {
  try {
    const parsed: unknown = JSON.parse(Buffer.from(body).toString('utf8'));
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return { logRef: '', fields: '' };
    const document = parsed as Record<string, unknown>;
    const logRef = typeof document.logRef === 'string' && /^[A-Za-z0-9-]{1,64}$/.test(document.logRef) && (!accessToken || !document.logRef.includes(accessToken)) ? document.logRef : '';
    const embedded = document._embedded;
    const errors = embedded && typeof embedded === 'object' && !Array.isArray(embedded) ? (embedded as Record<string, unknown>).errors : undefined;
    const fields: string[] = [];
    if (Array.isArray(errors)) for (const raw of errors.slice(0, 5)) {
      if (!raw || typeof raw !== 'object' || Array.isArray(raw)) continue;
      const entry = raw as Record<string, unknown>;
      if (typeof entry.path !== 'string' || !/^[A-Za-z][A-Za-z0-9_.[\]]{0,63}$/.test(entry.path) || accessToken && entry.path.includes(accessToken)) continue;
      fields.push(entry.path);
    }
    return { logRef, fields: fields.length ? ` [${fields.join(', ')}]` : '' };
  } catch { return { logRef: '', fields: '' }; }
}

/** Detect a provably unsent transport failure for a fenced reply. */
export function isProvablyUnsent(error: unknown): boolean { return error instanceof ProviderTransportError && error.isProvablyNotSent; }

function goQueryEscape(value: string): string {
  return encodeURIComponent(value).replace(/[!'()*~]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`).replace(/%20/g, '+');
}
