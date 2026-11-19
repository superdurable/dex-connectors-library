// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetTokenUsageDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetTokenUsageInput, GetTokenUsageOutput } from './generated/models.js';

const usageEndpoint = 'https://api.ip138.com/status/';
const emptyOutput: GetTokenUsageOutput = { currentHourRequests: 0, hourlyAllowance: 0, packageQuota: 0 };

/** Send one token-usage request through a caller-owned transport. */
export type UsageRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;

/** Optional transport dependency for provider tests. */
export interface IP138ClientOptions {
  /** Request sender; production uses the Connector SDK transport. */
  send?: UsageRequestSender;
}

/** Resolve the IP138 Key for each token-usage attempt. */
export class IP138Client {
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly credentials: CredentialProvider<Credentials>;
  readonly send: UsageRequestSender;

  /** Validate settings without making a provider request. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: IP138ClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('IP138 configuration and credential source are required');
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('IP138 configuration is invalid');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the token-usage Query. */
  getTokenUsage(): GetTokenUsageOperation { return new GetTokenUsageOperation(this); }
}

/** Read current usage and purchased quota for one token. */
export class GetTokenUsageOperation implements Query<GetTokenUsageInput, GetTokenUsageOutput> {
  /** Bind an authenticated IP138 client. */
  constructor(private readonly client: IP138Client) {}

  /** Return the stable generated Query contract. */
  definition(): QueryDefinition { return GetTokenUsageDefinition; }

  /** Send and classify one token-usage request. */
  async invoke(call: Call, input: GetTokenUsageInput): Promise<QueryAttempt<GetTokenUsageOutput>> {
    if (input === null || typeof input !== 'object' || Array.isArray(input)) return branch('defect', 'VALIDATION', 'iP138 usage request is invalid');
    let credentials: Credentials;
    try { credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION', 'iP138 token is unavailable or invalid'); }
    const token = credentials.token instanceof SecretString ? credentials.token.reveal() : '';
    if (!isValidToken(token)) return branch('defect', 'AUTHENTICATION', 'iP138 token is unavailable or invalid');
    let response: ProviderResponse;
    try {
      response = await this.client.send({
        url: `${usageEndpoint}?token=${encodeURIComponent(token)}`, method: 'GET', headers: { Accept: 'application/json' },
        timeoutMs: this.client.timeoutMs, maxResponseBytes: this.client.maxResponseBytes,
        addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal,
      });
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status === 200) return branch('invalidResponse', 'RESPONSE_TOO_LARGE', 'IP138 response exceeded the configured limit');
      if (error instanceof BodyTooLargeError && error.status !== undefined) return classifyStatus(error.status, error.headers ?? {});
      return retry('AVAILABILITY', 'iP138 usage request failed');
    }
    if (response.status !== 200) return classifyStatus(response.status, response.headers);
    let document: unknown;
    try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL', 'iP138 returned invalid usage data'); }
    if (!isRecord(document) || typeof document.ret !== 'string' || !document.ret) return branch('invalidResponse', 'PROTOCOL', 'iP138 returned invalid usage data');
    if (document.ret === 'err') return branch('providerRejected', 'PROVIDER_REJECTION', 'iP138 rejected the usage request');
    if (document.ret !== 'ok' || !isRecord(document.data)) return branch('invalidResponse', 'PROTOCOL', 'iP138 returned incomplete usage data');
    const { reqs, hour, package: packageQuota } = document.data;
    if (![reqs, hour, packageQuota].every(value => Number.isSafeInteger(value) && (value as number) >= 0)) return branch('invalidResponse', 'PROTOCOL', 'iP138 returned incomplete usage data');
    return newQueryBranch('found', { currentHourRequests: reqs as number, hourlyAllowance: hour as number, packageQuota: packageQuota as number });
  }
}

function classifyStatus(status: number, headers: ProviderResponse['headers']): QueryAttempt<GetTokenUsageOutput> {
  const retryAfterHeader = headers['retry-after'];
  const retryAfterMs = parseRetryAfter(Array.isArray(retryAfterHeader) ? retryAfterHeader[0] ?? '' : retryAfterHeader ?? '');
  if (status === 429) return retry('RATE_LIMIT', 'iP138 rate limited the request', retryAfterMs);
  if (status === 408 || status >= 500) return retry('AVAILABILITY', 'iP138 usage service is temporarily unavailable', retryAfterMs);
  const kind: Failure['kind'] = status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION';
  return branch('providerRejected', kind, `iP138 rejected usage request with HTTP ${status}`);
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function isValidToken(value: string): boolean { return value.length > 0 && value.length <= 1024 && /^[!-~]+$/.test(value); }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'ip138', operation: 'getTokenUsage', kind, message }; }
function branch(id: string, kind: Failure['kind'], message: string): QueryAttempt<GetTokenUsageOutput> { return newQueryBranch(id, emptyOutput, {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delayMs = 0): QueryAttempt<GetTokenUsageOutput> { return newQueryRetry(failure(kind, message), delayMs); }
