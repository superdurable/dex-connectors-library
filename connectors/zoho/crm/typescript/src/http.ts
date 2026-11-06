// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { ReauthorizationRequiredError, resolveCredential, resolveCredentialAfterRejection, type Call, type CredentialRefreshDriver, type Failure, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { Config, CredentialSource, Credentials, ProviderError } from './models.js';
import { isFieldPath, isRecordId } from './coql.js';

/** Testable Zoho provider transport; production uses the SDK bounded sender. */
export type ZohoTransport = (request: ProviderRequest) => Promise<ProviderResponse>;

/** Constructor-injected HTTP and credential collaborators. */
export interface ZohoClientOptions {
  /** Local fake API base URL, including /crm/v8. */ apiBaseURL?: string;
  /** Provider request sender. */ transport?: ZohoTransport;
  /** Generated-declaration OAuth driver override for controlled tests. */ refreshDriver?: CredentialRefreshDriver<Credentials>;
  /** Clock used for receipts and Retry-After dates. */ now?: () => Date;
}

/** One validated provider error, with no provider message or credential text. */
export interface ErrorSummary {
  code: string; field: string; duplicateRecordId: string; hasDetailsId: boolean;
}

/** One classified Zoho CRM exchange. */
export interface Exchange {
  outcome: 'success' | 'retry' | 'rejected' | 'invalid' | 'defect';
  status: number; headers: Record<string, string | string[] | undefined>; body: Uint8Array;
  summary: ErrorSummary; failure?: Failure; retryAfterMs: number; remainingCredits: string;
}

const emptySummary = (): ErrorSummary => ({ code: '', field: '', duplicateRecordId: '', hasDetailsId: false });
const emptyExchange = (outcome: Exchange['outcome'], failure?: Failure): Exchange => ({ outcome, status: 0, headers: {}, body: new Uint8Array(), summary: emptySummary(), ...(failure ? { failure } : {}), retryAfterMs: 0, remainingCredits: '' });
const dataCenters: Record<string, string> = {
  'zoho-us-oauth': 'zohoapis.com', 'zoho-eu-oauth': 'zohoapis.eu', 'zoho-in-oauth': 'zohoapis.in',
  'zoho-au-oauth': 'zohoapis.com.au', 'zoho-jp-oauth': 'zohoapis.jp', 'zoho-ca-oauth': 'zohoapis.ca', 'zoho-sa-oauth': 'zohoapis.sa',
};

/** Return a safe, connector-written failure. */
export function crmFailure(kind: Failure['kind'], operation: string, message: string): Failure {
  return { kind, provider: 'zoho-crm', operation, message };
}

function detailText(summary: ErrorSummary, message: string): string {
  const details = [summary.code, summary.field && `field: ${summary.field}`, summary.duplicateRecordId && `duplicate record: ${summary.duplicateRecordId}`].filter(Boolean).join('; ');
  return details ? `${message} [${details}]` : message;
}

function summaryFromObject(value: unknown): ErrorSummary {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return emptySummary();
  const item = value as Record<string, unknown>;
  if (typeof item.code !== 'string' || !/^[A-Z][A-Z0-9_]{0,63}$/.test(item.code)) return emptySummary();
  const details = item.details && typeof item.details === 'object' && !Array.isArray(item.details) ? item.details as Record<string, unknown> : {};
  const duplicate = details.duplicate_record && typeof details.duplicate_record === 'object' ? details.duplicate_record as Record<string, unknown> : {};
  return { code: item.code, field: typeof details.api_name === 'string' && isFieldPath(details.api_name) ? details.api_name : '',
    duplicateRecordId: typeof duplicate.id === 'string' && isRecordId(duplicate.id) ? duplicate.id : '',
    hasDetailsId: details.id !== undefined && details.id !== null };
}

/** Read the first machine-readable error without exposing provider message text. */
export function describeError(body: Uint8Array): ErrorSummary {
  let document: unknown;
  try { document = JSON.parse(new TextDecoder().decode(body)); }
  catch { return emptySummary(); }
  const root = summaryFromObject(document);
  if (root.code) return root;
  if (document && typeof document === 'object' && !Array.isArray(document)) {
    const data = (document as Record<string, unknown>).data;
    if (Array.isArray(data)) for (const item of data) {
      if (item && typeof item === 'object' && (item as Record<string, unknown>).status === 'error') return summaryFromObject(item);
    }
  }
  return emptySummary();
}

