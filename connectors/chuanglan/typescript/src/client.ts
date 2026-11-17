// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetBalanceDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetBalanceInput, GetBalanceOutput } from './generated/models.js';

const emptyBalance = (): GetBalanceOutput => ({ remainingMessages: 0 });
/** Provider HTTP sender, injectable for deterministic tests. */
export type ChuanglanRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Client dependencies for tests and approved transports. */
export interface ChuanglanClientOptions {
  /** SDK transport when omitted. */ send?: ChuanglanRequestSender;
  /** Clock used to interpret HTTP Retry-After dates. */ now?: () => Date;
}
/** Authenticated Chuanglan domestic SMS balance client. */
export class ChuanglanClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: ChuanglanRequestSender;
  private readonly now: () => Date;

  /** Validate settings without contacting the provider; credentials resolve for each call. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: ChuanglanClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Chuanglan configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || 'https://smssh1.253.com').replace(/\/$/, '');
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 65_536;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Chuanglan timeout and maxResponseBytes must be positive whole numbers');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }
  /** Bind the read-only getBalance Query. */ getBalance(): GetBalanceOperation { return new GetBalanceOperation(this); }
  /** Resolve private credentials and send one bounded balance request. */
  async lookup(call: Call): Promise<{ response: ProviderResponse } | { failure: Failure; retry?: true; retryAfterMs?: number; branch?: string }> {
    let account: string;
    let password: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      account = credentials.account instanceof SecretString ? credentials.account.reveal() : '';
      password = credentials.password instanceof SecretString ? credentials.password.reveal() : '';
    } catch { return { branch: 'defect', failure: failure('AUTHENTICATION', 'connection credentials are unavailable or invalid') }; }
    if (!validCredential(account) || !validCredential(password)) return { branch: 'defect', failure: failure('AUTHENTICATION', 'connection credentials are unavailable or invalid') };
    const request: ProviderRequest = {
      url: `${this.baseURL}/msg/balance/json`, method: 'POST',
      headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
      body: new TextEncoder().encode(JSON.stringify({ account, password })),
      timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes,
      addressPolicy: this.baseURL.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        if (error.status === 429 || error.status === 408 || error.status >= 500) return statusOutcome(error.status, error.headers ?? {}, this.now());
        return { branch: 'invalidResponse', failure: failure('RESPONSE_TOO_LARGE', 'Chuanglan response exceeded the configured limit') };
      }
      return { retry: true, failure: failure('AVAILABILITY', 'Chuanglan request failed') };
    }
    if (response.status === 429 || response.status === 408 || response.status >= 500) return statusOutcome(response.status, response.headers, this.now());
    if (response.body.length > this.maxResponseBytes) return { branch: 'invalidResponse', failure: failure('RESPONSE_TOO_LARGE', 'Chuanglan response exceeded the configured limit') };
    return { response };
  }
}
/** Read the number of domestic SMS messages remaining. */
export class GetBalanceOperation implements Query<GetBalanceInput, GetBalanceOutput> {
  /** Bind the provider client. */ constructor(private readonly client: ChuanglanClient) {}
  /** Generated Query definition. */ definition(): QueryDefinition { return GetBalanceDefinition; }
  /** Send one read-only request and classify its result. */
  async invoke(call: Call, input: GetBalanceInput): Promise<QueryAttempt<GetBalanceOutput>> {
    if (!input || Object.keys(input).length !== 0) return terminal('defect', 'VALIDATION', 'balance input must be empty');
    const exchange = await this.client.lookup(call);
    if ('failure' in exchange) return exchange.retry ? newQueryRetry(exchange.failure, exchange.retryAfterMs ?? 0) : newQueryBranch(exchange.branch!, emptyBalance(), {}, exchange.failure);
    const response = exchange.response;
    if (response.status !== 200) return terminal('providerRejected', httpFailureKind(response.status), 'Chuanglan rejected the balance lookup');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; }
    catch { return terminal('invalidResponse', 'PROTOCOL', 'Chuanglan returned an invalid balance response'); }
    if (!wire || typeof wire !== 'object' || Array.isArray(wire)) return terminal('invalidResponse', 'PROTOCOL', 'Chuanglan returned an invalid balance response');
    const fields = wire as Record<string, unknown>;
    if (fields.code === undefined || fields.code === null) return terminal('invalidResponse', 'PROTOCOL', 'Chuanglan returned an invalid balance response');
    const code = fields.code;
    if (!(typeof code === 'string' || typeof code === 'number') || !/^-?\d+$/.test(String(code))) return terminal('invalidResponse', 'PROTOCOL', 'Chuanglan returned an invalid status code');
    if (String(code) !== '0') return terminal('providerRejected', 'PROVIDER_REJECTION', 'Chuanglan rejected the balance lookup');
    const balance = fields.balance;
    if (typeof balance !== 'string' || !/^\d+$/.test(balance)) return terminal('invalidResponse', 'PROTOCOL', 'Chuanglan returned an invalid remaining-message count');
    const remainingMessages = Number(balance);
    if (!Number.isSafeInteger(remainingMessages) || remainingMessages < 0) return terminal('invalidResponse', 'PROTOCOL', 'Chuanglan returned an invalid remaining-message count');
    return newQueryBranch('fetched', { remainingMessages });
  }
}
function validCredential(value: string): boolean { return value.length > 0 && value.length <= 512 && /^[!-~]+$/.test(value); }
function failure(kind: Failure['kind'], message: string): Failure { return { kind, provider: 'chuanglan', operation: 'getBalance', message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<GetBalanceOutput> { return newQueryBranch(branch, emptyBalance(), {}, failure(kind, message)); }
function httpFailureKind(status: number): Failure['kind'] { return status === 401 || status === 403 ? 'AUTHENTICATION' : status === 400 ? 'VALIDATION' : 'PROVIDER_REJECTION'; }
function statusOutcome(status: number, headers: ProviderResponse['headers'], now: Date): { retry: true; failure: Failure; retryAfterMs: number } {
  const entry = Object.entries(headers).find(([name]) => name.toLowerCase() === 'retry-after')?.[1];
  const retryAfter = Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
  return { retry: true, retryAfterMs: parseRetryAfter(retryAfter, now), failure: status === 429 ? failure('RATE_LIMIT', 'Chuanglan rate limit reached') : failure('AVAILABILITY', 'Chuanglan is temporarily unavailable') };
}
