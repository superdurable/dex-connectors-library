// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { BodyTooLargeError, MaxErrorBodyBytes, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ReauthorizationRequiredError, RetryError, SecretString, type Call, type Failure, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import type { Config, Credentials, CredentialSource } from './generated/models.js';
import { Authorization, CredentialFields } from './generated/refresh.js';
import { ListContactsOperation, FindContactByEmailOperation, GetInvoiceOperation, ListInvoicesOperation, CreateInvoiceOperation, RecordPaymentOperation } from './operations.js';

const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const accountingURL = 'https://api.xero.com/api.xro/2.0';
const connectionsURL = 'https://api.xero.com/connections';

/** One transport with the same request and response boundaries as the production SDK. */
export type XeroRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;

/** Test seams for the provider transport and receipt clock. */
export interface XeroClientOptions {
  /** Transport for deterministic provider tests; production uses the SDK transport. */
  send?: XeroRequestSender;
  /** Clock for receipts and Retry-After. */
  now?: () => Date;
  /** Loopback Xero stand-in, preserving the Accounting and connections paths. */
  localProviderURL?: string;
}

/** One classified exchange before an operation selects its branch. */
export interface XeroExchange {
  outcome: 'succeeded' | 'notFound' | 'retry' | 'rejected' | 'dailyLimit' | 'invalid' | 'serverError' | 'defect';
  response?: ProviderResponse;
  failure?: Failure;
  retryAfterMs: number;
  receipt: ReceiptDraft;
}

/** One Xero request descriptor; operations own its fields. */
export interface XeroRequest {
  method: string;
  path: string;
  query?: Record<string, string>;
  modifiedSince?: string | null;
  payload?: Uint8Array;
  isMutation?: boolean;
}

/** Authenticated Xero Accounting client; credentials remain outside Flow state. */
export class XeroClient {
  private readonly credentials: CredentialSource;
  private readonly send: XeroRequestSender;
  private readonly now: () => Date;
  private readonly baseURL: string;
  private readonly connectionsEndpoint: string;
  private readonly maxResponseBytes: number;
  private readonly organisation: string;
  private readonly refreshDriver: ManifestCredentialRefreshDriver<Credentials>;
  private derivedTenantID = '';