/** Whether INVALID_DATA conclusively reports a missing record ID. */
export function isMissingRecord(summary: ErrorSummary): boolean {
  return summary.code === 'INVALID_DATA' && (summary.field === 'id' || (!summary.field && summary.hasDetailsId));
}

/** Convert the summary to the public error list used by mutations. */
export function providerErrors(summary: ErrorSummary): ProviderError[] | undefined {
  return summary.code ? [{ code: summary.code, ...(summary.field ? { field: summary.field } : {}), ...(summary.duplicateRecordId ? { duplicateRecordId: summary.duplicateRecordId } : {}) }] : undefined;
}

/** Validate the exact data-center API host before transmitting a token. */
export function apiBaseURL(credentials: Credentials, override?: string): string {
  const suffix = dataCenters[credentials.authMethodId ?? ''];
  if (!suffix) throw new TypeError('the authorization method is not a supported data center');
  if (!isHeaderSafeCredential(credentials.accessToken.reveal())) throw new TypeError('the access token is missing or is not a valid header value');
  if (override) return override;
  const apiDomain = credentials.apiDomain || `https://www.${suffix}`;
  let url: URL;
  try { url = new URL(apiDomain); }
  catch { throw new TypeError('Zoho CRM api_domain must be an https zohoapis host of the connection\'s data center'); }
  if (url.protocol !== 'https:' || url.username || url.password || url.port || !['', '/'].includes(url.pathname) || url.search || url.hash || !['www', 'sandbox', 'developer'].some(prefix => url.hostname === `${prefix}.${suffix}`)) {
    throw new TypeError('Zoho CRM api_domain is not a zohoapis host of the connection\'s data center');
  }
  return `${url.origin}/crm/v8`;
}

