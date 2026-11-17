// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetAccountBalanceDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetAccountBalanceInput, GetAccountBalanceOutput } from './generated/models.js';

const productionOrigin = 'https://open.dieyuyun.com';
const endpointPath = '/v1/balance';
const emptyOutput = (): GetAccountBalanceOutput => ({ availableCNY: '' });
/** Injectable provider HTTP sender. */
export type DieyuyunRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Transport dependency for tests and approved proxies. */
export interface DieyuyunClientOptions { /** SDK sender when omitted. */ send?: DieyuyunRequestSender; }
/** Authenticated Wuliang AI balance reader. */
export class DieyuyunClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: DieyuyunRequestSender;
  /** Validate the HTTPS origin and bounded request settings. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: DieyuyunClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Dieyuyun configuration and credentials are required');
    this.baseUrl = (config.baseUrl || productionOrigin).replace(/\/$/, '');
    const parsed = new URL(this.baseUrl);
    if ((parsed.protocol !== 'https:' && !(parsed.protocol === 'http:' && ['127.0.0.1', 'localhost'].includes(parsed.hostname))) || parsed.username || parsed.password || parsed.search || parsed.hash || parsed.pathname !== '/') throw new TypeError('Dieyuyun API URL must be a trusted host root');
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 65_536;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Dieyuyun request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the getAccountBalance Query. */ getAccountBalance(): GetAccountBalanceOperation { return new GetAccountBalanceOperation(this); }
  /** Resolve the secret Key and send one bounded balance request. */
  async read(call: Call): Promise<{ response: ProviderResponse } | { failure: Failure; retry?: true; retryAfterMs?: number; branch?: string }> {
    let key: string;
    try { const credentials = await this.credentials.resolve(call, call.context.cancellationSignal); key = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : ''; }
    catch { return { branch: 'defect', failure: failure('AUTHENTICATION', 'connection API Key is unavailable or invalid') }; }
    if (!validAPIKey(key)) return { branch: 'defect', failure: failure('AUTHENTICATION', 'connection API Key is unavailable or invalid') };
    const request: ProviderRequest = {
      url: `${this.baseUrl}${endpointPath}`, method: 'GET',
      headers: { Accept: 'application/json', Authorization: `Bearer ${key}` },
      timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes,
      addressPolicy: this.baseUrl.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP', signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        if (error.status === 429 || error.status === 408 || error.status >= 500) return statusRetry(error.status, error.headers ?? {});
        return { branch: 'invalidResponse', failure: failure('RESPONSE_TOO_LARGE', 'Wuliang AI response exceeded the configured limit') };
      }
      return { retry: true, failure: failure('AVAILABILITY', 'Wuliang AI request failed') };
    }
    if (response.status === 429 || response.status === 408 || response.status >= 500) return statusRetry(response.status, response.headers);
    if (response.body.length > this.maxResponseBytes) return { branch: 'invalidResponse', failure: failure('RESPONSE_TOO_LARGE', 'Wuliang AI response exceeded the configured limit') };
    return { response };
  }
}
/** Read the connected tenant's available CNY balance exactly as decimal text. */
export class GetAccountBalanceOperation implements Query<GetAccountBalanceInput, GetAccountBalanceOutput> {
  /** Bind the provider client. */ constructor(private readonly client: DieyuyunClient) {}
  /** Generated Query contract. */ definition(): QueryDefinition { return GetAccountBalanceDefinition; }
  /** Classify transport, provider, and exact numeric data outcomes. */
  async invoke(call: Call, input: GetAccountBalanceInput): Promise<QueryAttempt<GetAccountBalanceOutput>> {
    void input;
    const exchange = await this.client.read(call);
    if ('failure' in exchange) return exchange.retry ? newQueryRetry(exchange.failure, exchange.retryAfterMs ?? 0) : newQueryBranch(exchange.branch!, emptyOutput(), {}, exchange.failure);
    const response = exchange.response;
    if (response.status !== 200) return terminal('providerRejected', response.status === 401 ? 'AUTHENTICATION' : response.status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION', `Wuliang AI rejected the request with HTTP ${response.status}`);
    let raw: string; let parsed: unknown;
    try { raw = new TextDecoder('utf-8', { fatal: true }).decode(response.body); parsed = JSON.parse(raw) as unknown; }
    catch { return terminal('invalidResponse', 'PROTOCOL', 'Wuliang AI returned invalid balance data'); }
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return terminal('invalidResponse', 'PROTOCOL', 'Wuliang AI returned invalid balance data');
    const fields = parsed as Record<string, unknown>;
    const balance = typeof fields.balance === 'string' ? fields.balance : topLevelBalanceNumber(raw);
    if (fields.object !== 'balance' || fields.currency !== 'CNY' || !balance || balance.length > 64 || !/^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$/.test(balance)) return terminal('invalidResponse', 'PROTOCOL', 'Wuliang AI returned invalid balance data');
    return newQueryBranch('fetched', { availableCNY: balance });
  }
}
function topLevelBalanceNumber(raw: string): string | undefined {
  let depth = 0;
  let lastBalance: string | undefined;
  for (let index = 0; index < raw.length; index++) {
    const character = raw[index];
    if (character === '"') {
      const start = index;
      index++;
      while (index < raw.length) {
        if (raw[index] === '\\') { index += 2; continue; }
        if (raw[index] === '"') break;
        index++;
      }
      if (depth !== 1) continue;
      const key = JSON.parse(raw.slice(start, index + 1)) as string;
      let valueStart = index + 1;
      while (/\s/.test(raw[valueStart] ?? '')) valueStart++;
      if (raw[valueStart] !== ':' || key !== 'balance') continue;
      valueStart++;
      while (/\s/.test(raw[valueStart] ?? '')) valueStart++;
      const match = /^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?/.exec(raw.slice(valueStart));
      const end = valueStart + (match?.[0].length ?? 0);
      lastBalance = match && /[,}\s]/.test(raw[end] ?? '') ? match[0] : undefined;
      continue;
    }
    if (character === '{' || character === '[') depth++;
    else if (character === '}' || character === ']') depth--;
  }
  return lastBalance;
}
function validAPIKey(value: string): boolean { return value.length > 0 && value.length <= 512 && /^[!-~]+$/.test(value); }
function failure(kind: Failure['kind'], message: string): Failure { return { kind, provider: 'dieyuyun', operation: 'getAccountBalance', message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<GetAccountBalanceOutput> { return newQueryBranch(branch, emptyOutput(), {}, failure(kind, message)); }
function statusRetry(status: number, headers: ProviderResponse['headers']): { retry: true; failure: Failure; retryAfterMs: number } {
  const entry = Object.entries(headers).find(([name]) => name.toLowerCase() === 'retry-after')?.[1];
  return { retry: true, retryAfterMs: parseRetryAfter(Array.isArray(entry) ? entry[0] ?? '' : entry ?? '', new Date()),
    failure: status === 429 ? failure('RATE_LIMIT', 'Wuliang AI rate limited the request') : failure('AVAILABILITY', 'Wuliang AI is temporarily unavailable') };
}
