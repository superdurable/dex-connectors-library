// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetAccountBalanceDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetAccountBalanceInput, GetAccountBalanceOutput } from './generated/models.js';

const emptyBalance = (): GetAccountBalanceOutput => ({ availableBalance: '', balance: '', dailyQuota: 0, todayUsedQuota: 0 });

/** Inject the provider sender for tests. */
export interface SendCloudClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

/** Read account balance and quota without exposing credentials in Flow state. */
export class SendCloudClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate the provider connection. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: SendCloudClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('SendCloud configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || 'https://api.sendcloud.net');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 262_144;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('SendCloud request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the getAccountBalance Query. */ getAccountBalance(): GetAccountBalanceOperation { return new GetAccountBalanceOperation(this); }

  /** Send one bounded balance request. */
  async get(call: Call): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let apiUser: string;
    let apiKey: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      apiUser = credentials.apiUser instanceof SecretString ? credentials.apiUser.reveal() : '';
      apiKey = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    } catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!validCredential(apiUser) || !validCredential(apiKey)) return { error: 'credential', retryAfterMs: 0 };
    const body = new URLSearchParams({ apiUser, apiKey }).toString();
    const request: ProviderRequest = { url: `${this.baseURL}/apiv2/userinfo/get`, method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded', Accept: 'application/json' }, body: Buffer.from(body), timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: this.baseURL.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    const retryAfterMs = parseRetryAfter(header(response.headers, 'retry-after'));
    if (response.body.length > this.maxResponseBytes) return { response, error: 'tooLarge', retryAfterMs };
    return { response, retryAfterMs };
  }
}

/** Read the balance and request quota for one SendCloud account. */
export class GetAccountBalanceOperation implements Query<GetAccountBalanceInput, GetAccountBalanceOutput> {
  /** Bind the client. */ constructor(private readonly client: SendCloudClient) {}
  /** Return the generated operation definition. */ definition(): QueryDefinition { return GetAccountBalanceDefinition; }
  /** Classify the provider result according to the Go connector. */
  async invoke(call: Call, input: GetAccountBalanceInput): Promise<QueryAttempt<GetAccountBalanceOutput>> {
    void input;
    const exchange = await this.client.get(call);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION', 'connection credentials are unavailable or invalid');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY', 'SendCloud request failed');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', 'SendCloud rate limited the request', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', 'SendCloud is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE', 'SendCloud response exceeded the configured limit');
    let raw: unknown;
    try { raw = JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { raw = undefined; }
    if (!isObject(raw) || typeof raw.result !== 'boolean' || !Number.isInteger(raw.statusCode)) {
      return status !== 200 ? branch('providerRejected', 'PROVIDER_REJECTION', `SendCloud rejected the request with HTTP ${status}`) : branch('invalidResponse', 'PROTOCOL', 'SendCloud returned an invalid response');
    }
    const code = raw.statusCode as number;
    if (code === 50000) return retry('RATE_LIMIT', 'SendCloud rate limited the request', exchange.retryAfterMs);
    if (status !== 200 || !raw.result || code !== 200) return branch('providerRejected', classifyCode(code), 'SendCloud rejected the account balance request');
    if (!isObject(raw.info)) return branch('invalidResponse', 'PROTOCOL', 'SendCloud returned an invalid response');
    const info = raw.info;
    const availableBalance = numberText(info.avaliableBalance);
    const balance = numberText(info.balance);
    if (availableBalance === undefined || balance === undefined || !Number.isInteger(info.quota) || (info.quota as number) < 0 || !Number.isInteger(info.todayUsedQuota) || (info.todayUsedQuota as number) < 0) return branch('invalidResponse', 'PROTOCOL', 'SendCloud returned an invalid response');
    return newQueryBranch('retrieved', { availableBalance, balance, dailyQuota: info.quota as number, todayUsedQuota: info.todayUsedQuota as number });
  }
}

function validCredential(value: string): boolean { return value.length > 0 && value.length <= 512 && /^[\x21-\x7e]+$/.test(value); }
function numberText(value: unknown): string | undefined { return typeof value === 'number' && Number.isFinite(value) ? String(value) : undefined; }
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function classifyCode(code: number): Failure['kind'] { if (code === 40005) return 'AUTHENTICATION'; if ([40018, 40025, 40026, 6001].includes(code)) return 'AUTHORIZATION'; return 'PROVIDER_REJECTION'; }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'sendcloud', operation: 'getAccountBalance', kind, message }; }
function branch(name: string, kind: Failure['kind'], message: string): QueryAttempt<GetAccountBalanceOutput> { return newQueryBranch(name, emptyBalance(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<GetAccountBalanceOutput> { return newQueryRetry(failure(kind, message), delay); }