function header(headers: Exchange['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}

/** Bound authenticated Zoho CRM client shared by the six operations. */
export class ZohoHTTPClient {
  readonly #credentials: CredentialSource;
  readonly #refreshDriver: CredentialRefreshDriver<Credentials>;
  readonly #transport: ZohoTransport;
  readonly #apiBaseURL: string | undefined;
  readonly #maxResponseBytes: number;
  readonly #now: () => Date;

  /** Validate configuration and bind host-owned credentials. */
  constructor(config: Config, credentials: CredentialSource, options: ZohoClientOptions = {}) {
    if (!credentials) throw new TypeError('Zoho CRM credential provider is required');
    const maxResponseBytes = config.maxResponseBytes ?? 4_194_304;
    if (!Number.isSafeInteger(maxResponseBytes) || maxResponseBytes < 1) throw new TypeError('Zoho CRM response limit must be positive');
    this.#maxResponseBytes = maxResponseBytes;
    this.#credentials = credentials;
    this.#refreshDriver = options.refreshDriver ?? new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'Zoho', auth: Authorization, credentialFields: CredentialFields, ...(options.now ? { now: options.now } : {}) });
    this.#transport = options.transport ?? sendProviderRequest;
    this.#apiBaseURL = options.apiBaseURL ? validateBaseURL(options.apiBaseURL) : undefined;
    this.#now = options.now ?? (() => new Date());
  }

  /** Resolve the credential and classify admission errors like Go. */
  async session(call: Call, operation: string): Promise<{ credentials: Credentials; baseURL: string } | Exchange> {
    let credentials: Credentials;
    try { credentials = await resolveCredential(this.#credentials, call, this.#refreshDriver, call.context.cancellationSignal); }
    catch (error) {
      if (error instanceof ReauthorizationRequiredError) return emptyExchange('rejected', crmFailure('AUTHENTICATION', operation, 'Zoho authorization is revoked or expired; reconnect the connection'));
      return emptyExchange('retry', crmFailure('AUTHENTICATION', operation, 'Zoho credentials are temporarily unavailable'));
    }
    try { return { credentials, baseURL: apiBaseURL(credentials, this.#apiBaseURL) }; }
    catch (error) { return emptyExchange('defect', crmFailure('AUTHENTICATION', operation, `Zoho CRM connection credentials are missing or invalid: ${String(error)}`)); }
  }

  /** Send a provider request, retrying once after a rejected token. */
  async exchange(call: Call, operation: string, session: { credentials: Credentials; baseURL: string }, method: string, pathAndQuery: string, payload?: unknown): Promise<Exchange> {
    let encoded: Uint8Array | undefined;
    if (payload !== undefined) {
      try { encoded = new TextEncoder().encode(JSON.stringify(payload)); }
      catch { return emptyExchange('defect', crmFailure('LOCAL_DEFECT', operation, 'Zoho CRM request could not be built')); }
    }
    for (let attempt = 0; attempt < 2; attempt++) {
      const result = await this.#exchangeOnce(call, operation, session, method, pathAndQuery, encoded);
      if (result.status !== 401 || result.summary.code === 'OAUTH_SCOPE_MISMATCH' || attempt > 0 || !('resolveAfterRejection' in this.#credentials)) return result;
      try {
        const replacement = await resolveCredentialAfterRejection(this.#credentials, call, this.#refreshDriver, call.context.cancellationSignal);
        if (replacement.authMethodId !== session.credentials.authMethodId) return result;
        session.credentials = replacement;
        session.baseURL = apiBaseURL(replacement, this.#apiBaseURL);
      } catch { return result; }
    }
    return emptyExchange('defect', crmFailure('LOCAL_DEFECT', operation, 'Zoho CRM request could not be built'));
  }

  async #exchangeOnce(call: Call, operation: string, session: { credentials: Credentials; baseURL: string }, method: string, pathAndQuery: string, body?: Uint8Array): Promise<Exchange> {
    if (call.context.cancellationSignal?.aborted) return emptyExchange('retry', crmFailure('TRANSPORT', operation, 'the operation deadline passed before Zoho CRM answered'));
    const deadline = AbortSignal.timeout(25_000);
    const signal = call.context.cancellationSignal ? AbortSignal.any([deadline, call.context.cancellationSignal]) : deadline;
    let observedStatus = 0;
    const request: ProviderRequest = {
      url: `${session.baseURL}${pathAndQuery}`, method,
      headers: { Authorization: `Zoho-oauthtoken ${session.credentials.accessToken.reveal()}`, Accept: 'application/json', ...(body ? { 'Content-Type': 'application/json' } : {}) },
      maxResponseBytes: this.#maxResponseBytes, maxErrorResponseBytes: 65_536, addressPolicy: this.#apiBaseURL ? 'privateNetworkHTTP' : 'publicHTTPS',
      timeoutMs: 10_000, signal, ...(body ? { body } : {}), onResponseChunk: (_chunk, status) => { observedStatus = status; },
    };
    let response: ProviderResponse;
    try { response = await this.#transport(request); }
    catch (error) {
      const status = error instanceof BodyTooLargeError ? error.status ?? observedStatus : observedStatus;
      const headers = error instanceof BodyTooLargeError ? error.headers ?? {} : {};
      if (error instanceof BodyTooLargeError && status >= 200 && status < 300) return { ...emptyExchange('invalid', crmFailure('RESPONSE_TOO_LARGE', operation, 'Zoho CRM response exceeds the configured maxResponseBytes limit')), status, headers };
      if (error instanceof BodyTooLargeError && status && (status < 200 || status >= 300)) return this.#classify(operation, status, headers, new Uint8Array());
      return { ...emptyExchange('retry', crmFailure('TRANSPORT', operation, status ? 'Zoho CRM response could not be read' : 'Zoho CRM request failed before a response arrived')), status };
    }
    const headers = response.headers as Exchange['headers'];
    if (session.credentials.accessToken.reveal() && new TextDecoder().decode(response.body).includes(session.credentials.accessToken.reveal())) {
      if (response.status >= 200 && response.status < 300) return { ...emptyExchange('invalid', crmFailure('PROTOCOL', operation, 'Zoho CRM response reflected the connection credential')), status: response.status, headers };
      response = { ...response, body: new Uint8Array() };
    }
    if (response.status >= 200 && response.status < 300) {
      const credits = header(headers, 'X-API-CREDITS-REMAINING');
      return { ...emptyExchange('success'), status: response.status, headers, body: response.body, remainingCredits: /^[0-9]{1,12}$/.test(credits) ? credits : '' };
    }
    return this.#classify(operation, response.status, headers, response.body);
  }

  #classify(operation: string, status: number, headers: Exchange['headers'], body: Uint8Array): Exchange {
    const summary = describeError(body);
    const credits = header(headers, 'X-API-CREDITS-REMAINING');
    const result = { ...emptyExchange('rejected'), status, headers, summary, remainingCredits: /^[0-9]{1,12}$/.test(credits) ? credits : '' };
    if (status === 429) return { ...result, outcome: 'retry', retryAfterMs: parseRetryAfter(header(headers, 'Retry-After'), this.#now()), failure: crmFailure('RATE_LIMIT', operation, detailText(summary, 'Zoho CRM rate limited the request (HTTP 429)')) };
    if (status === 408 || status >= 500) return { ...result, outcome: 'retry', retryAfterMs: parseRetryAfter(header(headers, 'Retry-After'), this.#now()), failure: crmFailure('AVAILABILITY', operation, detailText(summary, `Zoho CRM could not complete the request (HTTP ${status})`)) };
    if (status >= 300 && status < 400) return { ...result, failure: crmFailure('PROTOCOL', operation, `Zoho CRM redirected the request (HTTP ${status}); check that the connection uses the account's data center`) };
    return this.rejectRecord(result, operation);
  }

  /** Classify a conclusive record-level rejection inside a successful HTTP response. */
  rejectRecord(exchange: Exchange, operation: string): Exchange {
    const { status, summary } = exchange;
    let kind: Failure['kind'], message: string;
    if (summary.code === 'OAUTH_SCOPE_MISMATCH') {
      const scope: Record<string, string> = { findRecords: 'ZohoCRM.coql.READ or ZohoCRM.modules.READ', listModifiedRecords: 'ZohoCRM.coql.READ or ZohoCRM.modules.READ', getRecord: 'ZohoCRM.modules.READ', upsertRecord: 'ZohoCRM.modules.CREATE', updateRecord: 'ZohoCRM.modules.UPDATE', listModuleFields: 'ZohoCRM.settings.fields.READ' };
      kind = 'AUTHORIZATION'; message = `Zoho CRM refused the request (HTTP ${status}): the token lacks ${scope[operation]}; reconnect and accept every requested scope`;
    } else if (status === 401) { kind = 'AUTHENTICATION'; message = 'Zoho CRM rejected the access token (HTTP 401); reconnect the connection'; }
    else if (status === 403 || ['NO_PERMISSION', 'AUTHORIZATION_FAILED'].includes(summary.code)) { kind = 'AUTHORIZATION'; message = `Zoho CRM refused the request (HTTP ${status}): the connection's user lacks permission`; }
    else if (summary.code === 'DUPLICATE_DATA') { kind = 'CONFLICT'; message = `Zoho CRM found another record with a value of a unique field (HTTP ${status})`; }
    else if (isMissingRecord(summary)) { kind = 'NOT_FOUND'; message = `Zoho CRM found no record with the ID (HTTP ${status})`; }
    else if (status === 404) { kind = 'NOT_FOUND'; message = 'Zoho CRM found no such resource (HTTP 404)'; }
    else if (status === 400 || status === 422 || (status >= 200 && status < 300)) { kind = 'VALIDATION'; message = `Zoho CRM rejected the request (HTTP ${status})`; }
    else { kind = 'PROVIDER_REJECTION'; message = `Zoho CRM rejected the request (HTTP ${status})`; }
    return { ...exchange, outcome: 'rejected', failure: crmFailure(kind, operation, detailText(summary, message)) };
  }

  /** Build a provider-safe observation receipt. */
  receipt(call: Call, result: Exchange, recordId = ''): ReceiptDraft {
    return { callId: call.id, ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}), provider: 'zoho-crm', providerObjectId: recordId, observedAt: this.#now().toISOString(), ...(result.remainingCredits ? { metadata: { remainingCredits: result.remainingCredits } } : {}) };
  }
}
