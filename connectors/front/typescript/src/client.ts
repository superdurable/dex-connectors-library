// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, ProviderTransportError, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ReauthorizationRequiredError, SecretString, resolveCredentialAfterRejection, type Call, type CredentialProvider, type Failure, type ReceiptDraft } from '@superdurable/connectors-sdk';
import type { Config, Credentials } from './generated/models.js';
import { SearchConversationsOperation, GetConversationOperation, ReplyToConversationOperation, UpdateConversationOperation, FindContactByEmailOperation } from './operations.js';

const defaultEndpoint = 'https://api2.frontapp.com';

/** One Front request, with an already escaped path. */
export interface FrontRequest {
  /** HTTP method sent to the provider. */ method: string;
  /** Escaped path below the configured Front API host. */ path: string;
  /** Optional query parameters. */ query?: URLSearchParams;
  /** Optional JSON request body. */ payload?: unknown;
  /** Cancellation for a fenced write. */ signal?: AbortSignal;
}
/** Classified Front response. */
export interface FrontExchange {
  /** Provider outcome before operation-specific branch mapping. */ outcome: 'succeeded' | 'merged' | 'notFound' | 'notSent' | 'rateLimited' | 'unconfirmed' | 'rejected' | 'invalid' | 'defect';
  /** Bounded HTTP response, when one arrived. */ response?: ProviderResponse;
  /** Secret-safe failure for unsuccessful exchanges. */ failure?: Failure;
  /** Provider-directed delay for a safe retry, in milliseconds. */ retryAfterMs: number;
}
/** Transport replacement for deterministic provider tests. */
export type FrontRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Client dependencies for tests. */
export interface FrontClientOptions {
  /** Sender; production uses the SDK's bounded, no-redirect transport. */ send?: FrontRequestSender;
  /** Clock used for receipts and rate-limit calculations. */ now?: () => Date;
  /** Front-compatible URL for local provider tests; production leaves it unset. */ apiBaseURL?: string;
}

/** Front Core API client for one named connection. */
export class FrontClient {
  /** Validated Front API base URL. */ readonly endpoint: string;
  private readonly maxResponseBytes: number;
  private readonly credentials: CredentialProvider<Credentials>;
  private readonly send: FrontRequestSender;
  private readonly now: () => Date;

  /** Construct a bounded client without contacting Front. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: FrontClientOptions = {}) {
    if (!credentials) throw new TypeError('Front credential provider is required');
    this.endpoint = validateBaseURL(options.apiBaseURL ?? defaultEndpoint);
    this.maxResponseBytes = config.maxResponseBytes ?? 4_194_304;
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes < 1) throw new TypeError('Front maxResponseBytes must be positive');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the search query. */ searchConversations() { return new SearchConversationsOperation(this); }
  /** Bind the conversation query. */ getConversation() { return new GetConversationOperation(this); }
  /** Bind the fenced reply mutation. */ replyToConversation() { return new ReplyToConversationOperation(this); }
  /** Bind the repeat-safe update mutation. */ updateConversation() { return new UpdateConversationOperation(this); }
  /** Bind the contact query. */ findContactByEmail() { return new FindContactByEmailOperation(this); }

