// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetAccountInfoDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetAccountInfoInput, GetAccountInfoOutput } from './generated/models.js';

const accountInfoEndpoint = 'https://v1.apizero.cn/api/user-info';
const emptyOutput: GetAccountInfoOutput = { scope: '' };

/** Send one account-information request through a caller-owned transport. */
export type AccountRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;

/** Optional transport dependency for provider tests. */
export interface ApiZeroClientOptions {
  /** Request sender; production uses the Connector SDK transport. */
  send?: AccountRequestSender;
}

/** Resolve ApiZero credentials for each account-information attempt. */
export class ApiZeroClient {
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly credentials: CredentialProvider<Credentials>;
  readonly send: AccountRequestSender;

  /** Validate configuration without making a provider request. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: ApiZeroClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('ApiZero configuration and credential source are required');
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('ApiZero configuration is invalid');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the read-only account-information Query. */
  getAccountInfo(): GetAccountInfoOperation { return new GetAccountInfoOperation(this); }
}

/** Read the current Key's wallet balance or remaining prepaid quota. */
export class GetAccountInfoOperation implements Query<GetAccountInfoInput, GetAccountInfoOutput> {
  /** Bind an authenticated ApiZero client. */
  constructor(private readonly client: ApiZeroClient) {}

  /** Return the stable generated Query contract. */
  definition(): QueryDefinition { return GetAccountInfoDefinition; }

  /** Send and classify one account-information request. */
  async invoke(call: Call, input: GetAccountInfoInput): Promise<QueryAttempt<GetAccountInfoOutput>> {
    if (input === null || typeof input !== 'object' || Array.isArray(input)) return branch('defect', 'VALIDATION', 'ApiZero account request is invalid');
    let credentials: Credentials;
    try { credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION', 'ApiZero API Key is unavailable or invalid'); }
    const apiKey = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    if (!isValidAPIKey(apiKey)) return branch('defect', 'AUTHENTICATION', 'ApiZero API Key is unavailable or invalid');
    let response: ProviderResponse;
    try {
      response = await this.client.send({
        url: accountInfoEndpoint, method: 'GET', headers: { Authorization: `Bearer ${apiKey}`, Accept: 'application/json' },
        timeoutMs: this.client.timeoutMs, maxResponseBytes: this.client.maxResponseBytes,
        addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal,
      });
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        if (error.status === 429) return retry('RATE_LIMIT', 'ApiZero rate limited the request', retryAfter(error.headers ?? {}));
        return branch('invalidResponse', 'RESPONSE_TOO_LARGE', 'ApiZero response exceeded the configured limit');
      }
      return retry('AVAILABILITY', 'ApiZero account request failed');
    }
    const delayMs = retryAfter(response.headers);
    if (response.status === 429) return retry('RATE_LIMIT', 'ApiZero rate limited the request', delayMs);
    let document: unknown;
    try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; }
    catch { return classifyUnparsedResponse(response.status, delayMs); }
    if (!isRecord(document) || !Number.isInteger(document.code)) return classifyUnparsedResponse(response.status, delayMs);
    const code = document.code as number;
    if (code !== 0) return classifyBusinessCode(code, delayMs);
    if (response.status !== 200) return branch('invalidResponse', 'PROTOCOL', 'ApiZero returned success with unexpected HTTP status');
    const data = document.data;
    if (!isRecord(data)) return branch('invalidResponse', 'PROTOCOL', 'ApiZero returned incomplete account data');
    if (data.scope === 'account' && isRecord(data.balance) && typeof data.balance.yuan === 'string' && isValidYuan(data.balance.yuan)) return newQueryBranch('found', { scope: 'account', balanceYuan: data.balance.yuan });
    if (data.scope === 'key' && isRecord(data.key) && Number.isSafeInteger(data.key.quota_remaining) && (data.key.quota_remaining as number) >= 0) return newQueryBranch('found', { scope: 'key', keyQuotaRemaining: data.key.quota_remaining as number });
    return branch('invalidResponse', 'PROTOCOL', 'ApiZero returned incomplete account data');
  }
}

function classifyBusinessCode(code: number, delayMs: number): QueryAttempt<GetAccountInfoOutput> {
  if (code === 4029) return retry('RATE_LIMIT', 'ApiZero per-second rate limit exceeded', delayMs);
  if (code === 5000 || code === 5020 || code === 5021 || code === 5030) return retry('AVAILABILITY', 'ApiZero service is temporarily unavailable', delayMs);
  if (code === 4011 || code === 4015) return branch('providerRejected', 'AUTHENTICATION', 'ApiZero rejected the API Key');
  if (code === 4013 || code === 4014) return branch('providerRejected', 'AUTHORIZATION', 'ApiZero API Key cannot access this endpoint');
  return branch('providerRejected', 'PROVIDER_REJECTION', `ApiZero rejected account request with code ${code}`);
}
function classifyUnparsedResponse(status: number, delayMs: number): QueryAttempt<GetAccountInfoOutput> {
  if (status === 408 || status >= 500) return retry('AVAILABILITY', 'ApiZero service is temporarily unavailable', delayMs);
  if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION', `ApiZero rejected account request with HTTP ${status}`);
  return branch('invalidResponse', 'PROTOCOL', 'ApiZero returned invalid account data');
}
function retryAfter(headers: ProviderResponse['headers']): number {
  const header = headers['retry-after'];
  return parseRetryAfter(Array.isArray(header) ? header[0] ?? '' : header ?? '');
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function isValidAPIKey(value: string): boolean { return value.startsWith('sk_live_') && value.length <= 1024 && /^[!-~]+$/.test(value); }
function isValidYuan(value: string): boolean { return value.length <= 32 && /^\d+(?:\.\d+)?$/.test(value); }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'apizero', operation: 'getAccountInfo', kind, message }; }
function branch(id: string, kind: Failure['kind'], message: string): QueryAttempt<GetAccountInfoOutput> { return newQueryBranch(id, emptyOutput, {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delayMs = 0): QueryAttempt<GetAccountInfoOutput> { return newQueryRetry(failure(kind, message), delayMs); }
