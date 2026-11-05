// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, readErrorTokens, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { isIP } from 'node:net';
import type { Call, CredentialProvider, Failure, Receipt } from '@superdurable/connectors-sdk';
import type { Config, Credentials } from './generated/models.js';
import { CreateTicketOperation, GetTicketOperation, SearchTicketsOperation, UpdateTicketOperation } from './operations.js';

/** Injectable transport and receipt clock for deterministic provider tests. */
export interface ZendeskClientOptions {
  /** Local compatible API base URL, validated against provider address policy. */
  apiBaseURL?: string;
  /** Substitute the production safe HTTP sender. */
  send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Receipt clock. */
  now?: () => Date;
}

/** Classified response to one Zendesk HTTP request. */
export type ZendeskExchange =
  | { outcome: 'success'; response: ZendeskResponse }
  | { outcome: 'retry' | 'notFound' | 'rejected' | 'invalid' | 'defect'; response: ZendeskResponse; failure: Failure; retryAfterMs?: number };

/** Secret-free response details needed by operations. */
export interface ZendeskResponse { status: number; headers: ProviderResponse['headers']; body: Uint8Array; requestId: string }

/** Authenticated Zendesk Support client shared across Dex Steps. */
export class ZendeskClient {
  private readonly apiBaseURL: string;
  readonly agentTicketURL: string;
  private readonly maxResponseBytes: number;
  private readonly credentials: CredentialProvider<Credentials>;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;

  /** Validate account configuration and bind the current credential provider. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: ZendeskClientOptions = {}) {
    if (!/^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(config.subdomain)) throw new TypeError('Zendesk subdomain must be lowercase letters, digits, and hyphens');
    if (!credentials) throw new TypeError('Zendesk credential provider is required');
    const maxResponseBytes = config.maxResponseBytes ?? 4_194_304;
    if (!Number.isSafeInteger(maxResponseBytes) || maxResponseBytes < 1) throw new TypeError('Zendesk response limit must be positive');
    this.apiBaseURL = validateBaseURL(options.apiBaseURL ?? `https://${config.subdomain}.zendesk.com/api/v2`);
    this.agentTicketURL = `https://${config.subdomain}.zendesk.com/agent/tickets/`;
    this.maxResponseBytes = maxResponseBytes;
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind searchTickets. */ searchTickets(): SearchTicketsOperation { return new SearchTicketsOperation(this); }
  /** Bind getTicket. */ getTicket(): GetTicketOperation { return new GetTicketOperation(this); }
  /** Bind createTicket. */ createTicket(): CreateTicketOperation { return new CreateTicketOperation(this); }
  /** Bind updateTicket. */ updateTicket(): UpdateTicketOperation { return new UpdateTicketOperation(this); }

  /** Resolve current credentials without exposing provider secrets through failures. */
  async resolveCredentials(call: Call): Promise<{ email: string; apiToken: string } | undefined> {
    try {
      const current = await this.credentials.resolve(call, call.context.cancellationSignal);
      const apiToken = current.apiToken?.reveal();
      if (!isBareEmailAddress(current.email) || !apiToken || !isHeaderSafeCredential(apiToken)) return undefined;
      return { email: current.email, apiToken };
    } catch { return undefined; }
  }

