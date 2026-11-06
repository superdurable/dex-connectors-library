// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { RetryError, resolveCredential, resolveCredentialAfterRejection, type Call, type CredentialProvider, type CredentialRefreshDriver, type Failure, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, MaxErrorBodyBytes, ProviderTransportError, isHeaderSafeCredential, parseRetryAfter, readErrorTokens, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { Config, Credentials } from './generated/models.js';

export type GraphTransport = (request: ProviderRequest) => Promise<ProviderResponse>;
export type GraphOutcome = 'succeeded' | 'notFound' | 'retry' | 'rejected' | 'invalid' | 'ambiguous' | 'defect';
export interface GraphExchange { outcome: GraphOutcome; status: number; body: Uint8Array; requestId: string; errorCode: string; failure?: Failure; retryAfterMs: number }
export interface GraphRequest { method: string; path?: string; absoluteURL?: string; query?: readonly (readonly [string,string])[]; payload?: unknown; prefersTextBody?: boolean }
const emptyBody = new Uint8Array();
const graphURL = 'https://graph.microsoft.com/v1.0';

/** Reproduce Go url.QueryEscape with OData spaces expressed as %20. */
export function escapeQueryComponent(value: string): string {
  return encodeURIComponent(value).replace(/[!'()~*]/g, character => character === '~' ? '~' : character === '*' ? '%2A' : '%' + character.charCodeAt(0).toString(16).toUpperCase()).replace(/%20/g, '%20');
}
/** Escape a single path segment as Go url.PathEscape does. */
export function escapePathSegment(value: string): string { return encodeURIComponent(value).replace(/[!'()*]/g, character => '%' + character.charCodeAt(0).toString(16).toUpperCase()).replace(/%3A/gi, ':').replace(/%40/gi, '@').replace(/%24/gi, '$').replace(/%26/gi, '&').replace(/%2B/gi, '+').replace(/%3D/gi, '='); }
export function graphFailure(operation: string, kind: Failure['kind'], message: string): Failure { return { provider: 'microsoft-graph', operation, kind, message }; }
export function zeroExchange(outcome: GraphOutcome, failure: Failure): GraphExchange { return { outcome, failure, status: 0, body: emptyBody, requestId: '', errorCode: '', retryAfterMs: 0 }; }
export function graphReceipt(call: Call, exchange: GraphExchange, objectId: string, now: () => Date): ReceiptDraft {
  return { callId: call.id, ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}), provider: 'microsoft-graph', providerObjectId: objectId, providerRequestId: exchange.requestId, observedAt: now().toISOString().replace(/\.000Z$/, 'Z').replace(/(\.\d*[1-9])0+Z$/, '$1Z') };
}
function header(headers: ProviderResponse['headers'], name: string): string { const raw = headers[name.toLowerCase()] ?? headers[name]; return Array.isArray(raw) ? raw[0] ?? '' : raw ?? ''; }
function classifyStatus(operation: string, status: number, errorCode: string, retryAfterMs: number, requestId: string): GraphExchange {
  const describe = (outcome: GraphOutcome, kind: Failure['kind'], message: string): GraphExchange => ({ outcome, status, body: emptyBody, requestId, errorCode, retryAfterMs, failure: graphFailure(operation, kind, `${message} (HTTP ${status}${errorCode ? ' ' + errorCode : ''})`) });
  if (status === 429 || status === 509) return describe('retry', 'RATE_LIMIT', 'Microsoft Graph throttled the request');
  if (status === 408) return describe('retry', 'AVAILABILITY', 'Microsoft Graph timed out waiting for the request');
  if (status === 409 || status === 423) return describe('retry', 'CONFLICT', 'Microsoft Graph reported a conflicting change to the item');
  if (status === 501) return describe('rejected', 'PROVIDER_REJECTION', 'Microsoft Graph does not implement the request');
  if (status === 507) return describe('rejected', 'QUOTA_EXHAUSTED', 'the mailbox is over its storage quota');
  if (status >= 500) return describe('ambiguous', 'AVAILABILITY', 'Microsoft Graph failed or is temporarily unavailable');
  if (status === 401) return describe('rejected', 'AUTHENTICATION', 'Microsoft Graph rejected the access token');
  if (status === 403 && errorCode.includes('Quota')) return describe('rejected', 'QUOTA_EXHAUSTED', 'Microsoft Graph refused the request for a mailbox quota');
  if (status === 403) return describe('rejected', 'AUTHORIZATION', 'Microsoft Graph denied access to the mailbox or item');
  if ((status === 404 && (!errorCode || errorCode === 'ErrorItemNotFound')) || (status === 400 && errorCode === 'ErrorInvalidIdMalformed')) return describe('notFound', 'NOT_FOUND', 'Microsoft Graph found no such item in the mailbox');
  if (status === 404) return describe('rejected', 'NOT_FOUND', 'Microsoft Graph found no such mailbox or resource');
  if (status >= 300 && status < 400) return describe('rejected', 'PROTOCOL', 'Microsoft Graph redirected the request');
  if ([400, 413, 422].includes(status)) return describe('rejected', 'VALIDATION', 'Microsoft Graph rejected the request');
  return describe('rejected', 'PROVIDER_REJECTION', 'Microsoft Graph rejected the request');
}
function classifyResponse(operation: string, response: ProviderResponse, secret: string, now: Date): GraphExchange {
  const requestIdValue = header(response.headers, 'request-id');
  const requestId = /^[A-Za-z0-9-]{1,64}$/.test(requestIdValue) ? requestIdValue : '';
  if (response.status < 200 || response.status >= 300) {
    const body = response.body.subarray(0, MaxErrorBodyBytes);
    const errorCode = readErrorTokens(body, ['/error/code', '/error/innerError/code']).find(value => !secret || !value.includes(secret)) ?? '';
    return classifyStatus(operation, response.status, errorCode, parseRetryAfter(header(response.headers, 'retry-after'), now), requestId);
  }
  if (secret && Buffer.from(response.body).includes(Buffer.from(secret))) return { outcome: 'invalid', status: response.status, body: emptyBody, requestId, errorCode: '', retryAfterMs: 0, failure: graphFailure(operation, 'PROTOCOL', 'the Microsoft Graph response reflected the connection credential') };
  return { outcome: 'succeeded', status: response.status, body: response.body, requestId, errorCode: '', retryAfterMs: 0 };
}
/** Credential-safe HTTP session for one Dex operation attempt. */
export class GraphSession {
  readonly #call: Call; readonly #transport: GraphTransport; readonly #refreshDriver: CredentialRefreshDriver<Credentials>; readonly #source: CredentialProvider<Credentials>;
  readonly #now: () => Date; readonly #root: string; readonly #maxResponseBytes: number; readonly #timeoutMs: number;
  readonly operation: string; #credentials: Credentials; #refreshedAfterRejection = false;
  private constructor(call: Call, operation: string, credentials: Credentials, root: string, config: Config, source: CredentialProvider<Credentials>, driver: CredentialRefreshDriver<Credentials>, transport: GraphTransport, now: () => Date, timeoutMs: number) {
    this.#call = call; this.operation = operation; this.#credentials = credentials; this.#root = root; this.#maxResponseBytes = config.maxResponseBytes ?? 4194304;
    this.#source = source; this.#refreshDriver = driver; this.#transport = transport; this.#now = now; this.#timeoutMs = timeoutMs;
  }
  static async open(call: Call, operation: string, config: Config, source: CredentialProvider<Credentials>, driver: CredentialRefreshDriver<Credentials>, transport: GraphTransport, now: () => Date, timeoutMs: number): Promise<GraphSession | GraphExchange> {
    let credentials: Credentials;
    try { credentials = await resolveCredential(source, call, driver, call.context.cancellationSignal); }
    catch (error) {
      if (error && typeof error === 'object' && 'name' in error && error.name === 'ReauthorizationRequiredError') return zeroExchange('rejected', graphFailure(operation, 'AUTHENTICATION', 'Microsoft authorization must be renewed: authorize the connection again or replace the client secret'));
      if (error instanceof RetryError) return zeroExchange('retry', graphFailure(operation, 'AVAILABILITY', 'the Microsoft token endpoint is temporarily unavailable'));
      return zeroExchange('defect', graphFailure(operation, 'AUTHENTICATION', 'Outlook Mail connection credentials are unavailable'));
    }
    if (!credentials.accessToken?.reveal() || !isHeaderSafeCredential(credentials.accessToken.reveal())) return zeroExchange('defect', graphFailure(operation, 'AUTHENTICATION', 'Outlook Mail connection credentials are unavailable'));
    const method = credentials.authMethodId || 'microsoft-oauth';
    let root: string;
    if (method === 'microsoft-oauth') root = '/me';
    else if (method === 'app-only') {
      if (!config.mailbox?.trim()) return zeroExchange('defect', graphFailure(operation, 'VALIDATION', 'an app-only connection needs the mailbox configuration field'));
      root = '/users/' + escapePathSegment(config.mailbox.trim());
    } else return zeroExchange('defect', graphFailure(operation, 'VALIDATION', 'Outlook Mail authorization method is not supported'));
    return new GraphSession(call, operation, credentials, root, config, source, driver, transport, now, timeoutMs);
  }
  receipt(exchange: GraphExchange, objectId = ''): ReceiptDraft { return graphReceipt(this.#call, exchange, objectId, this.#now); }
  async exchange(request: GraphRequest): Promise<GraphExchange> {
    let result = await this.#send(request);
    if (result.status !== 401 || this.#refreshedAfterRejection || !('resolveAfterRejection' in this.#source)) return result;
    this.#refreshedAfterRejection = true;
    try { const replacement = await resolveCredentialAfterRejection(this.#source, this.#call, this.#refreshDriver, this.#call.context.cancellationSignal);
      if (!replacement.accessToken?.reveal() || replacement.authMethodId !== this.#credentials.authMethodId) return result;
      this.#credentials = replacement; result = await this.#send(request);
    } catch { /* The original 401 is the conclusive answer. */ }
    return result;
  }
  async #send(request: GraphRequest): Promise<GraphExchange> {
    const token = this.#credentials.accessToken?.reveal() ?? '';
    if (!isHeaderSafeCredential(token)) return zeroExchange('defect', graphFailure(this.operation, 'LOCAL_DEFECT', 'Microsoft Graph request could not be built'));
    const url = request.absoluteURL ?? graphURL + this.#root + (request.path ?? '') + (request.query?.length ? '?' + request.query.map(([key, value]) => `${escapeQueryComponent(key)}=${escapeQueryComponent(value)}`).join('&') : '');
    let body: Uint8Array | undefined;
    try { if (request.payload !== undefined) body = Buffer.from(JSON.stringify(request.payload)); else if (request.method === 'POST') body = emptyBody; }
    catch { return zeroExchange('defect', graphFailure(this.operation, 'LOCAL_DEFECT', 'Microsoft Graph request could not be built')); }
    const headers: Record<string,string> = { Authorization: `Bearer ${token}`, Accept: 'application/json', Prefer: request.prefersTextBody ? ['IdType="ImmutableId"', 'outlook.body-content-type="text"'] as unknown as string : 'IdType="ImmutableId"' };
    if (request.payload !== undefined) headers['Content-Type'] = 'application/json';
    const providerRequest: ProviderRequest = { url, method: request.method, headers, body, maxResponseBytes: this.#maxResponseBytes, maxErrorResponseBytes: MaxErrorBodyBytes, ignoreErrorBodyReadFailure: true, addressPolicy: 'publicHTTPS', timeoutMs: this.#timeoutMs, signal: this.#call.context.cancellationSignal };
    let observedStatus = 0; let observedHeaders: ProviderResponse['headers'] = {};
    providerRequest.onResponseChunk = (_chunk, status, responseHeaders) => { observedStatus = status; observedHeaders = responseHeaders; };
    try { return classifyResponse(this.operation, await this.#transport(providerRequest), token, this.#now()); }
    catch (error) {
      if (error instanceof BodyTooLargeError) {
        const status = error.status ?? observedStatus; const responseHeaders = error.headers ?? observedHeaders;
        if (status >= 200 && status < 300) return { outcome: 'invalid', status, body: emptyBody, requestId: header(responseHeaders, 'request-id'), errorCode: '', retryAfterMs: 0, failure: graphFailure(this.operation, 'RESPONSE_TOO_LARGE', 'the Microsoft Graph response exceeds the configured maxResponseBytes limit') };
        if (status) return classifyStatus(this.operation, status, '', parseRetryAfter(header(responseHeaders, 'retry-after'), this.#now()), header(responseHeaders, 'request-id'));
      }
      if (observedStatus >= 200 && observedStatus < 300) return zeroExchange('ambiguous', graphFailure(this.operation, 'TRANSPORT', 'the Microsoft Graph response was interrupted'));
      if (observedStatus && (observedStatus < 200 || observedStatus >= 300)) return classifyStatus(this.operation, observedStatus, '', parseRetryAfter(header(observedHeaders, 'retry-after'), this.#now()), header(observedHeaders, 'request-id'));
      if (error instanceof ProviderTransportError && error.isProvablyNotSent) return zeroExchange('retry', graphFailure(this.operation, 'TRANSPORT', 'Microsoft Graph could not be reached; no request was sent'));
      return zeroExchange('ambiguous', graphFailure(this.operation, 'TRANSPORT', 'the Microsoft Graph request failed before an answer arrived'));
    }
  }
}
/** Construct OAuth and app-only renewal from generated manifest declarations. */
export function newGraphRefreshDriver(now: () => Date): CredentialRefreshDriver<Credentials> {
  return new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'microsoft-graph', auth: Authorization, credentialFields: CredentialFields, now });
}
export function isGraphExchange(value: GraphSession | GraphExchange): value is GraphExchange { return 'outcome' in value; }
