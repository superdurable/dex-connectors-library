// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { AddressRefusal, BodyTooLargeError, MaxErrorBodyBytes, ProviderTransportError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { ReauthorizationRequiredError, resolveCredential, resolveCredentialAfterRejection, type Call, type Failure, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { Config, CredentialSource, Credentials } from './generated/models.js';
import { SearchTicketsOperation, GetTicketOperation, CreateTicketOperation, UpdateTicketOperation, AddCommentOperation } from './operations.js';

const dataCenterHosts: Record<string, string> = {
  'zoho-us-oauth': 'desk.zoho.com', 'zoho-eu-oauth': 'desk.zoho.eu', 'zoho-in-oauth': 'desk.zoho.in',
  'zoho-au-oauth': 'desk.zoho.com.au', 'zoho-jp-oauth': 'desk.zoho.jp', 'zoho-ca-oauth': 'desk.zohocloud.ca',
  'zoho-sa-oauth': 'desk.zoho.sa', 'zoho-sg-oauth': 'desk.zoho.sg', 'zoho-ae-oauth': 'desk.zoho.ae',
};
const defaultMaxResponseBytes = 4_194_304;

/** Network and clock dependencies for deterministic provider tests. */
export interface ZohoDeskClientOptions {
  /** Replace the production bounded HTTP sender. */
  send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Override the Desk API base only for a trusted local provider stand-in. */
  apiBaseURL?: string;
  /** Supply a deterministic instant for receipts and Retry-After. */
  now?: () => Date;
}

/** One classified provider exchange. */
export interface ExchangeFailure {
  /** Go equivalent exchange outcome. */
  disposition: 'notFound' | 'rateLimited' | 'notSent' | 'unavailable' | 'rejected' | 'invalid' | 'defect';
  /** Safe failure without token or provider message text. */
  failure: Failure;
  /** Requested retry delay in milliseconds. */
  retryAfterMs: number;
  /** Response metadata for the receipt. */
  response?: ProviderResponse;
}

/** Complete Zoho Desk response or classified failure. */
export type ExchangeResult = { response: ProviderResponse } | { outcome: ExchangeFailure };

