// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { LookupIPLocationDefinition } from './generated/definitions.js';
import type { Config, Credentials, LookupIPLocationInput, LookupIPLocationOutput } from './generated/models.js';

const endpoint = 'https://v3.alapi.cn/api/ip';
const emptyOutput: LookupIPLocationOutput = { country: '', province: '', city: '', district: '', operator: '' };

/** Send one ALAPI request through a caller-owned transport. */
export type ALAPIRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;

/** Optional ALAPI transport dependency for local provider tests. */
export interface ALAPIClientOptions {
  /** Request sender; production uses the Connector SDK transport. */
  send?: ALAPIRequestSender;
}

/** Resolve ALAPI credentials for each read-only IP lookup. */
export class ALAPIClient {
  readonly maxResponseBytes: number;
  readonly timeoutMs: number;
  readonly send: ALAPIRequestSender;

  /** Validate settings without contacting ALAPI. */
  constructor(config: Config, readonly credentials: CredentialProvider<Credentials>, options: ALAPIClientOptions = {}) {
    if (!credentials) throw new TypeError('ALAPI credential source is required');
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 65_536;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > 16 * 1024 * 1024) {
      throw new TypeError('ALAPI configuration is invalid');
    }
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the public IPv4 location Query. */
  lookupIPLocation(): LookupIPLocationOperation { return new LookupIPLocationOperation(this); }
}

/** Read one public IPv4 address without storing provider credentials in Flow state. */
export class LookupIPLocationOperation implements Query<LookupIPLocationInput, LookupIPLocationOutput> {
  /** Bind an authenticated ALAPI client. */
  constructor(private readonly client: ALAPIClient) {}

  /** Return the stable generated Query contract. */
  definition(): QueryDefinition { return LookupIPLocationDefinition; }

  /** Send one provider request and classify its result. */
  async invoke(call: Call, input: LookupIPLocationInput): Promise<QueryAttempt<LookupIPLocationOutput>> {
    if (!isPublicIPv4(input.ipAddress)) return branch('defect', 'VALIDATION', 'ALAPI requires one public IPv4 address');
    let token: string;
    try {
      const resolved = await this.client.credentials.resolve(call, call.context.cancellationSignal);
      token = resolved.token instanceof SecretString ? resolved.token.reveal() : '';
    } catch { return branch('defect', 'AUTHENTICATION', 'ALAPI credentials are unavailable or invalid'); }
    if (!token || token.length > 1024 || !/^[!-~]+$/.test(token) || !isHeaderSafeCredential(token)) {
      return branch('defect', 'AUTHENTICATION', 'ALAPI credentials are unavailable or invalid');
    }
    let response: ProviderResponse;
    try {
      response = await this.client.send({
        url: endpoint, method: 'POST', headers: { Accept: 'application/json', 'Content-Type': 'application/json', token },
        body: Buffer.from(JSON.stringify({ ip: input.ipAddress }), 'utf8'),
        maxResponseBytes: this.client.maxResponseBytes, maxErrorResponseBytes: 4096,
        timeoutMs: this.client.timeoutMs, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal,
      });
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status === 200) return branch('invalidResponse', 'RESPONSE_TOO_LARGE', 'ALAPI response exceeded the configured limit');
      return retry('AVAILABILITY', 'ALAPI lookup request failed');
    }
    const retryAfterHeader = response.headers['retry-after'];
    const retryAfterMs = parseRetryAfter(Array.isArray(retryAfterHeader) ? retryAfterHeader[0] ?? '' : retryAfterHeader ?? '');
    if (response.status === 429) return retry('RATE_LIMIT', 'ALAPI rate limited the lookup', retryAfterMs);
    if (response.status === 408 || response.status >= 500) return retry('AVAILABILITY', 'ALAPI lookup service is temporarily unavailable', retryAfterMs);
    if (response.status !== 200) return branch('providerRejected', response.status === 401 ? 'AUTHENTICATION' : response.status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION', `ALAPI rejected lookup with HTTP ${response.status}`);
    let document: unknown;
    try { document = JSON.parse(Buffer.from(response.body).toString('utf8')) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL', 'ALAPI returned invalid lookup data'); }
    if (!isRecord(document) || !Number.isInteger(document.code) || typeof document.success !== 'boolean') return branch('invalidResponse', 'PROTOCOL', 'ALAPI returned invalid lookup data');
    const code = document.code as number;
    if (code === 429) return retry('RATE_LIMIT', 'ALAPI rate limited the lookup', retryAfterMs);
    if (code !== 200 || !document.success) {
      const kind: Failure['kind'] = [10001, 10002, 10008].includes(code) ? 'AUTHENTICATION' : [10004, 10009].includes(code) ? 'AUTHORIZATION' : [10005, 10010].includes(code) ? 'QUOTA_EXHAUSTED' : 'PROVIDER_REJECTION';
      return branch('providerRejected', kind, `ALAPI rejected lookup with code ${code}`);
    }
    const data = document.data;
    if (!isRecord(data) || !['country', 'province', 'city', 'district', 'isp'].every(field => typeof data[field] === 'string') || !(data.country as string).trim() || !(data.city as string).trim()) {
      return branch('invalidResponse', 'PROTOCOL', 'ALAPI returned incomplete location data');
    }
    return newQueryBranch('found', { country: data.country as string, province: data.province as string, city: data.city as string, district: data.district as string, operator: data.isp as string });
  }
}

function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function isPublicIPv4(value: unknown): value is string {
  if (typeof value !== 'string') return false;
  const parts = value.split('.');
  if (parts.length !== 4 || parts.some(part => !/^(0|[1-9]\d{0,2})$/.test(part) || Number(part) > 255)) return false;
  const first = Number(parts[0]);
  const second = Number(parts[1]);
  return !(parts.every(part => part === '0')) && first !== 10 && first !== 127 && !(first >= 224 && first <= 239) && !(first === 169 && second === 254) && !(first === 172 && second >= 16 && second <= 31) && !(first === 192 && second === 168);
}
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'alapi', operation: 'lookupIPLocation', kind, message }; }
function branch(id: string, kind: Failure['kind'], message: string): QueryAttempt<LookupIPLocationOutput> { return newQueryBranch(id, emptyOutput, {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delayMs = 0): QueryAttempt<LookupIPLocationOutput> { return newQueryRetry(failure(kind, message), delayMs); }
