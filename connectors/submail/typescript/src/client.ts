// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetSMSBalanceDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetSMSBalanceInput, SMSBalance } from './generated/models.js';

const emptyOutput: SMSBalance = { generalCredits: '', transactionalCredits: '' };
/** Optional provider transport for deterministic tests. */
export interface SubmailClientOptions { /** Sender; production uses Connector SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>; }
/** Read provider data through one credentialed Query. */
export class SubmailClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Validate connector settings without contacting the provider. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: SubmailClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Submail configuration and credential source are required');
    const origin = new URL(config.baseUrl ?? 'https://api-v4.mysubmail.com'); if (origin.protocol !== 'https:' || origin.username || origin.password || origin.search || origin.hash) throw new TypeError('SUBMAIL base URL is invalid'); this.baseUrl = origin.origin;
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 4_096;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('Submail request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the read-only provider Query. */ getSMSBalance(): GetSMSBalanceOperation { return new GetSMSBalanceOperation(this); }
  /** Dispatch one bounded provider request. */
  async request(call: Call): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge' | 'input'; retryAfterMs: number }> {
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    const appId = credentials.appId; const key = credentials.appKey instanceof SecretString ? credentials.appKey.reveal() : '';
    if (!appId || appId.length > 128 || !/^[!-~]+$/.test(appId) || !key || /[\r\n]/.test(key)) return { error: 'credential', retryAfterMs: 0 };
    const request: ProviderRequest = { url: `${this.baseUrl}/balance/sms.json`, method: 'POST', headers: { Accept: 'application/json', 'Content-Type': 'application/x-www-form-urlencoded' }, body: new TextEncoder().encode(new URLSearchParams({ appid: appId, signature: key }).toString()), timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
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
export class GetSMSBalanceOperation implements Query<GetSMSBalanceInput, SMSBalance> {
  /** Bind one validated client. */ constructor(private readonly client: SubmailClient) {}
  /** Return the generated operation contract. */ definition(): QueryDefinition { return GetSMSBalanceDefinition; }
  /** Perform one safe read and classify its outcome. */
  async invoke(call: Call, input: GetSMSBalanceInput): Promise<QueryAttempt<SMSBalance>> {
    void input;
    const exchange = await this.client.request(call);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'input') return branch('defect', 'VALIDATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('TRANSPORT');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', 'PROVIDER_REJECTION');
    let document: unknown;
    try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(document) || typeof document.status !== 'string') return branch('invalidResponse', 'PROTOCOL');
    if (document.status === 'error') return branch('providerRejected', 'PROVIDER_REJECTION');
    if (document.status !== 'success' || typeof document.balance !== 'string' || typeof document.transactional_balance !== 'string' || !/^[0-9]{1,30}$/.test(document.balance) || !/^[0-9]{1,30}$/.test(document.transactional_balance)) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('found', { generalCredits: document.balance, transactionalCredits: document.transactional_balance });
  }
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'submail', operation: 'getSMSBalance', kind, message: 'Submail provider request failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<SMSBalance> { return newQueryBranch(name, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<SMSBalance> { return newQueryRetry(failure(kind), delay); }
