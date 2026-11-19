// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetBalanceDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetBalanceInput, GetBalanceOutput } from './generated/models.js';

const emptyBalance = (): GetBalanceOutput => ({ balance: '' });
/** Provider HTTP sender, injectable for deterministic tests. */
export type CloopenRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Client dependencies for tests and approved transports. */
export interface CloopenClientOptions {
  /** SDK transport when omitted. */ send?: CloopenRequestSender;
  /** UTC clock used for request signatures and Retry-After dates. */ now?: () => Date;
}
/** Authenticated Cloopen main-account balance client. */
export class CloopenClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: CloopenRequestSender;
  private readonly now: () => Date;
  /** Validate settings without contacting the provider; credentials resolve for each call. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: CloopenClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Cloopen configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || 'https://app.cloopen.com:8883').replace(/\/$/, '');
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 65_536;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Cloopen timeout and maxResponseBytes must be positive whole numbers');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }
  /** Bind the read-only getBalance Query. */ getBalance(): GetBalanceOperation { return new GetBalanceOperation(this); }
  /** Resolve credentials and send one signed, bounded account request. */
  async lookup(call: Call): Promise<{ response: ProviderResponse } | { failure: Failure; retry?: true; retryAfterMs?: number; branch?: string }> {
    let accountSid: string;
    let authToken: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      accountSid = credentials.accountSid instanceof SecretString ? credentials.accountSid.reveal() : '';
      authToken = credentials.authToken instanceof SecretString ? credentials.authToken.reveal() : '';
    } catch { return { branch: 'defect', failure: failure('AUTHENTICATION', 'connection credentials are unavailable or invalid') }; }
    if (!validAccountSid(accountSid) || !validAuthToken(authToken)) return { branch: 'defect', failure: failure('AUTHENTICATION', 'connection credentials are unavailable or invalid') };
    const timestamp = this.now().toISOString().slice(0, 19).replace(/[-:T]/g, '');
    const signature = createHash('md5').update(accountSid + authToken + timestamp).digest('hex').toUpperCase();
    const authorization = Buffer.from(`${accountSid}:${timestamp}`, 'utf8').toString('base64');
    const request: ProviderRequest = {
      url: `${this.baseURL}/2013-12-26/Accounts/${accountSid}/AccountInfo?sig=${signature}`, method: 'GET',
      headers: { Authorization: authorization, Accept: 'application/json', 'Content-Type': 'application/json;charset=utf-8' },
      timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes,
      addressPolicy: this.baseURL.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        if (error.status === 429 || error.status === 408 || error.status >= 500) return statusOutcome(error.status, error.headers ?? {}, this.now());
        return { branch: 'invalidResponse', failure: failure('RESPONSE_TOO_LARGE', 'Cloopen response exceeded the configured limit') };
      }
      return { retry: true, failure: failure('AVAILABILITY', 'Cloopen request failed') };
    }
    if (response.status === 429 || response.status === 408 || response.status >= 500) return statusOutcome(response.status, response.headers, this.now());
    if (response.body.length > this.maxResponseBytes) return { branch: 'invalidResponse', failure: failure('RESPONSE_TOO_LARGE', 'Cloopen response exceeded the configured limit') };
    return { response };
  }
}
/** Read the main account balance without changing its provider units. */
export class GetBalanceOperation implements Query<GetBalanceInput, GetBalanceOutput> {
  /** Bind the provider client. */ constructor(private readonly client: CloopenClient) {}
  /** Generated Query definition. */ definition(): QueryDefinition { return GetBalanceDefinition; }
  /** Send one signed request and classify its result. */
  async invoke(call: Call, input: GetBalanceInput): Promise<QueryAttempt<GetBalanceOutput>> {
    if (!input || Object.keys(input).length !== 0) return terminal('defect', 'VALIDATION', 'balance input must be empty');
    const exchange = await this.client.lookup(call);
    if ('failure' in exchange) return exchange.retry ? newQueryRetry(exchange.failure, exchange.retryAfterMs ?? 0) : newQueryBranch(exchange.branch!, emptyBalance(), {}, exchange.failure);
    const response = exchange.response;
    if (response.status !== 200) return terminal('providerRejected', httpFailureKind(response.status), 'Cloopen rejected the account lookup');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; }
    catch { return terminal('invalidResponse', 'PROTOCOL', 'Cloopen returned an invalid account response'); }
    if (!wire || typeof wire !== 'object' || Array.isArray(wire)) return terminal('invalidResponse', 'PROTOCOL', 'Cloopen returned an invalid account response');
    const fields = wire as Record<string, unknown>;
    const statusCode = fields.statusCode;
    if (typeof statusCode !== 'string' || !statusCode) return terminal('invalidResponse', 'PROTOCOL', 'Cloopen returned an invalid account response');
    if (statusCode === '111111') return newQueryRetry(failure('RATE_LIMIT', 'Cloopen concurrency limit reached'));
    if (statusCode.startsWith('1110')) return newQueryRetry(failure('AVAILABILITY', 'Cloopen reported an internal failure'));
    if (statusCode !== '000000') return terminal('providerRejected', 'PROVIDER_REJECTION', 'Cloopen rejected the account lookup');
    if (!fields.Account || typeof fields.Account !== 'object' || Array.isArray(fields.Account)) return terminal('invalidResponse', 'PROTOCOL', 'Cloopen returned an invalid balance');
    const balance = (fields.Account as Record<string, unknown>).balance;
    if (typeof balance !== 'string' || balance.length > 64 || !/^(0|[1-9][0-9]*)(\.[0-9]+)?$/.test(balance)) return terminal('invalidResponse', 'PROTOCOL', 'Cloopen returned an invalid balance');
    return newQueryBranch('fetched', { balance });
  }
}
function validAccountSid(value: string): boolean { return /^[A-Za-z0-9]{32}$/.test(value); }
function validAuthToken(value: string): boolean { return value.length === 32 && /^[!-~]+$/.test(value); }
function failure(kind: Failure['kind'], message: string): Failure { return { kind, provider: 'cloopen', operation: 'getBalance', message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<GetBalanceOutput> { return newQueryBranch(branch, emptyBalance(), {}, failure(kind, message)); }
function httpFailureKind(status: number): Failure['kind'] { return status === 401 || status === 403 ? 'AUTHENTICATION' : status === 400 ? 'VALIDATION' : 'PROVIDER_REJECTION'; }
function statusOutcome(status: number, headers: ProviderResponse['headers'], now: Date): { retry: true; failure: Failure; retryAfterMs: number } {
  const entry = Object.entries(headers).find(([name]) => name.toLowerCase() === 'retry-after')?.[1];
  const retryAfter = Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
  return { retry: true, retryAfterMs: parseRetryAfter(retryAfter, now), failure: status === 429 ? failure('RATE_LIMIT', 'Cloopen rate limit reached') : failure('AVAILABILITY', 'Cloopen is temporarily unavailable') };
}
