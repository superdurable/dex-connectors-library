// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetCompanyProfileDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetCompanyProfileInput, GetCompanyProfileOutput } from './generated/models.js';

const emptyOutput: GetCompanyProfileOutput = { companyID: 0, name: '', registrationStatus: '', creditCode: '', industry: '', registeredCapital: '', companyType: '' };
/** Optional provider transport for deterministic tests. */
export interface TianyanchaClientOptions { /** Sender; production uses Connector SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>; }
/** Read provider data through one credentialed Query. */
export class TianyanchaClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Validate connector settings without contacting the provider. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: TianyanchaClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Tianyancha configuration and credential source are required');
    this.baseUrl = 'https://open.api.tianyancha.com';
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('Tianyancha request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the read-only provider Query. */ getCompanyProfile(): GetCompanyProfileOperation { return new GetCompanyProfileOperation(this); }
  /** Dispatch one bounded provider request. */
  async request(call: Call, input: GetCompanyProfileInput): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge' | 'input'; retryAfterMs: number }> {
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    const keyword = input?.keyword;
    if (typeof keyword !== 'string' || !keyword || keyword.trim() !== keyword || Buffer.byteLength(keyword, 'utf8') > 255 || /[\r\n\0]/.test(keyword)) return { error: 'input', retryAfterMs: 0 };
    const key = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    if (!key || key.length > 1024 || !/^[!-~]+$/.test(key)) return { error: 'credential', retryAfterMs: 0 };
    const request: ProviderRequest = { url: `${this.baseUrl}/services/v4/open/baseinfoV3?${new URLSearchParams({ keyword }).toString()}`,  method: 'GET', headers: { Accept: 'application/json', Authorization: key }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    const retryAfterValue = Object.entries(response.headers).find(([key]) => key.toLowerCase() === 'retry-after')?.[1];
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(Array.isArray(retryAfterValue) ? retryAfterValue[0] ?? '' : retryAfterValue ?? '') };
  }
}
/** Classify the provider response without storing credentials in Dex state. */
export class GetCompanyProfileOperation implements Query<GetCompanyProfileInput, GetCompanyProfileOutput> {
  /** Bind one validated client. */ constructor(private readonly client: TianyanchaClient) {}
  /** Return the generated operation contract. */ definition(): QueryDefinition { return GetCompanyProfileDefinition; }
  /** Perform one safe read and classify its outcome. */
  async invoke(call: Call, input: GetCompanyProfileInput): Promise<QueryAttempt<GetCompanyProfileOutput>> {
    const exchange = await this.client.request(call, input);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'input') return branch('defect', 'VALIDATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    let document: unknown;
    try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(document) || typeof document.error_code !== 'number' || !Number.isInteger(document.error_code)) return branch('invalidResponse', 'PROTOCOL');
    if (document.error_code === 300000) return branch('noData', 'NOT_FOUND');
    if (document.error_code === 300004) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (document.error_code === 300001 || document.error_code === 300012) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (document.error_code !== 0) return branch('providerRejected', [300002,300003,300009].includes(document.error_code) ? 'AUTHENTICATION' : [300005,300011].includes(document.error_code) ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    const profile = document.result;
    if (!isRecord(profile) || typeof profile.id !== 'number' || !Number.isSafeInteger(profile.id) || profile.id <= 0 || typeof profile.name !== 'string' || !profile.name || Buffer.byteLength(profile.name, 'utf8') > 255) return branch('invalidResponse', 'PROTOCOL');
    const fields = ['regStatus', 'creditCode', 'industry', 'regCapital', 'companyOrgType'] as const;
    const maximumLengths = [100, 100, 255, 100, 255];
    for (let index = 0; index < fields.length; index++) { const value = profile[fields[index]!]; if (value !== undefined && (typeof value !== 'string' || Buffer.byteLength(value, 'utf8') > maximumLengths[index]!)) return branch('invalidResponse', 'PROTOCOL'); }
    return newQueryBranch('found', { companyID: profile.id, name: profile.name, registrationStatus: typeof profile.regStatus === 'string' ? profile.regStatus : '', creditCode: typeof profile.creditCode === 'string' ? profile.creditCode : '', industry: typeof profile.industry === 'string' ? profile.industry : '', registeredCapital: typeof profile.regCapital === 'string' ? profile.regCapital : '', companyType: typeof profile.companyOrgType === 'string' ? profile.companyOrgType : '' });
  }
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'tianyancha', operation: 'getCompanyProfile', kind, message: 'Tianyancha provider request failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<GetCompanyProfileOutput> { return newQueryBranch(name, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<GetCompanyProfileOutput> { return newQueryRetry(failure(kind), delay); }
