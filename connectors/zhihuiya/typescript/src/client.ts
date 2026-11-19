// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetPatentValuationDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetPatentValuationInput, GetPatentValuationOutput } from './generated/models.js';

const endpoint = 'https://connect.zhihuiya.com/patent-valuation-data/patent_valuation';
const publicationNumberPattern = /^[A-Z]{2}[A-Z0-9]{5,24}$/;
const emptyOutput: GetPatentValuationOutput = { publicationNumber: '', patentID: '', patentValueUSD: 0, legalScore: 0, marketScore: 0, economyScore: 0, strategicScore: 0, technologyScore: 0 };

/** Optional transport for deterministic patent valuation tests. */
export interface ZhihuiyaClientOptions { send?: (request: ProviderRequest) => Promise<ProviderResponse>; }

/** Resolve the Zhihuiya API Key for each patent read. */
export class ZhihuiyaClient {
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Validate request limits without contacting Zhihuiya. */
  constructor(config: Config, readonly credentials: CredentialProvider<Credentials>, options: ZhihuiyaClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Zhihuiya configuration and credentials are required');
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('Zhihuiya request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the read-only patent valuation Query. */
  getPatentValuation(): GetPatentValuationOperation { return new GetPatentValuationOperation(this); }
}

/** Read one provider valuation without returning raw account data. */
export class GetPatentValuationOperation implements Query<GetPatentValuationInput, GetPatentValuationOutput> {
  /** Bind the configured provider client. */
  constructor(private readonly client: ZhihuiyaClient) {}
  /** Return the generated Query contract. */
  definition(): QueryDefinition { return GetPatentValuationDefinition; }
  /** Send and classify one patent valuation request. */
  async invoke(call: Call, input: GetPatentValuationInput): Promise<QueryAttempt<GetPatentValuationOutput>> {
    if (!input || typeof input.publicationNumber !== 'string' || !publicationNumberPattern.test(input.publicationNumber)) return branch('defect', 'VALIDATION');
    let credentials: Credentials;
    try { credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION'); }
    const apiKey = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    if (!isValidAPIKey(apiKey)) return branch('defect', 'AUTHENTICATION');
    let response: ProviderResponse;
    try {
      response = await this.client.send({ url: `${endpoint}?patent_number=${encodeURIComponent(input.publicationNumber)}`, method: 'GET',
        headers: { Authorization: `Bearer ${apiKey}`, Accept: 'application/json' }, timeoutMs: this.client.timeoutMs,
        maxResponseBytes: this.client.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal });
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status === 200) return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
      if (error instanceof BodyTooLargeError && error.status !== undefined) return classifyStatus(error.status, error.headers ?? {});
      return retry('AVAILABILITY');
    }
    if (response.status !== 200) return classifyStatus(response.status, response.headers);
    if (response.body.length > this.client.maxResponseBytes) return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let document: unknown;
    try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(document) || typeof document.status !== 'boolean' || !Number.isInteger(document.error_code)) return branch('invalidResponse', 'PROTOCOL');
    const errorCode = document.error_code as number;
    if (!document.status || errorCode !== 0) {
      const delay = retryDelay(response.headers);
      if ([67200000, 67200001, 67200002].includes(errorCode)) return retry('RATE_LIMIT', delay);
      if ([68300005, 68300008, 67200100].includes(errorCode)) return retry('AVAILABILITY', delay);
      return branch('providerRejected', 'PROVIDER_REJECTION');
    }
    if (!Array.isArray(document.data)) return branch('invalidResponse', 'PROTOCOL');
    if (document.data.length === 0) return branch('noData', 'NOT_FOUND');
    if (document.data.length !== 1 || !isRecord(document.data[0])) return branch('invalidResponse', 'PROTOCOL');
    const patent = document.data[0];
    if (patent.pn !== input.publicationNumber || typeof patent.patent_id !== 'string' || !patent.patent_id || !isRecord(patent.patsnap_patent_valuation)) return branch('invalidResponse', 'PROTOCOL');
    const valuation = patent.patsnap_patent_valuation;
    const values = ['patent_value', 'patsnap_legal', 'patsnap_market', 'patsnap_economy', 'patsnap_strategic', 'patsnap_technology'].map(field => integerOrZero(valuation[field]));
    if (values.some(value => value === undefined)) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('found', { publicationNumber: patent.pn, patentID: patent.patent_id,
      patentValueUSD: values[0]!, legalScore: values[1]!, marketScore: values[2]!, economyScore: values[3]!, strategicScore: values[4]!, technologyScore: values[5]! });
  }
}

function classifyStatus(status: number, headers: ProviderResponse['headers']): QueryAttempt<GetPatentValuationOutput> {
  if (status === 429) return retry('RATE_LIMIT', retryDelay(headers));
  if (status === 408 || status >= 500) return retry('AVAILABILITY', retryDelay(headers));
  if (status === 404) return branch('noData', 'NOT_FOUND');
  const kind: Failure['kind'] = status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION';
  return branch('providerRejected', kind);
}
function retryDelay(headers: ProviderResponse['headers']): number { const value = headers['retry-after']; return parseRetryAfter(Array.isArray(value) ? value[0] ?? '' : value ?? ''); }
function integerOrZero(value: unknown): number | undefined { if (value === undefined) return 0; return typeof value === 'number' && Number.isSafeInteger(value) ? value : undefined; }
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function isValidAPIKey(value: string): boolean { return value.startsWith('sk-') && value.length <= 1024 && /^[!-~]+$/.test(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'zhihuiya', operation: 'getPatentValuation', kind, message: 'Zhihuiya patent valuation failed' }; }
function branch(id: string, kind: Failure['kind']): QueryAttempt<GetPatentValuationOutput> { return newQueryBranch(id, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delayMs = 0): QueryAttempt<GetPatentValuationOutput> { return newQueryRetry(failure(kind), delayMs); }