  /** Validate configuration and bind a renewable credential provider without contacting Xero. */
  constructor(config: Config, credentials: CredentialSource, options: XeroClientOptions = {}) {
    if (!credentials) throw new TypeError('Xero credential provider is required');
    this.maxResponseBytes = config.maxResponseBytes ?? 4_194_304;
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Xero response limit must be positive');
    this.organisation = (config.organisation ?? '').trim();
    if ([...this.organisation].length > 255 || [...this.organisation].some(character => character.codePointAt(0)! < 32 || character.codePointAt(0) === 127)) throw new TypeError('Xero organisation must be one line of at most 255 characters');
    const local = options.localProviderURL;
    if (local) {
      const url = new URL(local);
      if (!['http:', 'https:'].includes(url.protocol) || !(url.hostname.toLowerCase() === 'localhost' || url.hostname === '[::1]' || /^127\./.test(url.hostname)) || url.pathname !== '/' || url.search || url.hash || url.username || url.password) throw new TypeError('Xero local provider URL must be a loopback origin');
    }
    this.baseURL = local ? `${local.replace(/\/$/, '')}/api.xro/2.0` : accountingURL;
    this.connectionsEndpoint = local ? `${local.replace(/\/$/, '')}/connections` : connectionsURL;
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
    this.refreshDriver = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'Xero', auth: Authorization, credentialFields: CredentialFields });
  }

  /** Bind the listContacts query. */ listContacts() { return new ListContactsOperation(this); }
  /** Bind the findContactByEmail query. */ findContactByEmail() { return new FindContactByEmailOperation(this); }
  /** Bind the getInvoice query. */ getInvoice() { return new GetInvoiceOperation(this); }
  /** Bind the listInvoices query. */ listInvoices() { return new ListInvoicesOperation(this); }
  /** Bind the createInvoice mutation. */ createInvoice() { return new CreateInvoiceOperation(this); }
  /** Bind the recordPayment mutation. */ recordPayment() { return new RecordPaymentOperation(this); }

  /** Send one Accounting request with live credentials and Xero's tenant header. */
  async exchange(call: Call, operation: string, request: XeroRequest): Promise<XeroExchange> {
    let credentials: Credentials;
    try { credentials = await this.credentials.resolveWithRefresh(call, this.refreshDriver, call.context.cancellationSignal); }
    catch (error) {
      const rejected = error instanceof ReauthorizationRequiredError;
      const transient = error instanceof RetryError;
      return this.localFailure(call, operation, rejected ? 'rejected' : transient ? 'retry' : 'defect', rejected ? 'AUTHENTICATION' : transient ? 'AVAILABILITY' : 'AUTHENTICATION', rejected ? 'Xero authorization must be renewed: reconnect the app or replace the client secret' : transient ? 'Xero access token request is temporarily unavailable' : 'Xero connection credentials are unavailable');
    }
    if (!['custom-connection', 'xero-oauth'].includes(credentials.authMethodId ?? '') || !(credentials.accessToken instanceof SecretString) || !isHeaderSafeCredential(credentials.accessToken.reveal())) return this.localFailure(call, operation, 'defect', 'AUTHENTICATION', 'Xero connection credentials are unavailable');
    let tenantID = '';
    if (credentials.authMethodId === 'xero-oauth') {
      const tenant = await this.resolveTenant(call, operation, credentials.accessToken.reveal());
      if (typeof tenant !== 'string') return tenant;
      tenantID = tenant;
    }
    let result = await this.sendAccounting(call, operation, request, credentials.accessToken.reveal(), tenantID);
    if (result.response?.status === 401 && 'resolveAfterRejection' in this.credentials && typeof this.credentials.resolveAfterRejection === 'function') {
      try {
        const replacement = await this.credentials.resolveAfterRejection(call, this.refreshDriver, call.context.cancellationSignal);
        if (replacement.authMethodId === credentials.authMethodId && replacement.accessToken instanceof SecretString && isHeaderSafeCredential(replacement.accessToken.reveal())) result = await this.sendAccounting(call, operation, request, replacement.accessToken.reveal(), tenantID);
      } catch { /* The original 401 remains the conclusive provider response. */ }
    }
    if (result.response?.status === 403 && tenantID === this.derivedTenantID) this.derivedTenantID = '';
    if (tenantID) result.receipt.metadata = { ...result.receipt.metadata, tenantId: tenantID };
    return result;
  }

  /** Build a receipt from one classified exchange and the operation's object ID. */
  receipt(call: Call, result: XeroExchange, objectID = ''): ReceiptDraft {
    return { ...result.receipt, callId: call.id, provider: 'xero', observedAt: this.now().toISOString(),
      ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}),
      ...(objectID ? { providerObjectId: objectID } : {}) };
  }

  private async resolveTenant(call: Call, operation: string, token: string): Promise<string | XeroExchange> {
    if (uuidPattern.test(this.organisation)) return this.organisation.toLowerCase();
    if (this.derivedTenantID) return this.derivedTenantID;
    const read = await this.request(call, operation, { url: this.connectionsEndpoint, method: 'GET', headers: { Authorization: `Bearer ${token}`, Accept: 'application/json' }, maxResponseBytes: this.maxResponseBytes, maxErrorResponseBytes: MaxErrorBodyBytes, ignoreErrorBodyReadFailure: true, addressPolicy: this.policy(), timeoutMs: 20_000, signal: call.context.cancellationSignal }, token);
    if (read.outcome !== 'succeeded') { if (read.outcome === 'serverError') read.outcome = 'retry'; else if (read.outcome === 'invalid' || read.outcome === 'notFound') read.outcome = 'rejected'; return read; }
    let all: unknown;
    try { all = JSON.parse(Buffer.from(read.response!.body).toString()); } catch { return this.localFailure(call, operation, 'rejected', 'PROTOCOL', 'Xero returned a connections list that is not a JSON array', read.receipt); }
    if (!Array.isArray(all)) return this.localFailure(call, operation, 'rejected', 'PROTOCOL', 'Xero returned a connections list that is not a JSON array', read.receipt);
    if (all.some(item => item !== null && (typeof item !== 'object' || Array.isArray(item) || ['tenantId', 'tenantType', 'tenantName'].some(key => item[key] !== undefined && item[key] !== null && typeof item[key] !== 'string')))) return this.localFailure(call, operation, 'rejected', 'PROTOCOL', 'Xero returned a connections list that is not a JSON array', read.receipt);
    const candidates = all.filter(item => item && typeof item === 'object' && item.tenantType === 'ORGANISATION' && typeof item.tenantId === 'string' && uuidPattern.test(item.tenantId) && (!this.organisation || (typeof item.tenantName === 'string' && item.tenantName.trim().toLowerCase() === this.organisation.toLowerCase())));
    if (candidates.length !== 1) {
      const message = !this.organisation ? (candidates.length ? `the Xero authorization connects ${candidates.length} organisations; set organisation to the name or tenant ID of one` : 'the Xero authorization connects no organisation; authorize again and select one') : (candidates.length ? 'several organisations connected to the Xero authorization have the configured name; set organisation to the tenant ID' : 'no organisation connected to the Xero authorization has the configured name');
      return this.localFailure(call, operation, 'defect', 'VALIDATION', message);
    }
    this.derivedTenantID = String(candidates[0].tenantId).toLowerCase();
    return this.derivedTenantID;
  }

  private async sendAccounting(call: Call, operation: string, request: XeroRequest, token: string, tenantID: string): Promise<XeroExchange> {
    if (request.isMutation && (!call.idempotencyKey || Buffer.byteLength(call.idempotencyKey) > 128)) return this.localFailure(call, operation, 'defect', 'LOCAL_DEFECT', 'Xero mutation has no usable idempotency key');
    const query = request.query ? encodeGoQuery(request.query) : '';
    const headers: Record<string, string> = { Authorization: `Bearer ${token}`, Accept: 'application/json' };
    if (request.payload) headers['Content-Type'] = 'application/json';
    if (tenantID) headers['Xero-Tenant-Id'] = tenantID;
    if (request.modifiedSince) headers['If-Modified-Since'] = new Date(request.modifiedSince).toISOString().slice(0, 19);
    if (request.isMutation) headers['Idempotency-Key'] = call.idempotencyKey!;
    return this.request(call, operation, { url: this.baseURL + request.path + (query ? `?${query}` : ''), method: request.method, headers, ...(request.payload ? { body: request.payload } : {}), maxResponseBytes: this.maxResponseBytes, maxErrorResponseBytes: MaxErrorBodyBytes, ignoreErrorBodyReadFailure: true, addressPolicy: this.policy(), timeoutMs: 20_000, signal: call.context.cancellationSignal }, token);
  }

  private policy(): ProviderRequest['addressPolicy'] { return this.baseURL === accountingURL ? 'publicHTTPS' : 'privateNetworkHTTP'; }

  private async request(call: Call, operation: string, request: ProviderRequest, token: string): Promise<XeroExchange> {
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError) return this.localFailure(call, operation, 'invalid', 'RESPONSE_TOO_LARGE', 'Xero response exceeds the configured maxResponseBytes limit');
      return this.localFailure(call, operation, 'retry', 'TRANSPORT', 'Xero request failed before a response arrived');
    }
    const headers = response.headers;
    const correlation = firstHeader(headers['xero-correlation-id']);
    const receipt: ReceiptDraft = { ...(correlation && /^[A-Za-z0-9-]{1,64}$/.test(correlation) ? { providerRequestId: correlation } : {}) };
    const metadata: Record<string, string> = {};
    for (const [header, key] of [['x-daylimit-remaining', 'dayLimitRemaining'], ['x-minlimit-remaining', 'minuteLimitRemaining'], ['x-appminlimit-remaining', 'appMinuteLimitRemaining']] as const) {
      const value = firstHeader(headers[header])?.trim() ?? '';
      if (/^[0-9]{1,10}$/.test(value)) metadata[key] = value;
    }
    if (Object.keys(metadata).length) receipt.metadata = metadata;
    if (response.status >= 200 && response.status < 300) {
      if (token && Buffer.from(response.body).includes(Buffer.from(token))) return this.localFailure(call, operation, 'invalid', 'PROTOCOL', 'Xero response reflected the connection credential', receipt, response);
      return { outcome: 'succeeded', response, retryAfterMs: 0, receipt };
    }
    const status = response.status;
    const retryAfterMs = parseRetryAfter(firstHeader(headers['retry-after']) ?? '', this.now());
    const summary = errorSummary(response.body, token);
    let outcome: XeroExchange['outcome'] = 'rejected';
    let kind: Failure['kind'] = 'PROVIDER_REJECTION';
    let message = 'Xero rejected the request';
    if (status === 429) {
      const rawProblem = firstHeader(headers['x-rate-limit-problem'])?.trim() ?? '';
      const problem = /^[A-Za-z]{1,32}$/.test(rawProblem) ? rawProblem.toLowerCase() : '';
      const rawSeconds = firstHeader(headers['retry-after'])?.trim() ?? '';
      const seconds = /^[0-9]{1,10}$/.test(rawSeconds) ? Number(rawSeconds) : 0;
      if (problem) metadata.rateLimitProblem = problem;
      if (problem === 'day' || seconds > 120) {
        outcome = 'dailyLimit'; kind = 'QUOTA_EXHAUSTED'; message = "Xero's daily API limit for the organisation is exhausted";
        if (rawSeconds && /^[0-9]{1,10}$/.test(rawSeconds)) metadata.retryAfterSeconds = rawSeconds;
      } else { outcome = 'retry'; kind = 'RATE_LIMIT'; message = `Xero's ${problem || 'rate'} limit throttled the request`; }
      if (Object.keys(metadata).length) receipt.metadata = metadata;
    } else if (status === 500) { outcome = 'serverError'; kind = 'AVAILABILITY'; message = 'Xero failed internally'; }
    else if (status === 501) { message = 'Xero does not implement the request'; }
    else if (status === 408 || status >= 500) { outcome = 'retry'; kind = 'AVAILABILITY'; message = 'Xero is temporarily unavailable'; }
    else if (status === 401) { kind = 'AUTHENTICATION'; message = 'Xero rejected the access token'; }
    else if (status === 403) { kind = 'AUTHORIZATION'; message = 'Xero denied access to the organisation or scope'; }
    else if (status === 404) { outcome = 'notFound'; kind = 'NOT_FOUND'; message = 'Xero found no such resource'; }
    else if (status === 400) { kind = 'VALIDATION'; message = 'Xero rejected the request'; }
    else if (status >= 300 && status < 400) { kind = 'PROTOCOL'; message = 'Xero redirected the request'; }
    return { outcome, response, retryAfterMs, receipt, failure: failure(kind, operation, withSummary(`${message} (HTTP ${status})`, summary)) };
  }

  private localFailure(call: Call, operation: string, outcome: XeroExchange['outcome'], kind: Failure['kind'], message: string, receipt: ReceiptDraft = {}, response?: ProviderResponse): XeroExchange {
    void call;
    return { outcome, ...(response ? { response } : {}), failure: failure(kind, operation, message), retryAfterMs: 0, receipt };
  }
}

