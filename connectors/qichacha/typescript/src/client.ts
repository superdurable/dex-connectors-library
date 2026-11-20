// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetEnterpriseBasicDetailsDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetEnterpriseBasicDetailsInput, GetEnterpriseBasicDetailsOutput } from './generated/models.js';

const emptyOutput: GetEnterpriseBasicDetailsOutput = { name: '', creditCode: '', registrationStatus: '' };
/** Transport and clock dependencies for deterministic request verification. */
export interface QichachaClientOptions {
  /** Dispatch provider requests; production uses the SDK transport. */
  send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Supply signing time; production uses the current clock. */
  now?: () => Date;
}
/** Read mainland enterprise registration details with one signed Query. */
export class QichachaClient {
  readonly baseUrl: string;
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  readonly now: () => Date;
  /** Validate settings without contacting Qichacha. */
  constructor(config: Config, readonly credentials: CredentialProvider<Credentials>, options: QichachaClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Qichacha configuration and credential source are required');
    const url = new URL(config.baseUrl ?? 'https://api.qichacha.com');
    if (url.protocol !== 'https:' || url.username || url.password || url.search || url.hash || url.pathname !== '/') throw new TypeError('Qichacha base URL is invalid');
    this.baseUrl = url.origin;
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 262_144;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > 16 << 20) throw new TypeError('Qichacha limits are invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }
  /** Bind the enterprise lookup Query. */
  getEnterpriseBasicDetails(): GetEnterpriseBasicDetailsOperation { return new GetEnterpriseBasicDetailsOperation(this); }
}
/** Classify signed responses without exposing personal fields. */
export class GetEnterpriseBasicDetailsOperation implements Query<GetEnterpriseBasicDetailsInput, GetEnterpriseBasicDetailsOutput> {
  /** Bind a validated client. */ constructor(private readonly client: QichachaClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return GetEnterpriseBasicDetailsDefinition; }
  /** Read one record and classify retry or terminal decisions. */
  async invoke(call: Call, input: GetEnterpriseBasicDetailsInput): Promise<QueryAttempt<GetEnterpriseBasicDetailsOutput>> {
    if (!input || typeof input.keyword !== 'string' || !input.keyword.trim() || input.keyword.trim() !== input.keyword || input.keyword.length > 1000) return branch('defect', 'VALIDATION');
    let credentials: Credentials;
    try { credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal); } catch { return branch('defect', 'AUTHENTICATION'); }
    const key = credentials.appKey instanceof SecretString ? credentials.appKey.reveal() : '';
    const secret = credentials.secretKey instanceof SecretString ? credentials.secretKey.reveal() : '';
    if (!isCredential(key) || !isCredential(secret)) return branch('defect', 'AUTHENTICATION');
    const timestamp = String(Math.floor(this.client.now().getTime() / 1000));
    const url = new URL('/ECIV4/GetBasicDetailsByName', this.client.baseUrl);
    url.searchParams.set('key', key); url.searchParams.set('keyword', input.keyword);
    const request: ProviderRequest = { url: url.toString(), method: 'GET', headers: { Accept: 'application/json', Timespan: timestamp, Token: createHash('md5').update(key + timestamp + secret).digest('hex').toUpperCase() }, timeoutMs: this.client.timeoutMs, maxResponseBytes: this.client.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.client.send(request); } catch (error) { return error instanceof BodyTooLargeError && error.status !== undefined ? branch('invalidResponse', 'RESPONSE_TOO_LARGE') : retry('AVAILABILITY'); }
    const header = Object.entries(response.headers).find(([name]) => name.toLowerCase() === 'retry-after')?.[1];
    const delay = parseRetryAfter(Array.isArray(header) ? header[0] ?? '' : header ?? '');
    if (response.status === 429) return retry('RATE_LIMIT', delay);
    if (response.status === 408 || response.status >= 500) return retry('AVAILABILITY', delay);
    if (response.body.length > this.client.maxResponseBytes) return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (response.status !== 200) return branch('providerRejected', 'PROVIDER_REJECTION');
    let document: unknown;
    try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(document) || typeof document.Status !== 'string' || !document.Status) return branch('invalidResponse', 'PROTOCOL');
    if (document.Status !== '200') return branch('providerRejected', 'PROVIDER_REJECTION');
    const result = document.Result;
    if (!isRecord(result) || typeof result.Name !== 'string' || !result.Name || result.Name.length > 1000 || typeof result.Status !== 'string' || !result.Status || result.Status.length > 100 || typeof result.CreditCode !== 'string' || result.CreditCode.length > 50) return branch('invalidResponse', 'PROTOCOL');
    if (!['0', '1', '4', '7', '9', '10', '11'].includes(String(result.EntType))) return branch('providerRejected', 'PROVIDER_REJECTION');
    return newQueryBranch('fetched', { name: result.Name, creditCode: result.CreditCode, registrationStatus: result.Status });
  }
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function isCredential(value: string): boolean { return value.length > 0 && value.length <= 512 && /^[!-~]+$/.test(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'qichacha', operation: 'getEnterpriseBasicDetails', kind, message: 'Qichacha enterprise lookup failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<GetEnterpriseBasicDetailsOutput> { return newQueryBranch(name, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<GetEnterpriseBasicDetailsOutput> { return newQueryRetry(failure(kind), delay); }
