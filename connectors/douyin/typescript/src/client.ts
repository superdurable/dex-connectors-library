// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetBehaviorSummaryDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetBehaviorSummaryInput, GetBehaviorSummaryOutput } from './generated/models.js';

const tokenEndpoint = 'https://open.douyin.com/oauth/client_token/';
const behaviorEndpoint = 'https://open.douyin.com/api/platform/v2/data_analysis/query_behavior_data/';
const emptyOutput: GetBehaviorSummaryOutput = { activeUserCount: 0, newUserCount: 0, totalUserCount: 0, openCount: 0, shareCount: 0 };
type Exchange = { response?: ProviderResponse; error?: 'transport' | 'tooLarge'; retryAfterMs: number };
type CachedToken = { value: string; expiresAt: number };

/** Optional provider sender and clock for deterministic conformance replay. */
export interface DouyinClientOptions {
  /** Sender; omitted uses the SDK HTTPS transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Clock; omitted uses the system wall clock. */ now?: () => Date;
}

/** Resolve mini app credentials and read aggregate behavior counts. */
export class DouyinClient {
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;
  private readonly tokens = new Map<string, CachedToken>();

  /** Validate local settings without contacting Douyin. */
  constructor(config: Config, readonly credentials: CredentialProvider<Credentials>, options: DouyinClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Douyin configuration and credentials are required');
    this.timeoutMs = config.timeoutMs || 10_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('Douyin request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the daily aggregate Query. */ getBehaviorSummary(): GetBehaviorSummaryOperation { return new GetBehaviorSummaryOperation(this); }
  /** Return the current clock value. */ currentTime(): Date { return this.now(); }
  /** Forget a rejected short-lived token. */ forgetToken(credentials: Credentials): void { this.tokens.delete(tokenKey(credentials)); }

  /** Acquire a cached token or make one bounded OAuth client-credential request. */
  async tokenFor(call: Call, credentials: Credentials): Promise<{ token?: string; attempt?: QueryAttempt<GetBehaviorSummaryOutput> }> {
    const key = tokenKey(credentials);
    const cached = this.tokens.get(key);
    if (cached && cached.expiresAt > this.now().getTime() + 60_000) return { token: cached.value };
    const body = Buffer.from(JSON.stringify({ grant_type: 'client_credential', client_key: credentials.clientKey, client_secret: credentials.clientSecret.reveal() }), 'utf8');
    const exchange = await this.exchange(call, { url: tokenEndpoint, method: 'POST', headers: { 'Content-Type': 'application/json', Accept: 'application/json' }, body, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal });
    const httpAttempt = classifyHTTP(exchange);
    if (httpAttempt) return { attempt: httpAttempt };
    if (exchange.error === 'tooLarge') return { attempt: branch('invalidResponse', 'RESPONSE_TOO_LARGE') };
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(exchange.response!.body)); } catch { return { attempt: branch('invalidResponse', 'PROTOCOL') }; }
    if (!isRecord(wire) || !isRecord(wire.data) || !Number.isInteger(wire.data.error_code)) return { attempt: branch('invalidResponse', 'PROTOCOL') };
    if (wire.data.error_code === 10020) return { attempt: retry('RATE_LIMIT') };
    if (wire.data.error_code !== 0) return { attempt: branch('providerRejected', 'AUTHENTICATION') };
    if (!validOpaque(wire.data.access_token) || !Number.isInteger(wire.data.expires_in) || (wire.data.expires_in as number) <= 60 || (wire.data.expires_in as number) > 86_400) return { attempt: branch('invalidResponse', 'PROTOCOL') };
    const token = wire.data.access_token as string;
    this.tokens.set(key, { value: token, expiresAt: this.now().getTime() + (wire.data.expires_in as number) * 1000 });
    return { token };
  }

  /** Send one daily behavior summary request. */
  async readBehavior(call: Call, token: string, startSeconds: number): Promise<Exchange> {
    const query = new URLSearchParams({ start_time: String(startSeconds), end_time: String(startSeconds + 86_399) });
    return this.exchange(call, { url: behaviorEndpoint + '?' + query.toString(), method: 'GET', headers: { Accept: 'application/json', 'access-token': token }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal });
  }

