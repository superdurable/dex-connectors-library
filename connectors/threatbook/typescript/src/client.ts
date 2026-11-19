// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { isIP } from 'node:net';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { CheckIPReputationDefinition } from './generated/definitions.js';
import type { Config, Credentials, CheckIPReputationInput, CheckIPReputationOutput } from './generated/models.js';

/** Optional sender for local provider tests. */
export interface ThreatBookClientOptions { /** Sender; omitted uses the SDK HTTP transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>; }

/** Read bounded ThreatBook IP intelligence with credentials resolved per call. */
export class ThreatBookClient {
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Validate local limits without contacting ThreatBook. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: ThreatBookClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('ThreatBook configuration and credentials are required');
    this.timeoutMs = config.timeoutMs || 10_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('ThreatBook request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the IP reputation Query. */ checkIPReputation(): CheckIPReputationOperation { return new CheckIPReputationOperation(this); }
  /** Send one bounded provider request. */
  async request(call: Call, ipAddress: string): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let apiKey = '';
    try { const credentials = await this.credentials.resolve(call, call.context.cancellationSignal); apiKey = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : ''; }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!apiKey || apiKey.length > 1024 || /[^\x21-\x7e]/.test(apiKey)) return { error: 'credential', retryAfterMs: 0 };
    const query = new URLSearchParams({ apikey: apiKey, resource: ipAddress, lang: 'en' });
    const request: ProviderRequest = { url: `https://api.threatbook.cn/v3/scene/ip_reputation?${query}`, method: 'GET', headers: { Accept: 'application/json' }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** Check one inbound IP against ThreatBook's reputation data. */
export class CheckIPReputationOperation implements Query<CheckIPReputationInput, CheckIPReputationOutput> {
  /** Bind the provider client. */ constructor(private readonly client: ThreatBookClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return CheckIPReputationDefinition; }
  /** Classify one provider response using the Go connector contract. */
  async invoke(call: Call, input: CheckIPReputationInput): Promise<QueryAttempt<CheckIPReputationOutput>> {
    if (!isIP(input.ipAddress)) return branch('defect', 'VALIDATION');
    const exchange = await this.client.request(call, input.ipAddress);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { wire = undefined; }
    if (isObject(wire) && Number.isInteger(wire.response_code)) {
      if (wire.response_code === 2) return branch('noData', 'NOT_FOUND');
      if (wire.response_code === -4) return wire.verbose_msg === 'Frequent Limitation' ? retry('RATE_LIMIT', exchange.retryAfterMs) : branch('providerRejected', 'PROVIDER_REJECTION');
      if (wire.response_code === -5 || wire.response_code === -11) return retry('AVAILABILITY', exchange.retryAfterMs);
      if (wire.response_code !== 0) return branch('providerRejected', 'PROVIDER_REJECTION');
      if (status !== 200 || !isObject(wire.data) || Object.keys(wire.data).length !== 1) return branch('invalidResponse', 'PROTOCOL');
      const [address] = Object.keys(wire.data);
      const info = address ? wire.data[address] : undefined;
      if (!address || address !== input.ipAddress || !isObject(info) || typeof info.is_malicious !== 'boolean' || !Array.isArray(info.judgments) || info.judgments.length > 20 || typeof info.confidence_level !== 'string' && info.confidence_level !== undefined || typeof info.severity !== 'string' && info.severity !== undefined || (info.confidence_level as string | undefined ?? '').length > 16 || (info.severity as string | undefined ?? '').length > 16 || info.judgments.some(item => typeof item !== 'string' || !item || item.length > 80)) return branch('invalidResponse', 'PROTOCOL');
      return newQueryBranch('found', { isMalicious: info.is_malicious, confidenceLevel: info.confidence_level ?? '', severity: info.severity ?? '', judgments: info.judgments });
    }
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    return branch('invalidResponse', 'PROTOCOL');
  }
}

function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'threatbook', operation: 'checkIPReputation', kind, message: 'ThreatBook reputation query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<CheckIPReputationOutput> { return newQueryBranch(name, { isMalicious: false, confidenceLevel: '', severity: '', judgments: [] }, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<CheckIPReputationOutput> { return newQueryRetry(failure(kind), delay); }
