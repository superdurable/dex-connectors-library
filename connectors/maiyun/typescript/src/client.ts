// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { LookupIPLocationDefinition } from './generated/definitions.js';
import type { Config, Credentials, LookupIPLocationInput, LookupIPLocationOutput } from './generated/models.js';

const endpoint = 'https://lts.maiyun.net/api/service/ip';
const emptyOutput: LookupIPLocationOutput = { country: '', province: '', city: '', district: '' };
/** Optional HTTP sender for deterministic provider tests. */
export interface MaiyunClientOptions { /** Sender; defaults to the Connector SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>; }
/** Read the IP location with private credentials resolved per call. */
export class MaiyunClient {
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Validate local settings without contacting Maiyun. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: MaiyunClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Maiyun configuration and credential source are required');
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 65_536;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('Maiyun request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the read-only IP-location Query. */ lookupIPLocation(): LookupIPLocationOperation { return new LookupIPLocationOperation(this); }
  /** Dispatch one bounded provider request. */
  async request(call: Call, input: LookupIPLocationInput): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    const apiKey = credentials.token instanceof SecretString ? credentials.token.reveal() : '';
    if (!/^[!-~]+$/.test(apiKey) || apiKey.length > 1024) return { error: 'credential', retryAfterMs: 0 };
    const request: ProviderRequest = { url: endpoint, method: 'POST', headers: { Accept: 'application/json', 'Content-Type': 'application/json', Authorization: `Bearer ${apiKey}` }, body: new TextEncoder().encode(JSON.stringify({ ip: input.ipAddress })), timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
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
/** Classify Maiyun IP-location results without retaining secrets or metadata. */
export class LookupIPLocationOperation implements Query<LookupIPLocationInput, LookupIPLocationOutput> {
  /** Bind one validated client. */ constructor(private readonly client: MaiyunClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return LookupIPLocationDefinition; }
  /** Read and validate the IP location. */
  async invoke(call: Call, _input: LookupIPLocationInput): Promise<QueryAttempt<LookupIPLocationOutput>> {
    if (!isPublicIPv4(_input.ipAddress)) return branch('defect', 'VALIDATION');
    const exchange = await this.client.request(call, _input);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    let document: unknown;
    try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(document) || typeof document.result !== 'number' || !Number.isInteger(document.result)) return branch('invalidResponse', 'PROTOCOL');
    const result = document.result;
    if (result === -429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (result <= -500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (result !== 1) return branch('providerRejected', result === -401 || result === -402 ? 'AUTHENTICATION' : result === -403 ? 'AUTHORIZATION' : result === -10 ? 'QUOTA_EXHAUSTED' : 'PROVIDER_REJECTION');
    if (typeof document.country !== 'string' || !document.country.trim() || typeof document.city !== 'string' || !document.city.trim()) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('found', { country: document.country, province: typeof document.province === 'string' ? document.province : '', city: document.city, district: typeof document.district === 'string' ? document.district : '' });
  }
}
function isPublicIPv4(address: unknown): address is string {
  if (typeof address !== 'string' || !/^(?:\d{1,3}\.){3}\d{1,3}$/.test(address)) return false;
  const parts = address.split('.').map(Number);
  if (parts.some(part => !Number.isInteger(part) || part < 0 || part > 255)) return false;
  const [first, second] = parts;
  return first !== 0 && first !== 10 && first !== 127 && first !== undefined && first < 224 && !(first === 169 && second === 254) && !(first === 172 && second !== undefined && second >= 16 && second <= 31) && !(first === 192 && second === 168);
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'maiyun', operation: 'lookupIPLocation', kind, message: 'Maiyun provider request failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<LookupIPLocationOutput> { return newQueryBranch(name, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<LookupIPLocationOutput> { return newQueryRetry(failure(kind), delay); }
