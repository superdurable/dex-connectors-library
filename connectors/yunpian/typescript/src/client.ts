// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetAccountBalanceDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetAccountBalanceInput, AccountBalance } from './generated/models.js';

/** Optional provider sender and clock for deterministic tests. */
export interface YunpianClientOptions {
  /** Send one bounded provider request. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Return the current time for signed or retry requests. */ now?: () => Date;
}

/** Read Yunpian provider data through Dex Queries. */
export class YunpianClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;
  /** Validate immutable provider settings. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: YunpianClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Yunpian configuration and credentials are required');
    this.baseUrl = (config.baseUrl ?? 'https://sms.yunpian.com').replace(/\/$/, '');
    const url = new URL(this.baseUrl);
    if ((url.protocol !== 'https:' && !(url.protocol === 'http:' && ['localhost','127.0.0.1','[::1]'].includes(url.hostname))) || url.search || url.hash || false) throw new TypeError('Yunpian base URL is invalid');
    this.timeoutMs = config.timeoutMs ?? 15000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1048576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > 16 << 20) throw new TypeError('Yunpian request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }
  /** Bind the provider Query. */ getAccountBalance(): GetAccountBalanceOperation { return new GetAccountBalanceOperation(this); }
  /** Execute one provider request and classify its outcome. */
  async invoke(call: Call, _input: GetAccountBalanceInput): Promise<QueryAttempt<AccountBalance>> {
    void _input;
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION'); }
    const secret = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    if (!secret || secret.trim() !== secret) return branch('defect', 'AUTHENTICATION');
    const form = new URLSearchParams({ apikey: secret });
    const request: ProviderRequest = { url: this.baseUrl + '/v2/user/get.json', method: 'POST', headers: { Accept: 'application/json', 'Content-Type': 'application/x-www-form-urlencoded; charset=utf-8' }, body: new TextEncoder().encode(form.toString()), timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: this.baseUrl.startsWith('https:') ? 'publicHTTPS' : 'privateNetworkHTTP', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
      return retry('TRANSPORT');
    }
    const { status, body } = response;
    const retryHeader = Object.entries(response.headers).find(([key]) => key.toLowerCase() === 'retry-after')?.[1];
    const delay = parseRetryAfter(Array.isArray(retryHeader) ? retryHeader[0] ?? '' : retryHeader ?? '', this.now());
    if (status === 429) return retry('RATE_LIMIT', delay);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', delay);
    if (body.length > this.maxResponseBytes) return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(wire)) return branch('invalidResponse', 'PROTOCOL');
    if (typeof wire.code === 'number' && Number.isInteger(wire.code)) {
      if (wire.code === -5) return retry('RATE_LIMIT', delay);
      if (wire.code === -50 || wire.code === -51) return retry('AVAILABILITY', delay);
      if (wire.code !== 0) return branch('providerRejected', 'PROVIDER_REJECTION');
    }
    if (status !== 200) return branch('providerRejected', 'PROVIDER_REJECTION');
    if (typeof wire.balance !== 'number' || !Number.isFinite(wire.balance) || wire.balance < 0) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('read', { balance: wire.balance });
  }
}

/** Query adapter backed by a validated Yunpian client. */
export class GetAccountBalanceOperation implements Query<GetAccountBalanceInput, AccountBalance> {
  /** Bind the provider client. */ constructor(private readonly client: YunpianClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return GetAccountBalanceDefinition; }
  /** Invoke the provider Query. */ invoke(call: Call, input: GetAccountBalanceInput): Promise<QueryAttempt<AccountBalance>> { return this.client.invoke(call, input); }
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'yunpian', operation: 'getAccountBalance', kind, message: 'Yunpian query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<AccountBalance> { return newQueryBranch(name, { balance: 0 }, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<AccountBalance> { return newQueryRetry(failure(kind), delay); }
