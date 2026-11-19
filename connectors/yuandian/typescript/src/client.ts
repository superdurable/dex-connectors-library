// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetCompanyProfileDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetCompanyProfileInput, GetCompanyProfileOutput } from './generated/models.js';

const endpoint = 'https://open.chineselaw.com/open/rh_company_detail';
const emptyOutput: GetCompanyProfileOutput = { companyID: '', name: '', creditCode: '', registrationStatus: '', industry: '', companyType: '' };

/** Optional provider sender for deterministic tests. */
export interface YuandianClientOptions { send?: (request: ProviderRequest) => Promise<ProviderResponse>; }

/** Resolve the API Key for each read-only company profile Query. */
export class YuandianClient {
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Validate request limits without contacting the provider. */
  constructor(config: Config, readonly credentials: CredentialProvider<Credentials>, options: YuandianClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Yuandian configuration and credentials are required');
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('Yuandian request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the company profile Query. */
  getCompanyProfile(): GetCompanyProfileOperation { return new GetCompanyProfileOperation(this); }
}

/** Read selected registration facts by unified social credit code. */
export class GetCompanyProfileOperation implements Query<GetCompanyProfileInput, GetCompanyProfileOutput> {
  /** Bind a validated provider client. */ constructor(private readonly client: YuandianClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return GetCompanyProfileDefinition; }
  /** Classify one provider attempt and retain only selected facts. */
  async invoke(call: Call, input: GetCompanyProfileInput): Promise<QueryAttempt<GetCompanyProfileOutput>> {
    if (!input || typeof input.creditCode !== 'string' || !/^[0-9A-Z]{18}$/.test(input.creditCode)) return branch('defect', 'VALIDATION');
    let apiKey = '';
    try { const fields = await this.client.credentials.resolve(call, call.context.cancellationSignal); apiKey = fields.apiKey instanceof SecretString ? fields.apiKey.reveal() : ''; }
    catch { return branch('defect', 'AUTHENTICATION'); }
    if (!apiKey || apiKey.length > 1024 || !/^[!-~]+$/.test(apiKey)) return branch('defect', 'AUTHENTICATION');
    const url = new URL(endpoint); url.searchParams.set('tyshxydm', input.creditCode);
    let response: ProviderResponse; let tooLarge = false;
    try {
      response = await this.client.send({ url: url.toString(), method: 'GET', headers: { 'X-API-Key': apiKey, Accept: 'application/json' }, timeoutMs: this.client.timeoutMs, maxResponseBytes: this.client.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal });
    } catch (error) {
      if (!(error instanceof BodyTooLargeError) || error.status === undefined) return retry('AVAILABILITY');
      response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }; tooLarge = true;
    }
    const retryAfterValue = Object.entries(response.headers).find(([key]) => key.toLowerCase() === 'retry-after')?.[1];
    const retryAfterMs = parseRetryAfter(Array.isArray(retryAfterValue) ? retryAfterValue[0] ?? '' : retryAfterValue ?? '');
    if (response.status === 429) return retry('RATE_LIMIT', retryAfterMs);
    if (response.status === 408 || response.status >= 500) return retry('AVAILABILITY', retryAfterMs);
    if (response.status !== 200) return branch('providerRejected', response.status === 401 ? 'AUTHENTICATION' : response.status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    if (tooLarge || response.body.length > this.client.maxResponseBytes) return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let document: unknown;
    try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(document) || typeof document.code !== 'number' || !Number.isInteger(document.code)) return branch('invalidResponse', 'PROTOCOL');
    if (document.code === 429) return retry('RATE_LIMIT', retryAfterMs);
    if (document.code >= 500) return retry('AVAILABILITY', retryAfterMs);
    if (document.code !== 200) return branch('providerRejected', document.code === 401 ? 'AUTHENTICATION' : document.code === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    if (!isRecord(document.data)) return typeof document.message === 'string' && document.message.includes('未查询到') ? branch('noData', 'NOT_FOUND') : branch('invalidResponse', 'PROTOCOL');
    const data = document.data;
    if (typeof data['企业ID'] !== 'string' || !data['企业ID'] || Buffer.byteLength(data['企业ID'], 'utf8') > 255 || typeof data['企业名称'] !== 'string' || !data['企业名称'] || Buffer.byteLength(data['企业名称'], 'utf8') > 255 || data['统一社会信用代码'] !== input.creditCode || typeof data['经营状态'] !== 'string' || Buffer.byteLength(data['经营状态'], 'utf8') > 100 || typeof data['行业'] !== 'string' || Buffer.byteLength(data['行业'], 'utf8') > 255 || typeof data['企业类型'] !== 'string' || Buffer.byteLength(data['企业类型'], 'utf8') > 255) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('found', { companyID: data['企业ID'], name: data['企业名称'], creditCode: input.creditCode, registrationStatus: data['经营状态'], industry: data['行业'], companyType: data['企业类型'] });
  }
}

function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'yuandian', operation: 'getCompanyProfile', kind, message: 'Yuandian company profile failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<GetCompanyProfileOutput> { return newQueryBranch(name, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<GetCompanyProfileOutput> { return newQueryRetry(failure(kind), delay); }
