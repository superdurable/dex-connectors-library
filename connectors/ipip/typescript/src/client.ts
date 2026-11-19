// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { isIP } from 'node:net';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { LookupIPLocationDefinition } from './generated/definitions.js';
import type { Config, Credentials, LookupIPLocationInput, LookupIPLocationOutput } from './generated/models.js';

const emptyOutput: LookupIPLocationOutput = { countryName: '', regionName: '', cityName: '', countryCode: '' };
const endpoint = 'https://ipapi.ipip.net/v2/query/';

/** Optional provider transport used for deterministic tests. */
export interface IPIPClientOptions {
  /** Sender; production uses the Connector SDK HTTP transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

/** Resolve one secret token per read-only IP location Query. */
export class IPIPClient {
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate request limits without contacting IPIP. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: IPIPClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('IPIP configuration and credential source are required');
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('IPIP request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the location lookup Query. */ lookupIPLocation(): LookupIPLocationOperation { return new LookupIPLocationOperation(this); }

  /** Dispatch one authenticated provider request. */
  async request(call: Call, ipAddress: string): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let token = '';
    try {
      const resolved = await this.credentials.resolve(call, call.context.cancellationSignal);
      token = resolved.token instanceof SecretString ? resolved.token.reveal() : '';
    } catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!token || token.length > 1024 || !/^[!-~]+$/.test(token)) return { error: 'credential', retryAfterMs: 0 };
    const request: ProviderRequest = { url: `${endpoint}${encodeURIComponent(ipAddress)}?token=${encodeURIComponent(token)}`, method: 'GET', headers: { Accept: 'application/json' }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
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

/** Query coarse IP location fields through IPIP's v2 API. */
export class LookupIPLocationOperation implements Query<LookupIPLocationInput, LookupIPLocationOutput> {
  /** Bind a validated IPIP client. */ constructor(private readonly client: IPIPClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return LookupIPLocationDefinition; }
  /** Classify a provider attempt and preserve secret safety. */
  async invoke(call: Call, input: LookupIPLocationInput): Promise<QueryAttempt<LookupIPLocationOutput>> {
    if (!input || isIP(input.ipAddress) === 0) return branch('defect', 'VALIDATION');
    const exchange = await this.client.request(call, input.ipAddress);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let document: unknown;
    try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(document) || typeof document.ret !== 'string' || !document.ret) return branch('invalidResponse', 'PROTOCOL');
    if (document.ret === 'err') return branch('providerRejected', 'PROVIDER_REJECTION');
    const info = isRecord(document.data) && isRecord(document.data.info) ? document.data.info : undefined;
    if (document.ret !== 'ok' || !info || typeof info.country_name !== 'string' || !info.country_name || typeof info.region_name !== 'string' || typeof info.city_name !== 'string' || typeof info.country_code !== 'string' || !info.country_code) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('found', { countryName: info.country_name, regionName: info.region_name, cityName: info.city_name, countryCode: info.country_code });
  }
}

function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'ipip', operation: 'lookupIPLocation', kind, message: 'IPIP location lookup failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<LookupIPLocationOutput> { return newQueryBranch(name, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<LookupIPLocationOutput> { return newQueryRetry(failure(kind), delay); }