  /** Resolve the current token before each logical operation. */
  async resolveToken(call: Call): Promise<{ token: string } | { failure: Failure; terminal: boolean }> {
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      if (!(credentials.apiToken instanceof SecretString)) throw new TypeError('invalid Front token');
      const token = credentials.apiToken.reveal();
      if (!isHeaderSafeCredential(token)) throw new TypeError('invalid Front token');
      return { token };
    } catch (error) {
      return { failure: this.failure('AUTHENTICATION', call.operation.operationId, 'the Front API token is unavailable, blank, or contains characters a header cannot carry'), terminal: error instanceof ReauthorizationRequiredError || error instanceof TypeError };
    }
  }

  /** Ask a refresh-capable credential source for a replacement after HTTP 401. */
  async replacementAfterRejection(call: Call, rejectedToken: string): Promise<'replacement' | 'reconnect' | 'temporarilyUnavailable'> {
    if (!('resolveAfterRejection' in this.credentials) || typeof this.credentials.resolveAfterRejection !== 'function') return 'reconnect';
    try {
      const replacement = await resolveCredentialAfterRejection(this.credentials, call, {
        refreshRequired: () => false,
        refresh: async () => { throw new ReauthorizationRequiredError(); },
      }, call.context.cancellationSignal);
      if (replacement.apiToken instanceof SecretString) {
        const token = replacement.apiToken.reveal();
        if (isHeaderSafeCredential(token) && token !== rejectedToken) return 'replacement';
      }
      return 'reconnect';
    } catch (error) {
      return error instanceof ReauthorizationRequiredError ? 'reconnect' : 'temporarilyUnavailable';
    }
  }

  /** Send and classify one provider request. */
  async exchange(call: Call, token: string, operation: string, request: FrontRequest): Promise<FrontExchange> {
    let body: Uint8Array | undefined;
    try { if (request.payload !== undefined) body = Buffer.from(JSON.stringify(request.payload)); }
    catch { return { outcome: 'defect', failure: this.failure('LOCAL_DEFECT', operation, 'Front request could not be built'), retryAfterMs: 0 }; }
    const query = request.query?.toString();
    const url = this.endpoint + request.path + (query ? `?${query}` : '');
    let response: ProviderResponse;
    try {
      response = await this.send({ url, method: request.method,
        headers: { Authorization: `Bearer ${token}`, Accept: 'application/json', ...(body ? { 'Content-Type': 'application/json' } : {}) },
        ...(body ? { body } : {}), maxResponseBytes: Math.max(this.maxResponseBytes, 65_536),
        addressPolicy: this.endpoint === defaultEndpoint ? 'publicHTTPS' : 'privateNetworkHTTP',
        timeoutMs: 25_000, signal: request.signal ?? call.context.cancellationSignal });
    } catch (error) {
      if (error instanceof BodyTooLargeError) {
        if (error.status && (error.status < 200 || error.status >= 300)) return this.classifyFailureStatus(error.status, error.headers ?? {}, operation);
        return { outcome: 'invalid', ...(error.status ? { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() } } : {}), failure: this.failure('RESPONSE_TOO_LARGE', operation, 'the Front response exceeds the configured maxResponseBytes'), retryAfterMs: 0 };
      }
      return { outcome: error instanceof ProviderTransportError && error.isProvablyNotSent ? 'notSent' : 'unconfirmed', failure: this.failure('TRANSPORT', operation, error instanceof ProviderTransportError && error.isProvablyNotSent ? 'Front could not be reached; no request was sent' : 'the Front request may have been sent, but no answer arrived'), retryAfterMs: 0 };
    }
    const status = response.status;
    if (status >= 200 && status < 300) {
      if (response.body.length > this.maxResponseBytes) return { outcome: 'invalid', response, failure: this.failure('RESPONSE_TOO_LARGE', operation, 'the Front response exceeds the configured maxResponseBytes'), retryAfterMs: 0 };
      if (Buffer.from(response.body).includes(Buffer.from(token))) return { outcome: 'invalid', response, failure: this.failure('PROTOCOL', operation, 'the Front response reflected the API token'), retryAfterMs: 0 };
      return { outcome: 'succeeded', response, retryAfterMs: 0 };
    }
    return this.classifyFailureStatus(status, response.headers, operation, response);
  }

  private classifyFailureStatus(status: number, headers: ProviderResponse['headers'], operation: string, response?: ProviderResponse): FrontExchange {
    let outcome: FrontExchange['outcome'] = 'rejected';
    let kind: Failure['kind'] = 'PROVIDER_REJECTION';
    let message = `Front rejected the request (HTTP ${status})`;
    let retryAfterMs = 0;
    if (status === 429) { outcome = 'rateLimited'; kind = 'RATE_LIMIT'; message = 'Front rate limited the request (HTTP 429)'; retryAfterMs = this.rateLimitDelay(headers); }
    else if (status === 408 || status >= 500) { outcome = 'unconfirmed'; kind = 'AVAILABILITY'; message = `Front could not complete the request (HTTP ${status})`; }
    else if (status === 301) { outcome = 'merged'; kind = 'NOT_FOUND'; message = 'Front reports that the conversation was merged into another (HTTP 301)'; }
    else if (status === 404 || status === 410) { outcome = 'notFound'; kind = 'NOT_FOUND'; message = `Front found no such resource (HTTP ${status})`; }
    else if (status >= 300 && status < 400) { kind = 'PROTOCOL'; message = `Front redirected the request (HTTP ${status}), which the connector never follows`; }
    else if (status === 401) { kind = 'AUTHENTICATION'; message = 'Front rejected the API token (HTTP 401); check that the token still exists'; }
    else if (status === 403) { kind = 'AUTHORIZATION'; message = "Front denied access (HTTP 403); check the token's namespaces and its read, write, or send permission for this resource"; }
    else if ([400, 413, 422].includes(status)) kind = 'VALIDATION';
    else if (status === 409) kind = 'CONFLICT';
    return { outcome, ...(response ? { response } : {}), failure: this.failure(kind, operation, message), retryAfterMs };
  }

  /** Return only public correlation fields. */
  receipt(call: Call, providerObjectId = ''): ReceiptDraft { return { callId: call.id, provider: 'front', observedAt: this.now().toISOString(), ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}), ...(providerObjectId ? { providerObjectId } : {}) }; }
  /** Construct a secret-safe failure. */
  failure(kind: Failure['kind'], operation: string, message: string): Failure { return { kind, provider: 'front', operation, message }; }
  /** Return the page token from a validated next link. */
  nextPageToken(next: unknown, pathPrefix: string): string {
    if (next == null || next === '') return '';
    if (typeof next !== 'string') throw new Error('the next-page link is not a plain URL');
    const parsed = new URL(next);
    const base = new URL(this.endpoint);
    if (parsed.username || parsed.password || parsed.hash || !((parsed.protocol === 'https:' && (parsed.host === 'api2.frontapp.com' || /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.api\.frontapp\.com$/.test(parsed.host))) || (parsed.protocol === base.protocol && parsed.host === base.host))) throw new Error("the next-page link points outside Front's API hosts");
    if (!parsed.pathname.startsWith(pathPrefix)) throw new Error('the next-page link names another resource');
    const token = parsed.searchParams.get('page_token') ?? '';
    if (!/^[A-Za-z0-9._~-]{1,1000}$/.test(token)) throw new Error('the next-page link has no valid page_token');
    return token;
  }

  private rateLimitDelay(headers: ProviderResponse['headers']): number {
    const value = headers['retry-after'];
    const delay = parseRetryAfter(Array.isArray(value) ? value[0] ?? '' : value ?? '', this.now());
    if (delay > 0) return delay;
    const reset = Number(headers['x-ratelimit-reset']);
    return Number.isSafeInteger(reset) && reset > 0 ? Math.min(Math.max(reset * 1000 - this.now().getTime(), 1000), 60_000) : 0;
  }
}
