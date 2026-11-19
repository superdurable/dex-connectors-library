// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { QueryTrackingDefinition } from './generated/definitions.js';
import type { Config, Credentials, QueryTrackingInput, QueryTrackingOutput, TrackingEvent } from './generated/models.js';

const emptyOutput: QueryTrackingOutput = { carrierCode: '', trackingNumber: '', state: '', events: [] };
/** Send a Kuaidi100 request through a caller-owned transport. */
export type TrackingRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Optional transport dependency for provider tests. */
export interface Kuaidi100ClientOptions { /** Request sender; production uses the Connector SDK transport. */ send?: TrackingRequestSender; }
/** Resolve Kuaidi100 credentials for each tracking query. */
export class Kuaidi100Client {
  readonly baseUrl: string;
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly credentials: CredentialProvider<Credentials>;
  readonly send: TrackingRequestSender;
  /** Validate configuration without contacting Kuaidi100. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: Kuaidi100ClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Kuaidi100 configuration and credential source are required');
    this.baseUrl = validateBaseURL(config.baseUrl || 'https://poll.kuaidi100.com').replace(/\/$/, '');
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Kuaidi100 timeout and response limit must be positive');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the tracking Query. */
  queryTracking(): QueryTrackingOperation { return new QueryTrackingOperation(this); }
}
/** Read one carrier waybill through the instant-query service. */
export class QueryTrackingOperation implements Query<QueryTrackingInput, QueryTrackingOutput> {
  /** Bind an authenticated Kuaidi100 client. */
  constructor(private readonly client: Kuaidi100Client) {}
  /** Return the stable generated Query contract. */
  definition(): QueryDefinition { return QueryTrackingDefinition; }
  /** Sign, send, and classify one tracking request. */
  async invoke(call: Call, input: QueryTrackingInput): Promise<QueryAttempt<QueryTrackingOutput>> {
    if (!/^[a-z][a-z0-9_]{0,31}$/.test(input.carrierCode ?? '') || !/^[A-Za-z0-9_-]{6,32}$/.test(input.trackingNumber ?? '') || (input.phone && !/^[0-9+*#-]{4,32}$/.test(input.phone))) return branch('defect', 'VALIDATION', 'invalid carrier code, tracking number, or phone');
    let credentials: Credentials;
    try { credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION', 'connection credentials are unavailable or invalid'); }
    const customer = credentials.customer instanceof SecretString ? credentials.customer.reveal() : '';
    const key = credentials.key instanceof SecretString ? credentials.key.reveal() : '';
    if (!customer || !key || customer.length > 512 || key.length > 512 || /\s/.test(customer) || /\s/.test(key)) return branch('defect', 'AUTHENTICATION', 'connection credentials are unavailable or invalid');
    const param = JSON.stringify({ com: input.carrierCode, num: input.trackingNumber, ...(input.phone ? { phone: input.phone } : {}), show: '0', order: 'desc' });
    const sign = createHash('md5').update(param + key + customer).digest('hex').toUpperCase();
    const form = new URLSearchParams({ customer, sign, param });
    let response: ProviderResponse;
    try {
      response = await this.client.send({ url: `${this.client.baseUrl}/poll/query.do`, method: 'POST',
        headers: { Accept: 'application/json', 'Content-Type': 'application/x-www-form-urlencoded' },
        body: new TextEncoder().encode(form.toString()), timeoutMs: this.client.timeoutMs,
        maxResponseBytes: this.client.maxResponseBytes, addressPolicy: this.client.baseUrl === 'https://poll.kuaidi100.com' ? 'publicHTTPS' : 'privateNetworkHTTP',
        signal: call.context.cancellationSignal });
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        if (error.status === 429 || error.status === 408 || error.status >= 500) return retry(error.status === 429 ? 'RATE_LIMIT' : 'AVAILABILITY', 'Kuaidi100 is temporarily unavailable');
        return branch('invalidResponse', 'RESPONSE_TOO_LARGE', 'Kuaidi100 response exceeded the configured limit');
      }
      return retry('AVAILABILITY', 'Kuaidi100 request failed');
    }
    if (response.status === 429) return retry('RATE_LIMIT', 'Kuaidi100 rate limited the request');
    if (response.status === 408 || response.status >= 500) return retry('AVAILABILITY', 'Kuaidi100 is temporarily unavailable');
    let document: unknown;
    try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL', 'Kuaidi100 returned an invalid response'); }
    if (!isRecord(document)) return branch('invalidResponse', 'PROTOCOL', 'Kuaidi100 returned an invalid response');
    if (document.result === false) {
      if (document.returnCode === '501' || document.returnCode === '502') return retry('AVAILABILITY', 'Kuaidi100 is temporarily unavailable');
      if (document.returnCode === '504') return branch('providerRejected', 'RATE_LIMIT', 'Kuaidi100 locked this waybill after excessive queries');
      if (document.returnCode === '503' || document.returnCode === '601') return branch('providerRejected', 'AUTHENTICATION', 'Kuaidi100 rejected account credentials');
      return branch('providerRejected', 'PROVIDER_REJECTION', 'Kuaidi100 rejected the tracking query');
    }
    if (response.status !== 200) return branch('providerRejected', 'PROVIDER_REJECTION', `Kuaidi100 rejected HTTP ${response.status}`);
    if (document.status !== '200' || document.com !== input.carrierCode || document.nu !== input.trackingNumber || typeof document.state !== 'string' || !document.state || !Array.isArray(document.data)) return branch('invalidResponse', 'PROTOCOL', 'Kuaidi100 returned incomplete or mismatched tracking data');
    const events: TrackingEvent[] = [];
    for (const trace of document.data as unknown[]) {
      if (!isRecord(trace) || typeof trace.time !== 'string' || !trace.time || typeof trace.context !== 'string' || !trace.context) return branch('invalidResponse', 'PROTOCOL', 'Kuaidi100 returned an incomplete tracking event');
      events.push({ time: trace.time, content: trace.context });
    }
    return newQueryBranch('retrieved', { carrierCode: document.com, trackingNumber: document.nu, state: document.state, events });
  }
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'kuaidi100', operation: 'queryTracking', kind, message }; }
function branch(id: string, kind: Failure['kind'], message: string): QueryAttempt<QueryTrackingOutput> { return newQueryBranch(id, emptyOutput, {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string): QueryAttempt<QueryTrackingOutput> { return newQueryRetry(failure(kind, message), 30 * 60_000); }
