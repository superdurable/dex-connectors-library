// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetAccountBalanceDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetAccountBalanceInput, GetAccountBalanceOutput } from './generated/models.js';

/** Optional HTTP sender for SendCloud provider tests. */
export interface SendCloudClientOptions { /** Sender; omitted uses the SDK HTTP transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>; }

/** Read account balance and quota without exposing API credentials. */
export class SendCloudClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate local settings without contacting SendCloud. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: SendCloudClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('SendCloud configuration and credentials are required');
    this.baseUrl = config.baseUrl || 'https://api.sendcloud.net';
    const parsed = new URL(this.baseUrl);
    if (parsed.protocol !== 'https:' || parsed.username || parsed.password || parsed.pathname !== '/' || parsed.search || parsed.hash) throw new TypeError('SendCloud baseUrl must be an HTTPS origin');
    this.timeoutMs = config.timeoutMs || 10_000;
    this.maxResponseBytes = config.maxResponseBytes || 262_144;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('SendCloud request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the account-balance Query. */ getAccountBalance(): GetAccountBalanceOperation { return new GetAccountBalanceOperation(this); }

  /** Send one credentialed form request. */
  async read(call: Call): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let apiUser = '';
    let apiKey = '';
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      apiUser = credentials.apiUser instanceof SecretString ? credentials.apiUser.reveal() : '';
      apiKey = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    } catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!validCredential(apiUser) || !validCredential(apiKey)) return { error: 'credential', retryAfterMs: 0 };
    const body = new TextEncoder().encode(new URLSearchParams({ apiUser, apiKey }).toString());
    const request: ProviderRequest = { url: this.baseUrl.replace(/\/$/, '') + '/apiv2/userinfo/get', method: 'POST', headers: { Accept: 'application/json', 'Content-Type': 'application/x-www-form-urlencoded' }, body, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** Classify SendCloud HTTP and business outcomes. */
export class GetAccountBalanceOperation implements Query<GetAccountBalanceInput, GetAccountBalanceOutput> {
  /** Bind a validated SendCloud client. */ constructor(private readonly client: SendCloudClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return GetAccountBalanceDefinition; }
  /** Read current balance through one bounded provider request. */
  async invoke(call: Call): Promise<QueryAttempt<GetAccountBalanceOutput>> {
    const exchange = await this.client.read(call);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { wire = undefined; }
    if (!isObject(wire) || typeof wire.result !== 'boolean' || !Number.isSafeInteger(wire.statusCode)) return status === 200 ? branch('invalidResponse', 'PROTOCOL') : branch('providerRejected', 'PROVIDER_REJECTION');
    const code = wire.statusCode as number;
    if (code === 50000) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status !== 200 || !wire.result || code !== 200) return branch('providerRejected', code === 40005 ? 'AUTHENTICATION' : [40018, 40025, 40026, 6001].includes(code) ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    if (!isObject(wire.info)) return branch('invalidResponse', 'PROTOCOL');
    const info = wire.info;
    const availableBalance = decimal(info.avaliableBalance);
    const balance = decimal(info.balance);
    if (availableBalance === undefined || balance === undefined || !Number.isSafeInteger(info.quota) || (info.quota as number) < 0 || !Number.isSafeInteger(info.todayUsedQuota) || (info.todayUsedQuota as number) < 0) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('retrieved', { availableBalance, balance, dailyQuota: info.quota as number, todayUsedQuota: info.todayUsedQuota as number });
  }
}

function validCredential(value: string): boolean { return !!value && value.length <= 512 && /^[!-~]+$/.test(value); }
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function decimal(value: unknown): string | undefined { const text = typeof value === 'number' || typeof value === 'string' ? String(value) : ''; return text && Number.isFinite(Number(text)) ? text : undefined; }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'sendcloud', operation: 'getAccountBalance', kind, message: 'SendCloud account balance query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<GetAccountBalanceOutput> { return newQueryBranch(name, { availableBalance: '', balance: '', dailyQuota: 0, todayUsedQuota: 0 }, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<GetAccountBalanceOutput> { return newQueryRetry(failure(kind), delay); }
