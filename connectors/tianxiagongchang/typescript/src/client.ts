// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetAccountStatusDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetAccountStatusInput, GetAccountStatusOutput } from './generated/models.js';

const emptyOutput: GetAccountStatusOutput = { appId: 0, creditsBalance: 0, frozenCredits: 0, isSandbox: false };
/** Optional provider transport for deterministic tests. */
export interface TianxiaGongchangClientOptions { /** Sender; production uses Connector SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>; }
/** Read provider data through one credentialed Query. */
export class TianxiaGongchangClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Validate connector settings without contacting the provider. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: TianxiaGongchangClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('TianxiaGongchang configuration and credential source are required');
    this.baseUrl = 'https://open.tianxiagongchang.com';
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('TianxiaGongchang request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the read-only provider Query. */ getAccountStatus(): GetAccountStatusOperation { return new GetAccountStatusOperation(this); }
  /** Dispatch one bounded provider request. */
  async request(call: Call): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge' | 'input'; retryAfterMs: number }> {
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    const key = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    if (!key || key.length > 512 || !/^[!-~]+$/.test(key)) return { error: 'credential', retryAfterMs: 0 };
    const request: ProviderRequest = { url: `${this.baseUrl}/open/v1/account`, method: 'GET', headers: { Accept: 'application/json', Authorization: `Bearer ${key}` }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
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
export class GetAccountStatusOperation implements Query<GetAccountStatusInput, GetAccountStatusOutput> {
  /** Bind one validated client. */ constructor(private readonly client: TianxiaGongchangClient) {}
  /** Return the generated operation contract. */ definition(): QueryDefinition { return GetAccountStatusDefinition; }
  /** Perform one safe read and classify its outcome. */
  async invoke(call: Call, input: GetAccountStatusInput): Promise<QueryAttempt<GetAccountStatusOutput>> {
    void input;
    const exchange = await this.client.request(call);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'input') return branch('defect', 'VALIDATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let document: unknown;
    try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(document) || typeof document.code !== 'number' || !Number.isInteger(document.code)) return branch('invalidResponse', 'PROTOCOL');
    if (document.code === 42900) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (document.code === 50000 || document.code === 50400) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (document.code === 40100) return branch('providerRejected', 'AUTHENTICATION');
    if (document.code === 40300 || document.code === 42901) return branch('providerRejected', 'AUTHORIZATION');
    if (document.code !== 0) return branch('providerRejected', 'PROVIDER_REJECTION');
    const data = document.data;
    if (status !== 200 || document.credits_charged !== 0 || typeof document.credits_balance !== 'number' || !Number.isSafeInteger(document.credits_balance) || document.credits_balance < 0 || !isRecord(data) || typeof data.app_id !== 'number' || !Number.isSafeInteger(data.app_id) || data.app_id <= 0 || typeof data.frozen_credits !== 'number' || !Number.isSafeInteger(data.frozen_credits) || data.frozen_credits < 0 || typeof data.is_sandbox !== 'boolean') return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('retrieved', { appId: data.app_id, creditsBalance: document.credits_balance, frozenCredits: data.frozen_credits, isSandbox: data.is_sandbox });
  }
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'tianxiagongchang', operation: 'getAccountStatus', kind, message: 'TianxiaGongchang provider request failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<GetAccountStatusOutput> { return newQueryBranch(name, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<GetAccountStatusOutput> { return newQueryRetry(failure(kind), delay); }
