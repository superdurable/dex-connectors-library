// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash, randomBytes } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetUserProfileDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetUserProfileInput, GetUserProfileOutput } from './generated/models.js';

const emptyOutput: GetUserProfileOutput = { accountId: '', name: '' };
/** Optional transport and signing sources for deterministic provider tests. */
export interface NeteaseClientOptions {
  /** Provider HTTP sender; production uses the Connector SDK transport. */
  send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Clock used for request signatures. */
  now?: () => Date;
  /** Sixteen random bytes used as a request nonce. */
  nonce?: () => Uint8Array;
}
/** Read NetEase Yunxin user profiles with signed requests. */
export class NeteaseClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;
  private readonly nonce: () => Uint8Array;
  /** Validate immutable connection settings without contacting NetEase. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: NeteaseClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('NetEase configuration and credential source are required');
    this.baseUrl = config.baseUrl ?? 'https://open.yunxinapi.com';
    if (!/^https:\/\/[^/?#]+$/.test(this.baseUrl)) throw new TypeError('NetEase base URL must be an HTTPS origin');
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 262_144;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('NetEase request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
    this.nonce = options.nonce ?? (() => randomBytes(16));
  }
  /** Bind the profile Query. */
  getUserProfile(): GetUserProfileOperation { return new GetUserProfileOperation(this); }
  /** Resolve credentials and dispatch exactly one signed request. */
  async request(call: Call, input: GetUserProfileInput): Promise<{ response?: ProviderResponse; error?: 'credential' | 'input' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    if (!input || typeof input.accountId !== 'string' || !input.accountId || Buffer.byteLength(input.accountId, 'utf8') > 128 || input.accountId.trim() !== input.accountId || /[/?#]/.test(input.accountId)) return { error: 'input', retryAfterMs: 0 };
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); } catch { return { error: 'credential', retryAfterMs: 0 }; }
    const appKey = credentials.appKey instanceof SecretString ? credentials.appKey.reveal() : '';
    const appSecret = credentials.appSecret instanceof SecretString ? credentials.appSecret.reveal() : '';
    if (![appKey, appSecret].every(value => !!value && value.length <= 512 && /^[!-~]+$/.test(value))) return { error: 'credential', retryAfterMs: 0 };
    let nonce: Uint8Array;
    try { nonce = this.nonce(); } catch { return { error: 'transport', retryAfterMs: 0 }; }
    if (nonce.length !== 16) return { error: 'transport', retryAfterMs: 0 };
    const nonceText = Buffer.from(nonce).toString('hex');
    const currentTime = Math.floor(this.now().getTime() / 1000).toString();
    const checksum = createHash('sha1').update(appSecret + nonceText + currentTime).digest('hex');
    const request: ProviderRequest = { url: `${this.baseUrl}/im/v2/users/${encodeURIComponent(input.accountId)}`, method: 'GET', headers: { AppKey: appKey, Nonce: nonceText, CurTime: currentTime, CheckSum: checksum, Accept: 'application/json', 'Content-Type': 'application/json' }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); } catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    const retryAfterValue = Object.entries(response.headers).find(([key]) => key.toLowerCase() === 'retry-after')?.[1];
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(Array.isArray(retryAfterValue) ? retryAfterValue[0] ?? '' : retryAfterValue ?? '') };
  }
}
/** Classify one Yunxin profile lookup. */
export class GetUserProfileOperation implements Query<GetUserProfileInput, GetUserProfileOutput> {
  /** Bind a validated NetEase client. */ constructor(private readonly client: NeteaseClient) {}
  /** Return the generated operation contract. */ definition(): QueryDefinition { return GetUserProfileDefinition; }
  /** Execute one safe profile read. */
  async invoke(call: Call, input: GetUserProfileInput): Promise<QueryAttempt<GetUserProfileOutput>> {
    const exchange = await this.client.request(call, input);
    if (exchange.error === 'input') return branch('defect', 'VALIDATION');
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(wire) || typeof wire.code !== 'number' || wire.code === 0) return branch('invalidResponse', 'PROTOCOL');
    if (wire.code === 200) {
      const profile = wire.data;
      if (status !== 200 || !isRecord(profile) || (profile.account_id !== undefined && profile.account_id !== input.accountId) || (profile.name !== undefined && typeof profile.name !== 'string')) return branch('invalidResponse', 'PROTOCOL');
      return newQueryBranch('fetched', { accountId: input.accountId, name: typeof profile.name === 'string' ? profile.name : '' });
    }
    if (wire.code === 102404 || wire.code === 103404) return branch('notFound', 'PROVIDER_REJECTION');
    if (wire.code === 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (wire.code === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (wire.code === 414) return branch('providerRejected', 'AUTHENTICATION');
    return branch('providerRejected', 'PROVIDER_REJECTION');
  }
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'netease', operation: 'getUserProfile', kind, message: 'NetEase provider request failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<GetUserProfileOutput> { return newQueryBranch(name, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<GetUserProfileOutput> { return newQueryRetry(failure(kind), delay); }
