// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetHolidayStatusDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetHolidayStatusInput, GetHolidayStatusOutput } from './generated/models.js';

const endpoint = 'https://apis.tianapi.com/jiejiari/index';

/** Optional sender for local provider tests. */
export interface TianapiClientOptions {
  /** Sender; omitted uses the SDK HTTP transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

/** Query TianAPI's holiday calendar with credentials resolved per call. */
export class TianapiClient {
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate local request limits without contacting TianAPI. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: TianapiClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('TianAPI configuration and credentials are required');
    this.timeoutMs = config.timeoutMs || 10_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('TianAPI request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the holiday-status Query. */ getHolidayStatus(): GetHolidayStatusOperation { return new GetHolidayStatusOperation(this); }

  /** Send a bounded form POST; the key never enters the URL. */
  async request(call: Call, input: GetHolidayStatusInput): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let key = '';
    try { const credentials = await this.credentials.resolve(call, call.context.cancellationSignal); key = credentials.key instanceof SecretString ? credentials.key.reveal() : ''; }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!key || key.length > 1024 || /[^\x21-\x7e]/.test(key)) return { error: 'credential', retryAfterMs: 0 };
    const form = new URLSearchParams({ key, date: input.date, type: '0' });
    const request: ProviderRequest = { url: endpoint, method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded', Accept: 'application/json' }, body: new TextEncoder().encode(form.toString()), timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** Read one Chinese calendar date's work/rest status. */
export class GetHolidayStatusOperation implements Query<GetHolidayStatusInput, GetHolidayStatusOutput> {
  /** Bind the provider client. */ constructor(private readonly client: TianapiClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return GetHolidayStatusDefinition; }
  /** Classify one TianAPI response using the Go connector contract. */
  async invoke(call: Call, input: GetHolidayStatusInput): Promise<QueryAttempt<GetHolidayStatusOutput>> {
    if (!/^\d{4}-\d{2}-\d{2}$/.test(input.date) || Number.isNaN(Date.parse(input.date + 'T00:00:00Z')) || new Date(input.date + 'T00:00:00Z').toISOString().slice(0, 10) !== input.date) return branch('defect', 'VALIDATION');
    const exchange = await this.client.request(call, input);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isObject(wire) || !Number.isInteger(wire.code)) return branch('invalidResponse', 'PROTOCOL');
    if (wire.code === 130) return retry('RATE_LIMIT');
    if (wire.code === 100 || wire.code === 120) return retry('AVAILABILITY');
    if (wire.code !== 200) return branch('providerRejected', 'PROVIDER_REJECTION');
    const result = isObject(wire.result) ? wire.result : undefined;
    const list = result?.list;
    if (!Array.isArray(list) || list.length !== 1 || !isObject(list[0])) return branch('invalidResponse', 'PROTOCOL');
    const item = list[0];
    if (item.date !== input.date || !Number.isInteger(item.daycode) || typeof item.daycode !== 'number' || item.daycode < 0 || item.daycode > 3 || (item.isnotwork !== 0 && item.isnotwork !== 1)) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('found', { date: item.date, dayCode: item.daycode, isNotWork: item.isnotwork });
  }
}

function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'tianapi', operation: 'getHolidayStatus', kind, message: 'TianAPI holiday query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<GetHolidayStatusOutput> { return newQueryBranch(name, { date: '', dayCode: 0, isNotWork: 0 }, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<GetHolidayStatusOutput> { return newQueryRetry(failure(kind), delay); }