/** Validated Zoho Desk client with generated OAuth refresh behavior. */
export class ZohoDeskClient {
  private readonly orgId: string;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly apiBaseURL?: string;
  private readonly now: () => Date;
  private readonly operationSignals = new WeakMap<Call, AbortSignal>();
  private readonly refreshDriver = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'Zoho', auth: Authorization, credentialFields: CredentialFields });

  /** Validate startup configuration without contacting the provider. */
  constructor(config: Config, private readonly credentials: CredentialSource, options: ZohoDeskClientOptions = {}) {
    if (!credentials) throw new TypeError('Zoho Desk credential provider is required');
    this.orgId = config.orgId?.trim() ?? '';
    if (!/^[0-9]{1,20}$/.test(this.orgId)) throw new TypeError('Zoho Desk orgId must be a numeric organization ID');
    this.maxResponseBytes = config.maxResponseBytes || defaultMaxResponseBytes;
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes < 1) throw new TypeError('Zoho Desk response limit must be positive');
    this.apiBaseURL = options.apiBaseURL ? validateBaseURL(options.apiBaseURL) : undefined;
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind ticket search. */ searchTickets(): SearchTicketsOperation { return new SearchTicketsOperation(this); }
  /** Bind ticket details. */ getTicket(): GetTicketOperation { return new GetTicketOperation(this); }
  /** Bind ticket creation. */ createTicket(): CreateTicketOperation { return new CreateTicketOperation(this); }
  /** Bind ticket update. */ updateTicket(): UpdateTicketOperation { return new UpdateTicketOperation(this); }
  /** Bind comment creation. */ addComment(): AddCommentOperation { return new AddCommentOperation(this); }
  /** Return the instant used for search bounds. */ currentTime(): Date { return this.now(); }

  /** Correlate the call, provider object, and safe credit counts. */
  receipt(call: Call, response?: ProviderResponse, objectId = ''): ReceiptDraft {
    const requestCredits = safeCreditCount(header(response, 'x-rate-limit-request-weight-v3'));
    const remainingCredits = safeCreditCount(header(response, 'x-rate-limit-remaining-v3'));
    return {
      callId: call.id, provider: 'zoho-desk', observedAt: this.now().toISOString(),
      ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}),
      ...(objectId ? { providerObjectId: objectId } : {}),
      ...(requestCredits || remainingCredits ? { metadata: { ...(requestCredits ? { requestCredits } : {}), ...(remainingCredits ? { remainingCredits } : {}) } } : {}),
    };
  }

  /** Resolve an OAuth credential before the first provider request. */
  async startSession(call: Call, operation: string): Promise<{ credentials: Credentials; baseURL: string } | ExchangeFailure> {
    let credentials: Credentials;
    try { credentials = await resolveCredential(this.credentials, call, this.refreshDriver, this.operationSignal(call)); }
    catch (error) {
      if (error instanceof ReauthorizationRequiredError) return this.failure('rejected', operation, 'AUTHENTICATION', 'Zoho authorization is revoked or expired; reconnect the connection');
      return this.failure('unavailable', operation, 'AUTHENTICATION', 'Zoho credentials are temporarily unavailable');
    }
    if (!this.hasValidCredentials(credentials)) return this.failure('defect', operation, 'AUTHENTICATION', 'Zoho Desk connection credentials are missing or invalid');
    const host = dataCenterHosts[credentials.authMethodId || 'zoho-us-oauth'];
    if (!host) return this.failure('defect', operation, 'AUTHENTICATION', 'Zoho Desk connection credentials are missing or invalid');
    return { credentials, baseURL: this.apiBaseURL ?? `https://${host}/api/v1` };
  }

  /** Send one provider exchange and refresh a rejected access token once. */
  async exchange(call: Call, operation: string, session: { credentials: Credentials; baseURL: string }, method: string, path: string, query?: Record<string, string>, payload?: unknown): Promise<ExchangeResult> {
    let encoded: Uint8Array | undefined;
    try { if (payload !== undefined) encoded = Buffer.from(JSON.stringify(payload)); }
    catch { return { outcome: this.failure('defect', operation, 'LOCAL_DEFECT', 'Zoho Desk request could not be built') }; }
    const target = session.baseURL + path + (query && Object.keys(query).length ? '?' + encodeGoQuery(query) : '');
    for (let attempt = 0; attempt < 2; attempt++) {
      let observedStatus = 0;
      let observedHeaders: ProviderResponse['headers'] = {};
      const token = session.credentials.accessToken.reveal();
      const request: ProviderRequest = {
        url: target, method, headers: { Authorization: `Zoho-oauthtoken ${token}`, orgId: this.orgId, Accept: 'application/json', ...(encoded ? { 'Content-Type': 'application/json' } : {}) },
        ...(encoded ? { body: encoded } : {}), maxResponseBytes: this.maxResponseBytes, maxErrorResponseBytes: MaxErrorBodyBytes,
        timeoutMs: 10_000, signal: this.operationSignal(call),
        onResponseChunk: (_chunk, status, headers) => { observedStatus = status; observedHeaders = headers; },
        addressPolicy: this.apiBaseURL ? 'privateNetworkHTTP' : 'publicHTTPS',
      };
      let response: ProviderResponse;
      try { response = await this.send(request); }
      catch (error) {
        if (error instanceof BodyTooLargeError) {
          const responseWithoutBody = error.status === undefined ? undefined : { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() };
          return { outcome: this.failure('invalid', operation, 'RESPONSE_TOO_LARGE', 'Zoho Desk response exceeds the configured maxResponseBytes limit', 0, responseWithoutBody) };
        }
        if (observedStatus !== 0) {
          const partialResponse = { status: observedStatus, headers: observedHeaders, body: new Uint8Array() };
          return { outcome: this.failure('unavailable', operation, 'TRANSPORT', 'Zoho Desk response could not be read', 0, partialResponse) };
        }
        if (error instanceof ProviderTransportError && error.isProvablyNotSent) return { outcome: this.failure('notSent', operation, 'TRANSPORT', 'Zoho Desk could not be reached; no request was sent') };
        if (error instanceof AddressRefusal) return { outcome: this.failure('notSent', operation, 'TRANSPORT', 'Zoho Desk could not be reached; no request was sent') };
        return { outcome: this.failure('unavailable', operation, 'TRANSPORT', 'Zoho Desk request failed before a response arrived') };
      }
      if (response.status === 401 && attempt === 0 && 'resolveAfterRejection' in this.credentials) {
        try {
          const replacement = await resolveCredentialAfterRejection(this.credentials, call, this.refreshDriver, this.operationSignal(call));
          if (this.hasValidCredentials(replacement) && replacement.authMethodId === session.credentials.authMethodId) { session.credentials = replacement; continue; }
        } catch { /* The original 401 remains authoritative when refresh fails. */ }
      }
      if (token && Buffer.from(response.body).includes(Buffer.from(token))) {
        if (response.status >= 200 && response.status < 300) return { outcome: this.failure('invalid', operation, 'PROTOCOL', 'Zoho Desk response reflected the connection credential', 0, response) };
        response = { ...response, body: new Uint8Array() };
      }
      if (response.status >= 200 && response.status < 300) return { response };
      return { outcome: classifyStatus(response, operation, this.now()) };
    }
    throw new Error('Zoho Desk credential retry exhausted');
  }

  private hasValidCredentials(credentials: Credentials): boolean {
    return !!credentials && !!dataCenterHosts[credentials.authMethodId || 'zoho-us-oauth'] &&
      !!credentials.accessToken?.reveal() && isHeaderSafeCredential(credentials.accessToken.reveal()) &&
      !!credentials.oauthClientId && !!credentials.oauthClientSecret?.reveal() && !!credentials.refreshToken?.reveal();
  }

  private operationSignal(call: Call): AbortSignal {
    const existing = this.operationSignals.get(call);
    if (existing) return existing;
    const signal = AbortSignal.any([call.context.cancellationSignal, AbortSignal.timeout(30_000)]);
    this.operationSignals.set(call, signal);
    return signal;
  }

  private failure(disposition: ExchangeFailure['disposition'], operation: string, kind: Failure['kind'], message: string, retryAfterMs = 0, response?: ProviderResponse): ExchangeFailure {
    return { disposition, failure: { kind, provider: 'zoho-desk', operation, message }, retryAfterMs, ...(response ? { response } : {}) };
  }
}

