// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { AddressRefusal, BodyTooLargeError, ProviderTransportError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import type { Call, CredentialProvider, Failure, ReceiptDraft } from '@superdurable/connectors-sdk';
import type { Config, Credentials } from './generated/models.js';
import { GetMemberOperation, ListMembersOperation, UpsertMemberOperation, UpdateMemberTagsOperation, SendCampaignOperation } from './operations.js';

/** Injectable provider transport for deterministic tests. */
export type MailchimpRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Constructor dependencies for deterministic tests and local stand-ins. */
export interface MailchimpClientOptions {
  /** Local Mailchimp-compatible endpoint; production derives the host from the API key. */
  apiBaseURL?: string;
  /** Optional request sender; production uses the bounded no-redirect transport. */
  send?: MailchimpRequestSender;
  /** Receipt and Retry-After clock. */
  now?: () => Date;
}
/** One classified Mailchimp exchange. */
export interface MailchimpExchange {
  outcome: 'succeeded' | 'notFound' | 'rateLimited' | 'notSent' | 'unavailable' | 'rejected' | 'invalid' | 'defect';
  status: number;
  body: Uint8Array;
  headers: ProviderResponse['headers'];
  failure?: Failure;
  retryAfterMs: number;
}
/** Build a secret-safe connector failure. */
export function mailchimpFailure(operation: string, kind: Failure['kind'], message: string): Failure {
  return { kind, provider: 'mailchimp', operation, message };
}
/** Authenticated Mailchimp Marketing API client. */
export class MailchimpClient {
  private readonly maxResponseBytes: number;
  private readonly credentials: CredentialProvider<Credentials>;
  private readonly apiBaseURL: string | undefined;
  private readonly send: MailchimpRequestSender;
  private readonly now: () => Date;

  /** Validate configuration and retain immutable dependencies. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: MailchimpClientOptions = {}) {
    if (!credentials) throw new TypeError('Mailchimp credential provider is required');
    const maximum = config.maxResponseBytes ?? 4_194_304;
    if (!Number.isSafeInteger(maximum) || maximum < 1) throw new TypeError('Mailchimp response limit must be positive');
    this.maxResponseBytes = maximum;
    this.credentials = credentials;
    this.apiBaseURL = options.apiBaseURL ? validateBaseURL(options.apiBaseURL) : undefined;
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }
  /** Bind the contact read. */
  getMember() { return new GetMemberOperation(this); }
  /** Bind the audience page read. */
  listMembers() { return new ListMembersOperation(this); }
  /** Bind the contact upsert. */
  upsertMember() { return new UpsertMemberOperation(this); }
  /** Bind the tag update. */
  updateMemberTags() { return new UpdateMemberTagsOperation(this); }
  /** Bind the guarded campaign send. */
  sendCampaign() { return new SendCampaignOperation(this); }

  /** Resolve the current API key and its data center without exposing it in failures. */
  async resolveConnection(call: Call): Promise<{ key: string; baseURL: string } | undefined> {
    let key: string;
    try { key = (await this.credentials.resolve(call, call.context.cancellationSignal)).apiKey.reveal(); }
    catch { return undefined; }
    const match = /^([A-Za-z0-9]{8,128})-([a-z]{2,10}[0-9]{1,4})$/.exec(key);
    if (!match || !isHeaderSafeCredential(key)) return undefined;
    return { key, baseURL: this.apiBaseURL ?? `https://${match[2]}.api.mailchimp.com/3.0` };
  }