  private async exchange(_call: Call, request: ProviderRequest): Promise<Exchange> {
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** Read completed aggregate activity for one China Standard Time day. */
export class GetBehaviorSummaryOperation implements Query<GetBehaviorSummaryInput, GetBehaviorSummaryOutput> {
  /** Bind the provider client. */ constructor(private readonly client: DouyinClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return GetBehaviorSummaryDefinition; }
  /** Classify token and report exchanges according to the Go contract. */
  async invoke(call: Call, input: GetBehaviorSummaryInput): Promise<QueryAttempt<GetBehaviorSummaryOutput>> {
    const date = input?.date;
    if (typeof date !== 'string' || !/^\d{4}-\d{2}-\d{2}$/.test(date)) return branch('defect', 'VALIDATION');
    const start = new Date(date + 'T00:00:00+08:00');
    if (!Number.isFinite(start.getTime()) || new Date(start.getTime() + 8 * 3600_000).toISOString().slice(0, 10) !== date) return branch('defect', 'VALIDATION');
    const chinaNow = new Date(this.client.currentTime().getTime() + 8 * 3600_000);
    const today = new Date(chinaNow.toISOString().slice(0, 10) + 'T00:00:00+08:00');
    const latest = today.getTime() - (chinaNow.getUTCHours() < 10 ? 2 : 1) * 86_400_000;
    if (start.getTime() < today.getTime() - 90 * 86_400_000 || start.getTime() > latest) return branch('defect', 'VALIDATION');
    let credentials: Credentials;
    try { credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal); } catch { return branch('defect', 'AUTHENTICATION'); }
    if (!validOpaque(credentials.clientKey) || !(credentials.clientSecret instanceof SecretString) || !validOpaque(credentials.clientSecret.reveal())) return branch('defect', 'AUTHENTICATION');
    const authorization = await this.client.tokenFor(call, credentials);
    if (authorization.attempt) return authorization.attempt;
    const exchange = await this.client.readBehavior(call, authorization.token!, Math.floor(start.getTime() / 1000));
    const httpAttempt = classifyHTTP(exchange);
    if (httpAttempt) return httpAttempt;
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(exchange.response!.body)); } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(wire) || !Number.isInteger(wire.err_no)) return branch('invalidResponse', 'PROTOCOL');
    if (wire.err_no === -1) return retry('AVAILABILITY');
    if (wire.err_no === 28001008) { this.client.forgetToken(credentials); return retry('AUTHENTICATION'); }
    if (wire.err_no !== 0) return branch('providerRejected', 'PROVIDER_REJECTION');
    if (!isRecord(wire.data) || !isRecord(wire.data.sum)) return branch('invalidResponse', 'PROTOCOL');
    const sum = wire.data.sum;
    const values = [sum.active_user_num, sum.new_user_num, sum.total_user_num, sum.open_time, sum.share_time];
    if (values.some(value => !Number.isSafeInteger(value) || (value as number) < 0)) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('found', { activeUserCount: values[0] as number, newUserCount: values[1] as number, totalUserCount: values[2] as number, openCount: values[3] as number, shareCount: values[4] as number }, {});
  }
}

function classifyHTTP(exchange: Exchange): QueryAttempt<GetBehaviorSummaryOutput> | undefined {
  if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
  const status = exchange.response.status;
  if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
  if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
  if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
  return undefined;
}
function validOpaque(value: unknown): value is string { return typeof value === 'string' && !!value && value.length <= 1024 && !/[^\x21-\x7e]/.test(value); }
function tokenKey(credentials: Credentials): string { return createHash('sha256').update(credentials.clientKey + '\0' + credentials.clientSecret.reveal()).digest('hex'); }
function isRecord(value: unknown): value is Record<string, unknown> { return value !== null && typeof value === 'object' && !Array.isArray(value); }
function header(headers: Record<string, string | string[] | undefined>, name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'douyin', operation: 'getBehaviorSummary', kind, message: 'Douyin behavior query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<GetBehaviorSummaryOutput> { return newQueryBranch(name, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<GetBehaviorSummaryOutput> { return newQueryRetry(failure(kind), delay); }
