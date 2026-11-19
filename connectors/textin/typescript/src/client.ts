// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetAccountBalanceDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetAccountBalanceInput, GetAccountBalanceOutput } from './generated/models.js';

/** Optional sender for local provider tests. */
export interface TextinClientOptions { /** Sender; omitted uses the SDK HTTP transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>; }

/** Read a TextIn account balance without exposing the raw account response. */
export class TextinClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate local settings without contacting TextIn. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: TextinClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('TextIn configuration and credentials are required');
    this.baseUrl = config.baseUrl || 'https://api.textin.com';
    this.timeoutMs = config.timeoutMs || 10_000;
    this.maxResponseBytes = config.maxResponseBytes || 65_536;
    const parsed = new URL(this.baseUrl);
    if (parsed.protocol !== 'https:' || parsed.username || parsed.password || parsed.pathname !== '/' || parsed.search || parsed.hash || !Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('TextIn request configuration is invalid');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the account-balance Query. */ getAccountBalance(): GetAccountBalanceOperation { return new GetAccountBalanceOperation(this); }

  /** Send one bounded account request. */
  async request(call: Call): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let appId = '', secretCode = '';
    try { const credentials = await this.credentials.resolve(call, call.context.cancellationSignal); appId = credentials.appId; secretCode = credentials.secretCode instanceof SecretString ? credentials.secretCode.reveal() : ''; }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!validHeaderValue(appId) || !validHeaderValue(secretCode)) return { error: 'credential', retryAfterMs: 0 };
    const request: ProviderRequest = { url: this.baseUrl + '/user/finance/balance', method: 'GET', headers: { Accept: 'application/json', 'x-ti-app-id': appId, 'x-ti-secret-code': secretCode }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** Query the connected account's available T-coins. */
export class GetAccountBalanceOperation implements Query<GetAccountBalanceInput, GetAccountBalanceOutput> {
  /** Bind the provider client. */ constructor(private readonly client: TextinClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return GetAccountBalanceDefinition; }
  /** Classify one provider response using the Go connector contract. */
  async invoke(call: Call): Promise<QueryAttempt<GetAccountBalanceOutput>> {
    const exchange = await this.client.request(call);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    let text = '', wire: unknown;
    try { text = new TextDecoder('utf-8', { fatal: true }).decode(body); wire = JSON.parse(text) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isObject(wire) || !Number.isInteger(wire.code)) return branch('invalidResponse', 'PROTOCOL');
    if (wire.code === 500) return retry('AVAILABILITY');
    if (wire.code === 40101 || wire.code === 40102) return branch('providerRejected', 'AUTHENTICATION');
    if (wire.code !== 200) return branch('providerRejected', 'PROVIDER_REJECTION');
    if (!isObject(wire.data) || !Number.isInteger(wire.data.pay_type) || (wire.data.pay_type !== 1 && wire.data.pay_type !== 2)) return branch('invalidResponse', 'PROTOCOL');
    const balance = /"available_balance"\s*:\s*(-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?)/.exec(text)?.[1];
    if (!balance || balance.length > 64 || typeof wire.data.available_balance !== 'number') return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('fetched', { availableTCoins: balance, payType: wire.data.pay_type });
  }
}

function validHeaderValue(value: string): boolean { return !!value && value.length <= 512 && !/[^\x21-\x7e]/.test(value); }
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'textin', operation: 'getAccountBalance', kind, message: 'TextIn balance query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<GetAccountBalanceOutput> { return newQueryBranch(name, { availableTCoins: '', payType: 0 }, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<GetAccountBalanceOutput> { return newQueryRetry(failure(kind), delay); }
