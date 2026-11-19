// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { QueryTrackingDefinition } from './generated/definitions.js';
import type { Config, Credentials, QueryTrackingInput, QueryTrackingOutput, TrackingEvent } from './generated/models.js';

const emptyOutput: QueryTrackingOutput = { carrierCode: '', trackingNumber: '', state: '', events: [] };
/** Send a KuaidiBird request through a caller-owned transport. */
export type TrackingRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Optional transport dependency for provider tests. */
export interface KuaidibirdClientOptions { /** Request sender; production uses the Connector SDK transport. */ send?: TrackingRequestSender; }
/** Resolve KuaidiBird credentials for each tracking query. */
export class KuaidibirdClient {
  readonly baseUrl: string;
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly credentials: CredentialProvider<Credentials>;
  readonly send: TrackingRequestSender;
  /** Validate configuration without contacting KuaidiBird. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: KuaidibirdClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('KuaidiBird configuration and credential source are required');
    this.baseUrl = validateBaseURL(config.baseUrl || 'https://api.kdniao.com').replace(/\/$/, '');
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('KuaidiBird timeout and response limit must be positive');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the tracking Query. */
  queryTracking(): QueryTrackingOperation { return new QueryTrackingOperation(this); }
}
/** Read one carrier waybill through the instant-query service. */
export class QueryTrackingOperation implements Query<QueryTrackingInput, QueryTrackingOutput> {
  /** Bind an authenticated KuaidiBird client. */
  constructor(private readonly client: KuaidibirdClient) {}
  /** Return the stable generated Query contract. */
  definition(): QueryDefinition { return QueryTrackingDefinition; }
  /** Sign, send, and classify one tracking request. */
  async invoke(call: Call, input: QueryTrackingInput): Promise<QueryAttempt<QueryTrackingOutput>> {
    if (!/^[A-Z][A-Z0-9]{1,19}$/.test(input.carrierCode ?? '') || !/^[A-Za-z0-9_-]{6,30}$/.test(input.trackingNumber ?? '')) return branch('defect', 'VALIDATION', 'invalid carrier code or tracking number');
    let credentials: Credentials;
    try { credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION', 'connection credentials are unavailable or invalid'); }
    const businessId = credentials.businessId instanceof SecretString ? credentials.businessId.reveal() : '';
    const apiKey = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    if (!/^[0-9]{1,20}$/.test(businessId) || !apiKey || apiKey.length > 512 || /\s/.test(apiKey)) return branch('defect', 'AUTHENTICATION', 'connection credentials are unavailable or invalid');
    const requestData = JSON.stringify({ ShipperCode: input.carrierCode, LogisticCode: input.trackingNumber });
    const digest = createHash('md5').update(requestData + apiKey).digest('hex');
    const form = new URLSearchParams({ RequestData: requestData, EBusinessID: businessId, RequestType: '1002', DataSign: Buffer.from(digest).toString('base64'), DataType: '2' });
    let response: ProviderResponse;
    try {
      response = await this.client.send({ url: `${this.client.baseUrl}/Ebusiness/EbusinessOrderHandle.aspx`, method: 'POST',
        headers: { Accept: 'application/json', 'Content-Type': 'application/x-www-form-urlencoded;charset=utf-8' },
        body: new TextEncoder().encode(form.toString()), timeoutMs: this.client.timeoutMs,
        maxResponseBytes: this.client.maxResponseBytes, addressPolicy: this.client.baseUrl === 'https://api.kdniao.com' ? 'publicHTTPS' : 'privateNetworkHTTP',
        signal: call.context.cancellationSignal });
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        if (error.status === 429 || error.status === 408 || error.status >= 500) return retry(error.status === 429 ? 'RATE_LIMIT' : 'AVAILABILITY', 'KuaidiBird is temporarily unavailable', getRetryAfter(error.headers ?? {}));
        return branch('invalidResponse', 'RESPONSE_TOO_LARGE', 'KuaidiBird response exceeded the configured limit');
      }
      return retry('AVAILABILITY', 'KuaidiBird request failed');
    }
    const delay = getRetryAfter(response.headers);
    if (response.status === 429) return retry('RATE_LIMIT', 'KuaidiBird rate limited the request', delay);
    if (response.status === 408 || response.status >= 500) return retry('AVAILABILITY', 'KuaidiBird is temporarily unavailable', delay);
    let document: unknown;
    try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL', 'KuaidiBird returned an invalid response'); }
    if (!isRecord(document) || typeof document.Success !== 'boolean') return branch('invalidResponse', 'PROTOCOL', 'KuaidiBird returned an invalid response');
    if (response.status !== 200) return branch('providerRejected', 'PROVIDER_REJECTION', `KuaidiBird rejected HTTP ${response.status}`);
    if (!document.Success) return branch('providerRejected', 'PROVIDER_REJECTION', 'KuaidiBird could not return this waybill');
    const state = typeof document.State === 'string' && document.State ? document.State : typeof document.State === 'number' && Number.isFinite(document.State) ? String(document.State) : '';
    if (!state || document.EBusinessID !== businessId || document.ShipperCode !== input.carrierCode || document.LogisticCode !== input.trackingNumber || !Array.isArray(document.Traces)) return branch('invalidResponse', 'PROTOCOL', 'KuaidiBird returned incomplete or mismatched tracking data');
    const events: TrackingEvent[] = [];
    for (const trace of document.Traces as unknown[]) {
      if (!isRecord(trace) || typeof trace.AcceptTime !== 'string' || !trace.AcceptTime || typeof trace.AcceptStation !== 'string' || !trace.AcceptStation) return branch('invalidResponse', 'PROTOCOL', 'KuaidiBird returned an incomplete tracking event');
      events.push({ time: trace.AcceptTime, content: trace.AcceptStation });
    }
    return newQueryBranch('retrieved', { carrierCode: input.carrierCode, trackingNumber: input.trackingNumber, state, events });
  }
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function getRetryAfter(headers: ProviderResponse['headers']): number { const value = headers['retry-after']; return parseRetryAfter(Array.isArray(value) ? value[0] ?? '' : value ?? ''); }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'kuaidibird', operation: 'queryTracking', kind, message }; }
function branch(id: string, kind: Failure['kind'], message: string): QueryAttempt<QueryTrackingOutput> { return newQueryBranch(id, emptyOutput, {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delayMs = 0): QueryAttempt<QueryTrackingOutput> { return newQueryRetry(failure(kind, message), delayMs); }
