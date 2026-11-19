// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetPushQuotaDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetPushQuotaInput, GetPushQuotaOutput } from './generated/models.js';

type Exchange = { response?: ProviderResponse; error?: 'transport' | 'tooLarge'; retryAfterMs: number };

/** Optional sender and clock for signed OPPO Push tests. */
export interface OppoClientOptions {
  /** HTTP sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Timestamp source; omitted uses wall time. */ now?: () => Date;
}

/** Query OPPO Push daily quota with fresh credentials for each call. */
export class OppoClient {
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;
  /** Validate bounded request settings. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: OppoClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('OPPO configuration and credentials are required');
    this.timeoutMs = config.timeoutMs || 15000;
    this.maxResponseBytes = config.maxResponseBytes || 1048576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > 16 * 1024 * 1024) throw new TypeError('OPPO request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }
  /** Bind the quota Query. */ getPushQuota(): GetPushQuotaOperation { return new GetPushQuotaOperation(this); }
  /** Resolve the app key and master secret without recording either. */
  async credentialsFor(call: Call): Promise<{ appKey: string; masterSecret: string } | undefined> {
    try {
      const fields = await this.credentials.resolve(call, call.context.cancellationSignal);
      const appKey = fields.appKey instanceof SecretString ? fields.appKey.reveal() : '';
      const masterSecret = fields.masterSecret instanceof SecretString ? fields.masterSecret.reveal() : '';
      return validCredential(appKey) && validCredential(masterSecret) ? { appKey, masterSecret } : undefined;
    } catch { return undefined; }
  }
  /** Sign an authentication timestamp using the provider's SHA-256 contract. */
  authForm(appKey: string, masterSecret: string): Uint8Array {
    const timestamp = String(this.now().getTime());
    const sign = createHash('sha256').update(appKey + timestamp + masterSecret).digest('hex');
    return new TextEncoder().encode(new URLSearchParams({ app_key: appKey, timestamp, sign }).toString());
  }
  /** Send one bounded provider request and classify transport errors. */
  async request(call: Call, url: string, method: string, headers: Record<string, string>, body?: Uint8Array): Promise<Exchange> {
    const request: ProviderRequest = { url, method, headers, body, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** Authenticate and read OPPO Push quota through two bounded HTTP exchanges. */
export class GetPushQuotaOperation implements Query<GetPushQuotaInput, GetPushQuotaOutput> {
  /** Bind the provider client. */ constructor(private readonly client: OppoClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return GetPushQuotaDefinition; }
  /** Execute authentication and quota lookup, retrying uncertain provider availability. */
  async invoke(call: Call, input: GetPushQuotaInput): Promise<QueryAttempt<GetPushQuotaOutput>> {
    void input;
    const credentials = await this.client.credentialsFor(call);
    if (!credentials) return branch('defect', 'AUTHENTICATION');
    const auth = await this.client.request(call, 'https://api.push.oppomobile.com/server/v1/auth', 'POST', { Accept: 'application/json', 'Content-Type': 'application/x-www-form-urlencoded' }, this.client.authForm(credentials.appKey, credentials.masterSecret));
    const authFailure = classifyExchange(auth);
    if (authFailure) return authFailure;
    const authWire = parseBody(auth.response!.body);
    if (!isRecord(authWire) || !Number.isInteger(authWire.code)) return branch('invalidResponse', 'PROTOCOL');
    if (authWire.code !== 0) return classifyCode(authWire.code as number, auth.retryAfterMs);
    if (!isRecord(authWire.data) || !validCredential(authWire.data.auth_token)) return branch('invalidResponse', 'PROTOCOL');
    const quota = await this.client.request(call, 'https://feedback.push.oppomobile.com/server/v1/feedback/fetch_push_permit', 'GET', { Accept: 'application/json', auth_token: authWire.data.auth_token });
    const quotaFailure = classifyExchange(quota);
    if (quotaFailure) return quotaFailure;
    const quotaWire = parseBody(quota.response!.body);
    if (!isRecord(quotaWire) || !Number.isInteger(quotaWire.code)) return branch('invalidResponse', 'PROTOCOL');
    if (quotaWire.code !== 0) return classifyCode(quotaWire.code as number, quota.retryAfterMs);
    const data = quotaWire.data;
    if (!isRecord(data) || ![data.total_count, data.push_count, data.remain_count].every(value => Number.isSafeInteger(value) && (value as number) >= 0) || (data.push_count as number) + (data.remain_count as number) > (data.total_count as number)) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('found', { totalCount: data.total_count as number, pushCount: data.push_count as number, remainCount: data.remain_count as number });
  }
}

function classifyExchange(exchange: Exchange): QueryAttempt<GetPushQuotaOutput> | undefined {
  if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
  const status = exchange.response.status;
  if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
  if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
  if (status !== 200) return branch('providerRejected', 'PROVIDER_REJECTION');
  if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
  return undefined;
}
function classifyCode(code: number, delay: number): QueryAttempt<GetPushQuotaOutput> {
  if (code === -2 || code === 13) return retry('RATE_LIMIT', delay);
  if (code === -1) return retry('AVAILABILITY', delay);
  return branch('providerRejected', 'PROVIDER_REJECTION');
}
function validCredential(value: unknown): value is string { return typeof value === 'string' && !!value && value.length <= 1024 && value.trim() === value && !value.split('').some(character => character.charCodeAt(0) < 32 || character.charCodeAt(0) === 127); }
function parseBody(body: Uint8Array): unknown { try { return JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return undefined; } }
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'oppo', operation: 'getPushQuota', kind, message: 'OPPO Push quota request failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<GetPushQuotaOutput> { return newQueryBranch(name, { totalCount: 0, pushCount: 0, remainCount: 0 }, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<GetPushQuotaOutput> { return newQueryRetry(failure(kind), delay); }
