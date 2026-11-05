// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { isIP } from 'node:net';
import { AddressRefusal, BodyTooLargeError, ProviderTransportError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { ReauthorizationRequiredError, resolveCredential, resolveCredentialAfterRejection, type Call, type Failure, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { Config, CredentialSource, Credentials } from './generated/models.js';
import { CreateObjectOperation, GetObjectOperation, ListObjectsOperation, SearchObjectsOperation, UpdateObjectOperation, UpsertObjectOperation } from './operations.js';

const defaultEndpoint = 'https://api.pipedrive.com';
const defaultMaxResponseBytes = 1_048_576;
const defaultTimeoutMs = 20_000;
const operationDeadlineMs = 25_000;

/** One classified Pipedrive exchange, with secret-free failure information. */
export interface PipedriveExchange {
  kind: 'success' | 'notSent' | 'rateLimited' | 'unavailable' | 'invalid' | 'notFound' | 'rejected' | 'defect';
  response?: ProviderResponse;
  failure?: Failure;
  retryAfterMs: number;
}

/** Credentials and company endpoint selected for one attempt. */
export interface PipedriveSession { credentials: Credentials; endpoint: string; token: string; signal: AbortSignal; deadlineMs: number; }

/** Classifies credential resolution before any provider request. */
export type PipedriveSessionResult = { kind: 'resolved'; session: PipedriveSession } | { kind: 'retry' | 'rejected' | 'defect'; failure: Failure };

/** Optional sender and receipt clock for provider replay. */
export interface PipedriveClientOptions {
  /** Replace production HTTP transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Replace the receipt and Retry-After clock. */ now?: () => Date;
}

/** Authenticated Pipedrive API client. */
export class PipedriveClient {
  private readonly endpoint: string;
  private readonly maxResponseBytes: number;
  private readonly timeoutMs: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;
  private readonly refreshDriver = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'Pipedrive', auth: Authorization, credentialFields: CredentialFields });

  /** Validate configuration and bind host-owned credentials. */
  constructor(config: Config & { timeout?: number }, private readonly credentials: CredentialSource, options: PipedriveClientOptions = {}) {
    if (!credentials) throw new TypeError('Pipedrive credential source is required');
    this.endpoint = validatePipedriveEndpoint(config.endpoint || defaultEndpoint);
    this.maxResponseBytes = config.maxResponseBytes || defaultMaxResponseBytes;
    this.timeoutMs = config.timeout || defaultTimeoutMs;
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes < 1) throw new TypeError('Pipedrive maxResponseBytes must be positive');
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs < 1) throw new TypeError('Pipedrive timeout must be positive');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the search Query. */ searchObjects(): SearchObjectsOperation { return new SearchObjectsOperation(this); }
  /** Bind the record read Query. */ getObject(): GetObjectOperation { return new GetObjectOperation(this); }
  /** Bind the bounded list Query. */ listObjects(): ListObjectsOperation { return new ListObjectsOperation(this); }
  /** Bind the identity upsert Mutation. */ upsertObject(): UpsertObjectOperation { return new UpsertObjectOperation(this); }
  /** Bind the checkpointed create Mutation. */ createObject(): CreateObjectOperation { return new CreateObjectOperation(this); }
  /** Bind the field update Mutation. */ updateObject(): UpdateObjectOperation { return new UpdateObjectOperation(this); }

  /** Resolve a selected authorization method through the generated OAuth driver. */
  async startSession(call: Call, operation: string): Promise<PipedriveSessionResult> {
    const deadlineMs = Math.min(Date.now() + operationDeadlineMs, (call.context.deadline?.getTime() ?? Number.POSITIVE_INFINITY) - 100);
    const signal = AbortSignal.any([call.context.cancellationSignal, AbortSignal.timeout(Math.max(0, deadlineMs - Date.now()))]);
    let credentials: Credentials;
    try { credentials = await resolveCredential(this.credentials, call, this.refreshDriver, signal); }
    catch (error) {
      if (signal.aborted) return { kind: 'retry', failure: this.failure(operation, 'AVAILABILITY', 'Pipedrive connection credentials are temporarily unavailable') };
      if (error instanceof ReauthorizationRequiredError) return { kind: 'rejected', failure: this.failure(operation, 'AUTHENTICATION', 'Pipedrive authorization must be renewed; reconnect the connection') };
      if (error instanceof Error && error.message === 'connection is not configured') return { kind: 'defect', failure: this.failure(operation, 'AUTHENTICATION', 'Pipedrive connection credentials are unavailable; reconnect the connection') };
      return { kind: 'retry', failure: this.failure(operation, 'AVAILABILITY', 'Pipedrive connection credentials are temporarily unavailable') };
    }
    if (signal.aborted) return { kind: 'retry', failure: this.failure(operation, 'AVAILABILITY', 'Pipedrive connection credentials are temporarily unavailable') };
    const method = credentials.authMethodId;
    const token = method === 'pipedrive-oauth' ? credentials.accessToken?.reveal() : credentials.apiToken?.reveal();
    if (!token || !isHeaderSafeCredential(token) || !method || !['api-token', 'pipedrive-oauth'].includes(method)) return { kind: 'defect', failure: this.failure(operation, 'AUTHENTICATION', 'Pipedrive connection credentials are unavailable; reconnect the connection') };
    let endpoint = this.endpoint;
    if (method === 'pipedrive-oauth') {
      try { endpoint = validatePipedriveEndpoint(credentials.apiDomain || ''); }
      catch { return { kind: 'defect', failure: this.failure(operation, 'AUTHENTICATION', 'Pipedrive OAuth connection has no valid api_domain; reconnect the connection') }; }
    }
    return { kind: 'resolved', session: { credentials, endpoint, token, signal, deadlineMs } };
  }

  /** Refresh after one OAuth 401, leaving generated refresh mechanics in the SDK. */
  async refreshAfterRejection(call: Call, session: PipedriveSession): Promise<boolean> {
    if (session.credentials.authMethodId !== 'pipedrive-oauth') return false;
    try {
      const replacement = await resolveCredentialAfterRejection(this.credentials, call, this.refreshDriver, session.signal);
      const token = replacement.accessToken?.reveal();
      if (replacement.authMethodId !== 'pipedrive-oauth' || !token || !isHeaderSafeCredential(token) || token === session.token) return false;
      session.endpoint = validatePipedriveEndpoint(replacement.apiDomain || '');
      session.credentials = replacement;
      session.token = token;
      return true;
    } catch { return false; }
  }

  /** Send one API request; a 401 may refresh and resend only outside a fenced create. */
  async exchange(call: Call, operation: string, session: PipedriveSession, method: string, path: string, query?: URLSearchParams, body?: unknown, signal?: AbortSignal, isSingleDispatch = false): Promise<PipedriveExchange> {
    const first = await this.exchangeOnce(operation, session, method, path, query, body, signal, isSingleDispatch);
    if (isSingleDispatch || first.response?.status !== 401 || session.credentials.authMethodId !== 'pipedrive-oauth' || !await this.refreshAfterRejection(call, session)) return first;
    return this.exchangeOnce(operation, session, method, path, query, body, signal, isSingleDispatch);
  }

  private async exchangeOnce(operation: string, session: PipedriveSession, method: string, path: string, query?: URLSearchParams, body?: unknown, signal?: AbortSignal, isSingleDispatch = false): Promise<PipedriveExchange> {
    if (session.signal.aborted || signal?.aborted) return this.failed('notSent', operation, 'TRANSPORT', 'the operation deadline passed before Pipedrive was contacted; no request was sent');
    const queryString = query ? encodeGoQuery(query) : '';
    const url = session.endpoint + path + (queryString ? `?${queryString}` : '');
    const headers: Record<string, string> = { Accept: 'application/json' };
    if (session.credentials.authMethodId === 'pipedrive-oauth') headers.Authorization = `Bearer ${session.token}`;
    else headers['X-Api-Token'] = session.token;
    let encoded: Uint8Array | undefined;
    if (body !== undefined) { encoded = Buffer.from(JSON.stringify(body)); headers['Content-Type'] = 'application/json'; }
    const request: ProviderRequest = {
      url, method, headers, ...(encoded ? { body: encoded } : {}),
      addressPolicy: isLoopbackEndpoint(session.endpoint) ? 'privateNetworkHTTP' : 'publicHTTPS',
      maxResponseBytes: this.maxResponseBytes, maxErrorResponseBytes: 65_536,
      timeoutMs: Math.max(1, Math.min(this.timeoutMs, session.deadlineMs - Date.now())), signal: signal ? AbortSignal.any([session.signal, signal]) : session.signal,
    };
    // The fenced write uses the SDK fence's cancellation signal and one-shot transport.
    void isSingleDispatch;
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined && error.headers) {
        response = { status: error.status, headers: error.headers, body: new Uint8Array() };
        if (response.status >= 200 && response.status < 300) return { ...this.failed('invalid', operation, 'RESPONSE_TOO_LARGE', 'Pipedrive response exceeds the configured maxResponseBytes limit'), response };
      } else if (error instanceof AddressRefusal || error instanceof ProviderTransportError && error.isProvablyNotSent || error instanceof Error && 'code' in error && error.code === 'ECONNREFUSED') {
        return this.failed('notSent', operation, 'TRANSPORT', 'Pipedrive could not be reached; no request was sent');
      } else return this.failed('unavailable', operation, 'TRANSPORT', 'Pipedrive request failed before a response arrived');
    }
    if (response.status >= 200 && response.status < 300) {
      if (Buffer.from(response.body).includes(Buffer.from(session.token))) return { ...this.failed('invalid', operation, 'PROTOCOL', 'Pipedrive response reflected the connection credential'), response };
      return { kind: 'success', response, retryAfterMs: 0 };
    }
    return this.classifyError(operation, response);
  }

  /** Construct a secret-free receipt with Pipedrive's safe correlation ID. */
  receipt(call: Call, response?: ProviderResponse, objectId = ''): ReceiptDraft {
    const correlation = response?.headers['x-correlation-id'];
    const correlationId = Array.isArray(correlation) ? correlation[0] : correlation;
    return { callId: call.id, provider: 'pipedrive', observedAt: this.now().toISOString(), ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}), ...(objectId ? { providerObjectId: objectId } : {}), ...(correlationId && /^[A-Za-z0-9-]{1,64}$/.test(correlationId) ? { providerRequestId: correlationId } : {}) };
  }

  /** Build a provider-specific safe failure. */
  failure(operation: string, kind: Failure['kind'], message: string): Failure { return { kind, provider: 'pipedrive', operation, message }; }

  private failed(kind: PipedriveExchange['kind'], operation: string, failureKind: Failure['kind'], message: string, retryAfterMs = 0): PipedriveExchange { return { kind, failure: this.failure(operation, failureKind, message), retryAfterMs }; }

  private classifyError(operation: string, response: ProviderResponse): PipedriveExchange {
    const status = response.status;
    const header = (name: string) => { const value = response.headers[name]; return (Array.isArray(value) ? value[0] : value) ?? ''; };
    const retryAfter = parseRetryAfter(header('retry-after'), this.now());
    let result: PipedriveExchange;
    if (status === 429 && isExhaustedDailyBudget(header('x-daily-ratelimit-token-remaining'))) result = this.failed('rejected', operation, 'QUOTA_EXHAUSTED', "Pipedrive's daily API token budget is exhausted until it resets at midnight in Pipedrive's server time zone");
    else if (status === 429) {
      const reset = Number(header('x-ratelimit-reset'));
      const burst = Number.isFinite(reset) && reset > 0 ? Math.min(Math.max(reset * 1000, 2000), 10000) : 2000;
      result = this.failed('rateLimited', operation, 'RATE_LIMIT', "Pipedrive's burst rate limit rejected the request", Math.trunc(Math.max(retryAfter, burst)));
    } else if (status === 409 || status === 408 || status >= 500 && status !== 501) result = this.failed('unavailable', operation, 'AVAILABILITY', `Pipedrive is temporarily unavailable (HTTP ${status})`, retryAfter);
    else if (status === 401) result = this.failed('rejected', operation, 'AUTHENTICATION', 'Pipedrive rejected the API token or access token (HTTP 401)');
    else if (status === 402) result = this.failed('rejected', operation, 'PROVIDER_REJECTION', 'Pipedrive requires a payment or plan change for the company account (HTTP 402)');
    else if (status === 403) result = this.failed('rejected', operation, 'AUTHORIZATION', 'Pipedrive denied the request, such as a missing scope or permission, or blocked API traffic (HTTP 403)');
    else if (status === 404) result = this.failed('notFound', operation, 'NOT_FOUND', 'Pipedrive found no such record (HTTP 404)');
    else if (status === 400 || status === 422) result = this.failed('rejected', operation, 'VALIDATION', `Pipedrive rejected the request as invalid (HTTP ${status})`);
    else if (status >= 300 && status < 400) result = this.failed('rejected', operation, 'PROTOCOL', `Pipedrive redirected the request (HTTP ${status})`);
    else result = this.failed('rejected', operation, 'PROVIDER_REJECTION', `Pipedrive rejected the request (HTTP ${status})`);
    return { ...result, response };
  }
}

