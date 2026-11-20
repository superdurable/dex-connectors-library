// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash, randomBytes } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { CheckUserOnlineDefinition } from './generated/definitions.js';
import type { Config, Credentials, CheckUserOnlineInput, CheckUserOnlineOutput } from './generated/models.js';

const empty: CheckUserOnlineOutput = { isOnline: false };
/** Inject provider transport, timestamp, and nonce for deterministic tests. */
export interface RongCloudClientOptions {
  /** Dispatch requests; production uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Supply signing time; production uses the current clock. */ now?: () => Date;
  /** Supply 9 nonce bytes; production uses cryptographic randomness. */ nonce?: () => Uint8Array;
}
/** Read one RongCloud user's presence through a signed Query. */
export class RongCloudClient {
  readonly baseUrl: string;
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  readonly now: () => Date;
  readonly nonce: () => Uint8Array;
  /** Validate provider settings before contacting RongCloud. */
  constructor(config: Config, readonly credentials: CredentialProvider<Credentials>, options: RongCloudClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('RongCloud configuration and credential source are required');
    const url = new URL(config.baseUrl ?? 'https://api.rong-api.com');
    if (url.protocol !== 'https:' || url.username || url.password || url.search || url.hash) throw new TypeError('RongCloud base URL is invalid');
    this.baseUrl = url.origin;
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 65_536;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > 16 << 20) throw new TypeError('RongCloud request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
    this.nonce = options.nonce ?? (() => randomBytes(9));
  }
  /** Bind the presence Query. */ checkUserOnline(): CheckUserOnlineOperation { return new CheckUserOnlineOperation(this); }
}
/** Classify presence results, provider failures, and safe retries. */
export class CheckUserOnlineOperation implements Query<CheckUserOnlineInput, CheckUserOnlineOutput> {
  /** Bind one validated client. */ constructor(private readonly client: RongCloudClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return CheckUserOnlineDefinition; }
  /** Send one signed form request and validate the provider response. */
  async invoke(call: Call, input: CheckUserOnlineInput): Promise<QueryAttempt<CheckUserOnlineOutput>> {
    if (!input || typeof input.userId !== 'string' || !/^[A-Za-z0-9]{1,64}$/.test(input.userId)) return branch('defect', 'VALIDATION');
    let credentials: Credentials;
    try { credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal); } catch { return branch('defect', 'AUTHENTICATION'); }
    const key = credentials.appKey;
    const secret = credentials.appSecret instanceof SecretString ? credentials.appSecret.reveal() : '';
    if (!isCredential(key) || !isCredential(secret)) return branch('defect', 'AUTHENTICATION');
    let nonce: string;
    try { const bytes = this.client.nonce(); if (bytes.length !== 9) throw new TypeError('nonce length'); nonce = Buffer.from(bytes).toString('hex'); } catch { return branch('defect', 'AVAILABILITY'); }
    const timestamp = String(this.client.now().getTime());
    const signature = createHash('sha1').update(secret + nonce + timestamp).digest('hex');
    const request: ProviderRequest = { url: `${this.client.baseUrl}/user/checkOnline.json`, method: 'POST', headers: { Accept: 'application/json', 'Content-Type': 'application/x-www-form-urlencoded', 'App-Key': key, Nonce: nonce, Timestamp: timestamp, Signature: signature }, body: new TextEncoder().encode(new URLSearchParams({ userId: input.userId }).toString()), timeoutMs: this.client.timeoutMs, maxResponseBytes: this.client.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.client.send(request); } catch (error) { return error instanceof BodyTooLargeError && error.status !== undefined ? branch('invalidResponse', 'RESPONSE_TOO_LARGE') : retry('AVAILABILITY'); }
    const header = Object.entries(response.headers).find(([name]) => name.toLowerCase() === 'retry-after')?.[1];
    const delay = parseRetryAfter(Array.isArray(header) ? header[0] ?? '' : header ?? '');
    if (response.status === 429) return retry('RATE_LIMIT', delay);
    if (response.status === 408 || response.status >= 500) return retry('AVAILABILITY', delay);
    if (response.status !== 200) return branch('providerRejected', response.status === 401 ? 'AUTHENTICATION' : response.status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    if (response.body.length > this.client.maxResponseBytes) return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let document: unknown;
    try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(document) || !Number.isSafeInteger(document.code)) return branch('invalidResponse', 'PROTOCOL');
    if (document.code !== 200) {
      if ([1008, 20607].includes(document.code as number)) return retry('RATE_LIMIT', delay);
      if (document.code === 1050) return retry('AVAILABILITY', delay);
      return branch('providerRejected', [1001, 1004].includes(document.code as number) ? 'AUTHENTICATION' : 'PROVIDER_REJECTION');
    }
    if (document.status !== '0' && document.status !== '1') return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('checked', { isOnline: document.status === '1' });
  }
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function isCredential(value: unknown): value is string { return typeof value === 'string' && value.length > 0 && value.length <= 512 && /^[!-~]+$/.test(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'rongcloud', operation: 'checkUserOnline', kind, message: 'RongCloud presence query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<CheckUserOnlineOutput> { return newQueryBranch(name, empty, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<CheckUserOnlineOutput> { return newQueryRetry(failure(kind), delay); }
