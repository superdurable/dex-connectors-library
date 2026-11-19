// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetBalanceDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetBalanceInput, Balance } from './generated/models.js';

const emptyOutput: Balance = { balanceYuan: 0, isActive: false, unit: '' };
/** Optional provider transport for deterministic tests. */
export interface TokenFactoryClientOptions { /** Sender; production uses Connector SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>; }
/** Read provider data through one credentialed Query. */
export class TokenFactoryClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Validate connector settings without contacting the provider. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: TokenFactoryClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('TokenFactory configuration and credential source are required');
    const origin = new URL(config.baseUrl ?? 'https://api.tokenfactory.cn'); if (origin.protocol !== 'https:' || origin.username || origin.password || origin.search || origin.hash) throw new TypeError('TokenFactory base URL is invalid'); this.baseUrl = origin.origin;
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 65_536;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('TokenFactory request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the read-only provider Query. */ getBalance(): GetBalanceOperation { return new GetBalanceOperation(this); }
  /** Dispatch one bounded provider request. */
  async request(call: Call): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge' | 'input'; retryAfterMs: number }> {
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    const key = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    if (!key || key.length > 2048 || !/^[!-~]+$/.test(key) || /^bearer /i.test(key)) return { error: 'credential', retryAfterMs: 0 };
    const request: ProviderRequest = { url: `${this.baseUrl}/v1/user/balance`, method: 'GET', headers: { Accept: 'application/json', Authorization: `Bearer ${key}` }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
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
export class GetBalanceOperation implements Query<GetBalanceInput, Balance> {
  /** Bind one validated client. */ constructor(private readonly client: TokenFactoryClient) {}
  /** Return the generated operation contract. */ definition(): QueryDefinition { return GetBalanceDefinition; }
  /** Perform one safe read and classify its outcome. */
  async invoke(call: Call, input: GetBalanceInput): Promise<QueryAttempt<Balance>> {
    void input;
    const exchange = await this.client.request(call);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'input') return branch('defect', 'VALIDATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : [400,404,405].includes(status) ? 'VALIDATION' : 'PROVIDER_REJECTION');
    let document: unknown;
    try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(document) || typeof document.balance !== 'number' || !Number.isFinite(document.balance) || typeof document.is_active !== 'boolean' || document.unit !== 'CNY') return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('retrieved', { balanceYuan: document.balance, isActive: document.is_active, unit: document.unit });
  }
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'tokenfactory', operation: 'getBalance', kind, message: 'TokenFactory provider request failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<Balance> { return newQueryBranch(name, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<Balance> { return newQueryRetry(failure(kind), delay); }
