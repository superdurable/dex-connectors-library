// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetAccountBalanceDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetAccountBalanceInput, GetAccountBalanceOutput } from './generated/models.js';

const emptyOutput: GetAccountBalanceOutput = { availableCredits: '', frozenCredits: '' };
/** Send a Tripo request through caller-owned HTTP transport. */
export type BalanceRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Optional provider transport for tests. */
export interface TripoClientOptions { send?: BalanceRequestSender }
/** Resolve API credentials per account-balance Query. */
export class TripoClient {
  readonly baseUrl: string;
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly credentials: CredentialProvider<Credentials>;
  readonly send: BalanceRequestSender;
  /** Validate connection settings without contacting Tripo. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: TripoClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Tripo configuration and credential source are required');
    this.baseUrl = config.baseUrl ?? 'https://openapi.tripo3d.com';
    const parsed = new URL(this.baseUrl);
    if (!['https:', 'http:'].includes(parsed.protocol) || (parsed.protocol === 'http:' && !['localhost', '127.0.0.1'].includes(parsed.hostname)) || parsed.pathname !== '/' || parsed.search || parsed.hash || parsed.username || parsed.password) throw new TypeError('Tripo base URL is invalid');
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 65_536;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('Tripo configuration is invalid');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the account-balance Query. */
  getAccountBalance(): GetAccountBalanceOperation { return new GetAccountBalanceOperation(this); }
}
/** Read available and frozen Tripo credits. */
export class GetAccountBalanceOperation implements Query<GetAccountBalanceInput, GetAccountBalanceOutput> {
  /** Bind an authenticated client. */
  constructor(private readonly client: TripoClient) {}
  /** Return the stable generated Query definition. */
  definition(): QueryDefinition { return GetAccountBalanceDefinition; }
  /** Send and classify one balance request. */
  async invoke(call: Call, input: GetAccountBalanceInput): Promise<QueryAttempt<GetAccountBalanceOutput>> {
    if (input === null || typeof input !== 'object' || Array.isArray(input)) return branch('defect', 'VALIDATION', 'Tripo account request is invalid');
    let credentials: Credentials;
    try { credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION', 'connection credentials are unavailable or invalid'); }
    const apiKey = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    if (!apiKey || apiKey.length > 512 || !/^[!-~]+$/.test(apiKey)) return branch('defect', 'AUTHENTICATION', 'connection credentials are unavailable or invalid');
    let response: ProviderResponse;
    try {
      response = await this.client.send({ url: `${this.client.baseUrl}/v3/account/balance`, method: 'GET', headers: { Accept: 'application/json', Authorization: `Bearer ${apiKey}` }, timeoutMs: this.client.timeoutMs, maxResponseBytes: this.client.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal });
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status === 200) return branch('invalidResponse', 'RESPONSE_TOO_LARGE', 'Tripo response exceeded the configured limit');
      if (error instanceof BodyTooLargeError && error.status !== undefined) return classifyStatus(error.status, error.headers ?? {});
      return retry('AVAILABILITY', 'Tripo request failed');
    }
    if (response.status !== 200) return classifyStatus(response.status, response.headers);
    let text: string; let wire: unknown;
    try { text = new TextDecoder('utf-8', { fatal: true }).decode(response.body); wire = JSON.parse(text) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL', 'Tripo returned an invalid response'); }
    if (!isRecord(wire) || !Number.isInteger(wire.code)) return branch('invalidResponse', 'PROTOCOL', 'Tripo returned an invalid response');
    const code = wire.code as number;
    if (code !== 0) return classifyCode(code);
    if (!isRecord(wire.data)) return branch('invalidResponse', 'PROTOCOL', 'Tripo response lacks valid credit balances');
    const values = extractCreditDecimals(text);
    if (!values || !isCreditDecimal(values.availableCredits) || !isCreditDecimal(values.frozenCredits)) return branch('invalidResponse', 'PROTOCOL', 'Tripo response lacks valid credit balances');
    return newQueryBranch('fetched', values);
  }
}
function extractCreditDecimals(text: string): GetAccountBalanceOutput | undefined {
  const data = /"data"\s*:\s*\{([^{}]*)\}/.exec(text)?.[1];
  if (!data) return undefined;
  const balance = /"balance"\s*:\s*("?)(\d+(?:\.\d+)?)\1/.exec(data)?.[2];
  const frozen = /"frozen"\s*:\s*("?)(\d+(?:\.\d+)?)\1/.exec(data)?.[2];
  return balance !== undefined && frozen !== undefined ? { availableCredits: balance, frozenCredits: frozen } : undefined;
}
function isCreditDecimal(value: string): boolean { return value.length <= 30 && /^(?:\d+)(?:\.\d{1,2})?$/.test(value); }
function classifyCode(code: number): QueryAttempt<GetAccountBalanceOutput> {
  if (code === 1000 || code === 1001) return retry('AVAILABILITY', 'Tripo service is temporarily unavailable');
  if (code === 1007 || code === 2000) return retry('RATE_LIMIT', 'Tripo rate limited the request');
  if (code === 2 || code === 1002) return branch('providerRejected', 'AUTHENTICATION', 'Tripo rejected the API key');
  if (code === 2010) return branch('providerRejected', 'QUOTA_EXHAUSTED', 'Tripo credits are exhausted');
  return branch('providerRejected', 'PROVIDER_REJECTION', 'Tripo rejected the account request');
}
function classifyStatus(status: number, headers: ProviderResponse['headers']): QueryAttempt<GetAccountBalanceOutput> {
  const value = headers['retry-after']; const delay = parseRetryAfter(Array.isArray(value) ? value[0] ?? '' : value ?? '');
  if (status === 429) return retry('RATE_LIMIT', 'Tripo rate limited the request', delay);
  if (status === 408 || status >= 500) return retry('AVAILABILITY', 'Tripo is temporarily unavailable', delay);
  const kind: Failure['kind'] = status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION';
  return branch('providerRejected', kind, `Tripo rejected the request with HTTP ${status}`);
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'tripo', operation: 'getAccountBalance', kind, message }; }
function branch(id: string, kind: Failure['kind'], message: string): QueryAttempt<GetAccountBalanceOutput> { return newQueryBranch(id, emptyOutput, {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delayMs = 0): QueryAttempt<GetAccountBalanceOutput> { return newQueryRetry(failure(kind, message), delayMs); }