/** Encode query keys and values like Go url.Values.Encode. */
export function encodeGoQuery(values: Record<string, string>): string {
  const escape = (value: string) => encodeURIComponent(value).replace(/%20/g, '+').replace(/[!'()*~]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`);
  return Object.keys(values).sort().map(key => `${escape(key)}=${escape(values[key]!)}`).join('&');
}

/** Escape an invoice identifier like Go url.PathEscape. */
export function escapeGoPath(value: string): string {
  return [...Buffer.from(value)].map(byte => {
    const character = String.fromCharCode(byte);
    return /[A-Za-z0-9._~\-$&+=:@]/.test(character) ? character : `%${byte.toString(16).toUpperCase().padStart(2, '0')}`;
  }).join('');
}

function firstHeader(value: string | string[] | number | undefined): string | undefined { return Array.isArray(value) ? value[0] : typeof value === 'string' ? value : undefined; }
function failure(kind: Failure['kind'], operation: string, message: string): Failure { return { kind, provider: 'xero', operation, message }; }
function withSummary(message: string, parts: string[]): string { return parts.length ? `${message} [${parts.join('; ')}]` : message; }
function errorSummary(body: Uint8Array, token: string): string[] {
  let value: unknown;
  try { value = JSON.parse(Buffer.from(body).toString()); } catch { return []; }
  if (!value || typeof value !== 'object' || Array.isArray(value)) return [];
  const record = value as Record<string, unknown>;
  const parts: string[] = [];
  if (typeof record.Type === 'string' && !record.Type.includes(token)) parts.push(record.Type);
  if (typeof record.ErrorNumber === 'number' && Number.isInteger(record.ErrorNumber) && record.ErrorNumber >= 0) parts.push(`error ${record.ErrorNumber}`);
  const detail = [record.Detail, record.detail, record.error].find(item => typeof item === 'string' && !item.includes(token));
  if (typeof detail === 'string') parts.push(detail);
  if (Array.isArray(record.Elements)) {
    const count = record.Elements.reduce((total: number, item: unknown) => total + (item && typeof item === 'object' && Array.isArray((item as Record<string, unknown>).ValidationErrors) ? ((item as Record<string, unknown>).ValidationErrors as unknown[]).length : 0), 0);
    if (count) parts.push(`${Math.min(count, 1000)} validation error${count === 1 ? '' : 's'}`);
  }
  return parts;
}
