// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { LookupAreaCodesDefinition } from './generated/definitions.js';
import type { Config, Credentials, LookupAreaCodesInput, AreaCodeMatches } from './generated/models.js';

const emptyOutput: AreaCodeMatches = { matches: [] };
/** Optional provider transport for deterministic tests. */
export interface ShowAPIClientOptions { /** Sender; production uses Connector SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>; }
/** Read provider data through one credentialed Query. */
export class ShowAPIClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Validate connector settings without contacting the provider. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: ShowAPIClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('ShowAPI configuration and credential source are required');
    const origin = new URL(config.baseUrl ?? 'https://route.showapi.com'); if (origin.protocol !== 'https:' || origin.username || origin.password || origin.search || origin.hash) throw new TypeError('ShowAPI base URL is invalid'); this.baseUrl = origin.origin;
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('ShowAPI request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the read-only provider Query. */ lookupAreaCodes(): LookupAreaCodesOperation { return new LookupAreaCodesOperation(this); }
  /** Dispatch one bounded provider request. */
  async request(call: Call, input: LookupAreaCodesInput): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge' | 'input'; retryAfterMs: number }> {
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    const key = credentials.appKey instanceof SecretString ? credentials.appKey.reveal() : '';
    if (!key || key.length > 512 || !/^[!-~]+$/.test(key)) return { error: 'credential', retryAfterMs: 0 };
    const area = input.area?.trim(); if (!area || [...area].length > 100 || /[\r\n]/.test(area)) return { error: 'input', retryAfterMs: 0 };
    const request: ProviderRequest = { url: `${this.baseUrl}/9-3?appKey=${encodeURIComponent(key)}`, method: 'POST', headers: { Accept: 'application/json', 'Content-Type': 'application/x-www-form-urlencoded' }, body: new TextEncoder().encode(new URLSearchParams({ area }).toString()), timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    const retryAfterValue = Object.entries(response.headers).find(([key]) => key.toLowerCase() === 'retry-after')?.[1];
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(Array.isArray(retryAfterValue) ? retryAfterValue[0] ?? '' : retryAfterValue ?? '') };
  }
}
/** Classify the provider response without storing credentials in Dex state. */
export class LookupAreaCodesOperation implements Query<LookupAreaCodesInput, AreaCodeMatches> {
  /** Bind one validated client. */ constructor(private readonly client: ShowAPIClient) {}
  /** Return the generated operation contract. */ definition(): QueryDefinition { return LookupAreaCodesDefinition; }
  /** Perform one safe read and classify its outcome. */
  async invoke(call: Call, input: LookupAreaCodesInput): Promise<QueryAttempt<AreaCodeMatches>> {
    const exchange = await this.client.request(call, input);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'input') return branch('defect', 'VALIDATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : status === 400 || status === 404 ? 'VALIDATION' : 'PROVIDER_REJECTION');
    let document: unknown;
    try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(document) || !Number.isSafeInteger(document.showapi_res_code)) return branch('invalidResponse', 'PROTOCOL');
    if (document.showapi_res_code !== 0) return branch('providerRejected', document.showapi_res_code === -1004 ? 'AUTHENTICATION' : 'PROVIDER_REJECTION');
    const data = document.showapi_res_body; if (!isRecord(data) || !Number.isSafeInteger(data.ret_code)) return branch('invalidResponse', 'PROTOCOL');
    if (data.ret_code !== 0) return branch('providerRejected', 'PROVIDER_REJECTION');
    if (!Array.isArray(data.list) || data.list.length > 100) return branch('invalidResponse', 'PROTOCOL');
    const matches: NonNullable<AreaCodeMatches['matches']> = [];
    for (const entry of data.list) { if (!isRecord(entry) || typeof entry.prov !== 'string' || typeof entry.area !== 'string' || !entry.area || typeof entry.areaCode !== 'string' || !entry.areaCode) return branch('invalidResponse', 'PROTOCOL'); matches.push({ province: entry.prov, area: entry.area, areaCode: entry.areaCode }); }
    return newQueryBranch('found', { matches });
  }
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'showapi', operation: 'lookupAreaCodes', kind, message: 'ShowAPI provider request failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<AreaCodeMatches> { return newQueryBranch(name, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<AreaCodeMatches> { return newQueryRetry(failure(kind), delay); }