/** Accept only Pipedrive HTTPS origins and loopback origins used in tests. */
export function validatePipedriveEndpoint(value: string): string {
  const base = validateBaseURL(value);
  const parsed = new URL(base);
  if (parsed.pathname !== '/' || parsed.search || parsed.hash) throw new TypeError('Pipedrive base URL must not contain a path');
  const host = parsed.hostname.toLowerCase().replace(/^\[|\]$/g, '');
  const isLoopback = host === 'localhost' || host === '::1' || isIP(host) === 4 && host.startsWith('127.');
  if (!isLoopback && !(parsed.protocol === 'https:' && (host === 'pipedrive.com' || host.endsWith('.pipedrive.com')))) throw new TypeError('Pipedrive base URL must be a pipedrive.com host');
  return base;
}

function isLoopbackEndpoint(endpoint: string): boolean {
  const host = new URL(endpoint).hostname.toLowerCase().replace(/^\[|\]$/g, '');
  return host === 'localhost' || host === '::1' || isIP(host) === 4 && host.startsWith('127.');
}

function isExhaustedDailyBudget(value: string): boolean {
  const trimmed = value.trim();
  if (!/^[+-]?\d+$/.test(trimmed)) return false;
  const amount = BigInt(trimmed);
  return amount >= -9_223_372_036_854_775_808n && amount <= 0n;
}

/** Encode query pairs like Go url.Values.Encode, preserving repeated values. */
export function encodeGoQuery(query: URLSearchParams): string {
  const escape = (value: string) => encodeURIComponent(value).replace(/%20/g, '+').replace(/[!'()*]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`);
  return [...query].sort(([left], [right]) => Buffer.compare(Buffer.from(left), Buffer.from(right))).map(([key, value]) => `${escape(key)}=${escape(value)}`).join('&');
}