function header(response: ProviderResponse | undefined, name: string): string {
  const value = response?.headers[name];
  return Array.isArray(value) ? value[0] ?? '' : typeof value === 'string' ? value : '';
}
function safeCreditCount(value: string): string { return /^[0-9]{1,12}$/.test(value) ? value : ''; }
export function queryEscape(value: string): string { return encodeURIComponent(value).replace(/[!'()*]/g, character => '%' + character.charCodeAt(0).toString(16).toUpperCase()).replace(/%20/g, '+'); }
export function encodeGoQuery(query: Record<string, string>): string { return Object.keys(query).sort((left, right) => Buffer.compare(Buffer.from(left), Buffer.from(right))).map(key => `${queryEscape(key)}=${queryEscape(query[key] ?? '')}`).join('&'); }
function classifyStatus(response: ProviderResponse, operation: string, now: Date): ExchangeFailure {
  const status = response.status;
  const summary = describeError(response.body);
  const withSummary = (message: string) => message + (summary ? ` [${summary}]` : '');
  const retryAfterMs = parseRetryAfter(header(response, 'retry-after'), now);
  const failure = (disposition: ExchangeFailure['disposition'], kind: Failure['kind'], message: string, delay = 0): ExchangeFailure =>
    ({ disposition, failure: { kind, provider: 'zoho-desk', operation, message }, retryAfterMs: delay, response });
  if (status === 429) return failure('rateLimited', 'RATE_LIMIT', withSummary('Zoho Desk rate limited the request (HTTP 429)'), retryAfterMs);
  if (status === 408 || status >= 500) return failure('unavailable', 'AVAILABILITY', withSummary(`Zoho Desk could not complete the request (HTTP ${status})`), retryAfterMs);
  if (status === 404) return failure('notFound', 'NOT_FOUND', withSummary('Zoho Desk found no such resource (HTTP 404)'));
  if (status >= 300 && status < 400) return failure('rejected', 'PROTOCOL', `Zoho Desk redirected the request (HTTP ${status}); check that the connection uses the account's data center`);
  if (status === 401) return failure('rejected', 'AUTHENTICATION', withSummary('Zoho Desk rejected the access token (HTTP 401); reconnect the connection'));
  if (status === 403) {
    const code = summary?.split(';')[0];
    const message = code === 'OAUTH_ORG_MISMATCH' ? 'Zoho Desk refused the request (HTTP 403): the token is bound to another organization; set orgId to the organization chosen at Connect, or reconnect'
      : code === 'SCOPE_MISMATCH' ? 'Zoho Desk refused the request (HTTP 403): the token lacks a required scope; reconnect and accept every requested scope'
      : 'Zoho Desk refused the request (HTTP 403)';
    return failure('rejected', 'AUTHORIZATION', withSummary(message));
  }
  return failure('rejected', status === 400 || status === 422 ? 'VALIDATION' : status === 409 ? 'CONFLICT' : 'PROVIDER_REJECTION', withSummary(`Zoho Desk rejected the request (HTTP ${status})`));
}
function describeError(body: Uint8Array): string {
  let document: unknown;
  try { document = JSON.parse(Buffer.from(body).toString('utf8')); } catch { return ''; }
  if (!document || typeof document !== 'object' || Array.isArray(document)) return '';
  const fields = document as Record<string, unknown>;
  const code = typeof fields.errorCode === 'string' && /^[A-Z][A-Z0-9_]{0,63}$/.test(fields.errorCode) ? fields.errorCode : '';
  const errors = Array.isArray(fields.errors) ? fields.errors.flatMap(item => {
    if (!item || typeof item !== 'object' || Array.isArray(item)) return [];
    const field = item as Record<string, unknown>;
    if (typeof field.fieldName !== 'string' || !/^\/[A-Za-z][A-Za-z0-9_]{0,63}(?:\/[A-Za-z0-9_]{1,64}){0,3}$/.test(field.fieldName)) return [];
    const type = typeof field.errorType === 'string' && /^[a-z][A-Za-z_]{0,31}$/.test(field.errorType) ? `=${field.errorType}` : '';
    return [field.fieldName + type];
  }).sort().slice(0, 5) : [];
  return [code, ...(errors.length ? [`errors: ${errors.join(', ')}`] : [])].filter(Boolean).join('; ');
}
