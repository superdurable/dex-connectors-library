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
    if ((url.protocol !== 'https:' && !(url.protocol === 'http:' && ['localhost','127.0.0.1','[::1]'].includes(url.hostname))) || url.search || url.hash || url.username || url.password) throw new TypeError('Yunpian base URL is invalid');
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
      if (error instanceof BodyTooLargeError) {
        const transient = retryStatusWithDelay(error.status ?? 0, retryDelay(error.headers ?? {}, this.now()));
        return transient ?? branch('invalidResponse', 'RESPONSE_TOO_LARGE');
      }
      return retry('TRANSPORT');
    }
    const { status, body } = response;
    const delay = retryDelay(response.headers, this.now());
    const transient = retryStatusWithDelay(status,delay);
    if (transient) return transient;
    if (body.length > this.maxResponseBytes) return branch('invalidResponse', 'RESPONSE_TOO_LARGE');

    let wire: unknown;
    try { wire = firstAccountRecord(new TextDecoder().decode(body)); }
    catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (wire === null) wire = {};
    if (!isRecord(wire)) return branch('invalidResponse', 'PROTOCOL');
    if (wire.code != null) {
      if (typeof wire.code !== 'bigint' || wire.code < -(1n << 63n) || wire.code >= (1n << 63n)) return branch('invalidResponse', 'PROTOCOL');
      if (wire.code === -5n) return retry('RATE_LIMIT', delay);
      if (wire.code === -50n || wire.code === -51n) return retry('AVAILABILITY', delay);
      if (wire.code !== 0n) return branch('providerRejected', 'PROVIDER_REJECTION');
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
function firstAccountRecord(text: string): unknown {
  // Yunpian's Go client reads one JSON value from the response stream.
  const source = text.replace(/^[ \t\r\n]+/, '');
  if (source.startsWith('null')) return null;
  if (!source.startsWith('{')) throw new TypeError('Invalid account response');
  let depth = 0;
  let quoted = false;
  for (let index = 0; index < source.length; index++) {
    const character = source[index];
    if (quoted && character === '\\') { index++; continue; }
    if (character === '"') { quoted = !quoted; continue; }
    if (quoted) continue;
    if (character === '{') depth++;
    if (character === '}' && --depth === 0) {
      return JSON.parse(source.slice(0, index + 1), (name, value: unknown, context?: {source?: string}) => {
        if (name.toLowerCase() === 'code' && typeof value === 'number' && context?.source && /^-?[0-9]{1,19}$/.test(context.source)) return BigInt(context.source);
        return isRecord(value) ? Object.fromEntries(Object.entries(value).map(([key, field]) => [key.toLowerCase(), field])) : value;
      }) as unknown;
    }
  }
  throw new TypeError('Invalid account response');
}
function retryDelay(headers: ProviderResponse['headers'], now: Date): number {
  const header = Object.entries(headers).find(([key]) => key.toLowerCase() === 'retry-after')?.[1];
  return parseRetryAfter(Array.isArray(header) ? header[0] ?? '' : header ?? '', now);
}
function retryStatusWithDelay(status: number, delay: number): QueryAttempt<AccountBalance> | undefined {
  if (status === 429) return retry('RATE_LIMIT', delay);
  if (status === 408 || status >= 500) return retry('AVAILABILITY', delay);
  return undefined;
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'yunpian', operation: 'getAccountBalance', kind, message: 'Yunpian query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<AccountBalance> { return newQueryBranch(name, { balance: 0 }, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<AccountBalance> { return newQueryRetry(failure(kind), delay); }