  /** Send one request and classify the outcome before an operation chooses its branch. */
  async exchange(call: Call, connection: { key: string; baseURL: string }, operation: string, method: string, path: string, query?: URLSearchParams, payload?: unknown): Promise<MailchimpExchange> {
    const url = `${connection.baseURL}${path}${query && [...query.keys()].length ? `?${sortedQuery(query)}` : ''}`;
    const headers: Record<string, string> = { Authorization: `Bearer ${connection.key}`, Accept: 'application/json' };
    if (payload !== undefined) headers['Content-Type'] = 'application/json';
    const request: ProviderRequest = {
      url, method, headers, maxResponseBytes: this.maxResponseBytes,
      addressPolicy: this.apiBaseURL ? 'privateNetworkHTTP' : 'publicHTTPS',
      timeoutMs: Math.min(25_000, call.context.deadline ? Math.max(1, call.context.deadline.getTime() - Date.now()) : 25_000),
      signal: call.context.cancellationSignal,
      ...(payload !== undefined ? { body: Buffer.from(JSON.stringify(payload), 'utf8') } : {}),
    };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined && error.headers !== undefined) {
        response = { status: error.status, headers: error.headers, body: new Uint8Array() };
        if (response.status >= 200 && response.status < 300) return { outcome: 'invalid', status: response.status, headers: response.headers, body: response.body, failure: mailchimpFailure(operation, 'RESPONSE_TOO_LARGE', 'Mailchimp response exceeds the configured maxResponseBytes limit'), retryAfterMs: 0 };
      } else {
        const isNotSent = error instanceof AddressRefusal || error instanceof ProviderTransportError && error.isProvablyNotSent;
        return { outcome: isNotSent ? 'notSent' : 'unavailable', status: 0, headers: {}, body: new Uint8Array(), failure: mailchimpFailure(operation, 'TRANSPORT', isNotSent ? 'Mailchimp could not be reached; no request was sent' : 'Mailchimp request failed before a response arrived'), retryAfterMs: 0 };
      }
    }
    const status = response.status;
    const retryAfterMs = parseRetryAfter(header(response.headers, 'retry-after'), this.now());
    if (status >= 200 && status < 300) {
      if (Buffer.from(response.body).includes(Buffer.from(connection.key))) return { outcome: 'invalid', status, headers: response.headers, body: new Uint8Array(), failure: mailchimpFailure(operation, 'PROTOCOL', 'Mailchimp response reflected the connection credential'), retryAfterMs: 0 };
      return { outcome: 'succeeded', status, headers: response.headers, body: response.body, retryAfterMs: 0 };
    }
    const hasProblem = hasProblemDocument(response.body);
    let outcome: MailchimpExchange['outcome'];
    let kind: Failure['kind'];
    if (status === 429 || (status === 403 && !hasProblem)) { outcome = 'rateLimited'; kind = 'RATE_LIMIT'; }
    else if (status === 408 || status >= 500) { outcome = 'unavailable'; kind = 'AVAILABILITY'; }
    else if (status === 404) { outcome = 'notFound'; kind = 'NOT_FOUND'; }
    else { outcome = 'rejected'; kind = status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : status === 409 ? 'CONFLICT' : status === 400 ? 'VALIDATION' : status >= 300 && status < 400 ? 'PROTOCOL' : 'PROVIDER_REJECTION'; }
    return { outcome, status, headers: response.headers, body: new Uint8Array(), failure: mailchimpFailure(operation, kind, `Mailchimp request failed (HTTP ${status})`), retryAfterMs };
  }

  /** Create a secret-free provider receipt. */
  receipt(call: Call, exchange: MailchimpExchange, objectID: string): ReceiptDraft {
    const requestID = header(exchange.headers, 'x-request-id');
    return { callId: call.id, provider: 'mailchimp', observedAt: goDate(this.now()),
      ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}),
      ...(objectID ? { providerObjectId: objectID } : {}),
      ...(/^[A-Za-z0-9._:-]{1,128}$/.test(requestID) ? { providerRequestId: requestID } : {}) };
  }
}

function header(headers: ProviderResponse['headers'], name: string): string {
  const value = headers[name.toLowerCase()];
  return Array.isArray(value) ? value[0] ?? '' : value ?? '';
}
function hasProblemDocument(body: Uint8Array): boolean {
  try { const document = JSON.parse(Buffer.from(body).toString('utf8')) as { title?: unknown }; return typeof document.title === 'string'; }
  catch { return false; }
}
function sortedQuery(query: URLSearchParams): string {
  return [...query.entries()].sort(([left], [right]) => Buffer.compare(Buffer.from(left), Buffer.from(right)))
    .map(([key, value]) => `${goQueryEscape(key)}=${goQueryEscape(value)}`).join('&');
}
function goQueryEscape(value: string): string {
  return [...Buffer.from(value, 'utf8')].map(byte =>
    byte >= 65 && byte <= 90 || byte >= 97 && byte <= 122 || byte >= 48 && byte <= 57 || [45, 46, 95, 126].includes(byte)
      ? String.fromCharCode(byte) : byte === 32 ? '+' : `%${byte.toString(16).toUpperCase().padStart(2, '0')}`).join('');
}
function goDate(date: Date): string { return date.toISOString().replace(/\.000Z$/, 'Z'); }
