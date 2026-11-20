// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash, randomBytes } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { QueryRoutesDefinition } from './generated/definitions.js';
import type { Config, Credentials, QueryRoutesInput, RouteEvent, RouteQueryResult } from './generated/models.js';

/** Optional sender, signature clock, and request ID source for tests. */
export interface SFExpressClientOptions {
  /** Send one bounded provider request. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Return the signature timestamp. */ now?: () => Date;
  /** Fill one UUID source with random bytes. */ random?: (count: number) => Uint8Array;
}

/** Read standard route events for an owned SF Express waybill. */
export class SFExpressClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;
  private readonly random: (count: number) => Uint8Array;
  /** Validate immutable request settings. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: SFExpressClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('SF Express configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl ?? 'https://bspgw.sf-express.com').replace(/\/$/, '');
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > 16 << 20) throw new TypeError('SF Express request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
    this.random = options.random ?? randomBytes;
  }
  /** Bind the route-history Query. */ queryRoutes(): QueryRoutesOperation { return new QueryRoutesOperation(this); }
  /** Sign and dispatch one route query. */
  async request(call: Call, input: QueryRoutesInput): Promise<{ response?: ProviderResponse; error?: 'input' | 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    if (!input || !validWaybill(input.waybillNumber)) return { error: 'input', retryAfterMs: 0 };
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    const partnerID = credentials.partnerId;
    const checkword = credentials.checkword instanceof SecretString ? credentials.checkword.reveal() : '';
    if (!validPartnerID(partnerID) || !validCheckword(checkword)) return { error: 'credential', retryAfterMs: 0 };
    const message = JSON.stringify({ language: '0', trackingType: '1', trackingNumber: [input.waybillNumber], methodType: '1' });
    const timestamp = String(Math.floor(this.now().getTime() / 1000));
    let requestID: string;
    try { requestID = newRequestID(this.random(16)); }
    catch { return { error: 'transport', retryAfterMs: 0 }; }
    const escaped = new URLSearchParams({ value: message + timestamp + checkword }).toString().slice('value='.length);
    const msgDigest = createHash('md5').update(escaped).digest('base64');
    const form = new URLSearchParams({ partnerID, requestID, serviceCode: 'EXP_RECE_SEARCH_ROUTES', timestamp, msgData: message, msgDigest });
    const request: ProviderRequest = { url: `${this.baseURL}/std/service`, method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded; charset=UTF-8', Accept: 'application/json' }, body: new TextEncoder().encode(form.toString()), timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    const retryAfter = Object.entries(response.headers).find(([name]) => name.toLowerCase() === 'retry-after')?.[1];
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(Array.isArray(retryAfter) ? retryAfter[0] ?? '' : retryAfter ?? '') };
  }
}

/** Classify route-query responses and retryable outcomes. */
export class QueryRoutesOperation implements Query<QueryRoutesInput, RouteQueryResult> {
  /** Bind the validated client. */ constructor(private readonly client: SFExpressClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return QueryRoutesDefinition; }
  /** Return bounded public route event times and codes. */
  async invoke(call: Call, input: QueryRoutesInput): Promise<QueryAttempt<RouteQueryResult>> {
    const exchange = await this.client.request(call, input);
    if (exchange.error === 'input') return branch('defect', 'VALIDATION');
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (status !== 200) return branch('providerRejected', 'PROVIDER_REJECTION');
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let envelope: unknown;
    try { envelope = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(envelope) || typeof envelope.apiResultCode !== 'string' || !envelope.apiResultCode) return branch('invalidResponse', 'PROTOCOL');
    const code = envelope.apiResultCode;
    if (['A1005', 'A1009', 'A1099'].includes(code)) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (code !== 'A1000') return branch('providerRejected', 'PROVIDER_REJECTION');
    if (typeof envelope.apiResultData !== 'string') return branch('invalidResponse', 'PROTOCOL');
    let business: unknown;
    try { business = JSON.parse(envelope.apiResultData) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(business) || typeof business.success !== 'boolean') return branch('invalidResponse', 'PROTOCOL');
    if (!business.success) return ['A1005', 'A1009', 'A1099'].includes(String(business.errorCode)) ? retry('AVAILABILITY', exchange.retryAfterMs) : branch('providerRejected', 'PROVIDER_REJECTION');
    if (!isRecord(business.msgData) || !Array.isArray(business.msgData.routeResps) || business.msgData.routeResps.length !== 1) return branch('invalidResponse', 'PROTOCOL');
    const routeResponse = business.msgData.routeResps[0];
    if (!isRecord(routeResponse) || routeResponse.mailNo !== input.waybillNumber || !Array.isArray(routeResponse.routes) || routeResponse.routes.length > 100) return branch('invalidResponse', 'PROTOCOL');
    const routes: RouteEvent[] = [];
    for (const event of routeResponse.routes) {
      if (!isRecord(event) || typeof event.acceptTime !== 'string' || !event.acceptTime || typeof event.remark !== 'string' || !event.remark || typeof event.opcode !== 'string' || !event.opcode) return branch('invalidResponse', 'PROTOCOL');
      routes.push({ occurredAt: event.acceptTime, opcode: event.opcode });
    }
    return newQueryBranch('found', { waybillNumber: input.waybillNumber, routes });
  }
}
function newRequestID(random: Uint8Array): string {
  if (random.length !== 16) throw new TypeError('request ID source must return 16 bytes');
  const bytes = Uint8Array.from(random);
  bytes[6] = ((bytes[6] ?? 0) & 0x0f) | 0x40;
  bytes[8] = ((bytes[8] ?? 0) & 0x3f) | 0x80;
  const hex = Buffer.from(bytes).toString('hex');
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}
function validWaybill(value: unknown): value is string { return typeof value === 'string' && /^[0-9A-Z]{8,30}$/.test(value); }
function validPartnerID(value: unknown): value is string { return typeof value === 'string' && !!value && value.length <= 64 && [...value].every(character => character.charCodeAt(0) > 32 && character.charCodeAt(0) < 127); }
function validCheckword(value: unknown): value is string { return typeof value === 'string' && !!value && value.length <= 2048 && value.trim() === value; }
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'sfexpress', operation: 'queryRoutes', kind, message: 'SF Express route query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<RouteQueryResult> { return newQueryBranch(name, { waybillNumber: '', routes: [] }, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<RouteQueryResult> { return newQueryRetry(failure(kind), delay); }