  /** Send and classify one request, keeping status classification independent of error-body size. */
  async exchange(call: Call, credentials: { email: string; apiToken: string }, operation: string, method: 'GET' | 'POST' | 'PUT', path: string, query?: Record<string, string>, payload?: unknown, idempotencyKey?: string): Promise<ZendeskExchange> {
    const headers: Record<string, string> = {
      Authorization: `Basic ${Buffer.from(`${credentials.email}/token:${credentials.apiToken}`).toString('base64')}`,
      Accept: 'application/json',
    };
    if (payload !== undefined) headers['Content-Type'] = 'application/json';
    if (idempotencyKey) headers['Idempotency-Key'] = idempotencyKey;
    const suffix = query ? `?${Object.keys(query).sort().map(key => `${goQueryEscape(key)}=${goQueryEscape(query[key]!)}`).join('&')}` : '';
    const request: ProviderRequest = {
      url: `${this.apiBaseURL}${path}${suffix}`, method, headers,
      maxResponseBytes: this.maxResponseBytes, timeoutMs: 9_000,
      addressPolicy: this.apiBaseURL.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal,
      ...(payload === undefined ? {} : { body: Buffer.from(JSON.stringify(payload), 'utf8') }),
    };
    let response: ZendeskResponse;
    let oversized = false;
    try {
      const sent = await this.send(request);
      response = { status: sent.status, headers: sent.headers, body: sent.body, requestId: header(sent.headers, 'x-zendesk-request-id') };
    } catch (error) {
      if (!(error instanceof BodyTooLargeError) || error.status === undefined || error.headers === undefined) {
        return { outcome: 'retry', response: emptyResponse(), failure: zendeskFailure(operation, 'TRANSPORT', 'Zendesk request failed before a response arrived') };
      }
      response = { status: error.status, headers: error.headers, body: new Uint8Array(), requestId: header(error.headers, 'x-zendesk-request-id') };
      oversized = true;
    }
    if (response.status >= 200 && response.status < 300) {
      if (oversized) return { outcome: 'invalid', response, failure: zendeskFailure(operation, 'RESPONSE_TOO_LARGE', 'Zendesk response exceeds the configured maxResponseBytes limit') };
      if (Buffer.from(response.body).includes(Buffer.from(credentials.apiToken))) return { outcome: 'invalid', response: { ...response, body: new Uint8Array() }, failure: zendeskFailure(operation, 'PROTOCOL', 'Zendesk response reflected the connection credential') };
      return { outcome: 'success', response };
    }
    const summary = describeError(response.body, credentials.apiToken);
    const status = response.status;
    const retryAfterMs = parseRetryAfter(header(response.headers, 'retry-after'), this.now());
    if (status === 429) return { outcome: 'retry', response, failure: zendeskFailure(operation, 'RATE_LIMIT', `Zendesk rate limited the request (HTTP 429)${summary}`), retryAfterMs };
    if (status === 408 || status === 409 || status >= 500) return { outcome: 'retry', response, failure: zendeskFailure(operation, status === 409 ? 'CONFLICT' : 'AVAILABILITY', `Zendesk could not complete the request yet (HTTP ${status})${summary}`), retryAfterMs };
    if (status === 404 || status === 410) return { outcome: 'notFound', response, failure: zendeskFailure(operation, 'NOT_FOUND', `Zendesk found no such resource (HTTP ${status})${summary}`) };
    if (status >= 300 && status < 400) return { outcome: 'rejected', response, failure: zendeskFailure(operation, 'PROTOCOL', `Zendesk redirected the request (HTTP ${status}); check that the subdomain is the zendesk.com account subdomain`) };
    const kind = summary.includes('IdempotentRequestError') ? 'CONFLICT' : status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : status === 400 || status === 422 ? 'VALIDATION' : 'PROVIDER_REJECTION';
    return { outcome: 'rejected', response, failure: zendeskFailure(operation, kind, `Zendesk rejected the request (HTTP ${status})${summary}`) };
  }

  /** Produce a provider receipt with the same ticket object ID as Go. */
  receipt(call: Call, response: ZendeskResponse, ticketId = 0): Receipt {
    return { callId: call.id, provider: 'zendesk', observedAt: this.now().toISOString().replace(/\.000Z$/, 'Z'),
      ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}),
      ...(response.requestId ? { providerRequestId: response.requestId } : {}),
      ...(ticketId > 0 ? { providerObjectId: String(ticketId) } : {}) };
  }
}

/** Construct a credential-safe failure for one operation. */
export function zendeskFailure(operation: string, kind: Failure['kind'], message: string): Failure { return { kind, provider: 'zendesk', operation, message }; }
/** Return one case-insensitive provider response header. */
export function header(headers: ProviderResponse['headers'], name: string): string { const value = headers[name.toLowerCase()]; return Array.isArray(value) ? (value[0] ?? '') : (value ?? ''); }
function emptyResponse(): ZendeskResponse { return { status: 0, headers: {}, body: new Uint8Array(), requestId: '' }; }
function goQueryEscape(value: string): string { return encodeURIComponent(Buffer.from(value, 'utf8').toString('utf8')).replace(/%20/g, '+').replace(/[!'()*]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`); }
function describeError(body: Uint8Array, secret: string): string {
  try {
    const parsed: unknown = JSON.parse(Buffer.from(body).toString('utf8'));
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return '';
    const error = parsed as Record<string, unknown>;
    const parts: string[] = [];
    const errorToken = readErrorTokens(body, ['/error'])[0];
    if (errorToken && !errorToken.includes(secret)) parts.push(errorToken);
    if (error.details && typeof error.details === 'object' && !Array.isArray(error.details)) {
      const details = Object.keys(error.details).filter(key => /^[a-z][a-z0-9_]{0,63}$/.test(key) && !key.includes(secret)).sort().slice(0, 5);
      const fields = details.map(key => { const token = readErrorTokens(body, [`/details/${key}/0/type`, `/details/${key}/0/error`])[0]; return `${key}${token && !token.includes(secret) ? `=${token}` : ''}`; });
      if (fields.length) parts.push(`details: ${fields.join(', ')}`);
    }
    return parts.length ? ` [${parts.join('; ')}]` : '';
  } catch { return ''; }
}
/** Match Go's bare mail.ParseAddress result, including Unicode atoms and domain literals. */
export function isBareEmailAddress(value: string): boolean {
  if (/[\s\p{Cc}"'()<>,;]/u.test(value) || value.includes('\\')) return false;
  const at = value.lastIndexOf('@');
  if (at < 1 || at !== value.indexOf('@') || at === value.length - 1) return false;
  const local = value.slice(0, at);
  const domain = value.slice(at + 1);
  const dotAtom = /^[^\s@.[\]:]+(?:\.[^\s@.[\]:]+)*$/u;
  if (!dotAtom.test(local)) return false;
  if (domain.startsWith('[') && domain.endsWith(']')) return !domain.includes('%') && isIP(domain.slice(1, -1)) !== 0;
  return dotAtom.test(domain);
}
