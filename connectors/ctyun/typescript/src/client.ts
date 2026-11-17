// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash, createHmac, randomUUID } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListKnowledgeBasesDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListKnowledgeBasesInput, ListKnowledgeBasesOutput, KnowledgeBaseSummary } from './generated/models.js';

const productionOrigin = 'https://kqa-global.ctapi.ctyun.cn';
const endpointPath = '/openapi/v1/infobases/list';
const emptyOutput = (): ListKnowledgeBasesOutput => ({ knowledgeBases: [] });
/** Injectable provider sender. */
export type CTYunRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Request dependencies for deterministic tests and approved transports. */
export interface CTYunClientOptions {
  /** SDK sender when omitted. */ send?: CTYunRequestSender;
  /** System clock when omitted. */ now?: () => Date;
  /** Fresh UUID generator when omitted. */ requestId?: () => string;
}
/** Signed CTYun Knowledge Base Q&A client. */
export class CTYunClient {
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: CTYunRequestSender;
  private readonly now: () => Date;
  private readonly requestId: () => string;
  /** Validate the fixed production origin and request limits. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: CTYunClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('CTYun configuration and credentials are required');
    const baseUrl = config.baseUrl || productionOrigin;
    if (baseUrl !== productionOrigin && baseUrl !== `${productionOrigin}/`) throw new TypeError('CTYun base URL must be the documented production origin');
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('CTYun request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
    this.requestId = options.requestId ?? randomUUID;
  }
  /** Bind the listKnowledgeBases Query. */
  listKnowledgeBases(): ListKnowledgeBasesOperation { return new ListKnowledgeBasesOperation(this); }
  /** Sign and send one read request. */
  async read(call: Call): Promise<{ response: ProviderResponse } | { failure: Failure; retry?: true; retryAfterMs?: number; branch?: string }> {
    let accessKey: string; let securityKey: string; let tenantId: string;
    try {
      const credential = await this.credentials.resolve(call, call.context.cancellationSignal);
      accessKey = revealCredential(credential.accessKey); securityKey = revealCredential(credential.securityKey); tenantId = revealCredential(credential.tenantId);
    } catch { return { branch: 'defect', failure: failure('AUTHENTICATION', 'CTYun credentials or tenant ID are unavailable or invalid') }; }
    if (![accessKey, securityKey, tenantId].every(validCredential)) return { branch: 'defect', failure: failure('AUTHENTICATION', 'CTYun credentials or tenant ID are unavailable or invalid') };
    let requestId: string;
    try { requestId = this.requestId(); if (!requestId) throw new TypeError('empty request ID'); }
    catch { return { retry: true, failure: failure('AVAILABILITY', 'CTYun request ID could not be generated') }; }
    const date = beijingEopDate(this.now());
    const request: ProviderRequest = {
      url: `${productionOrigin}${endpointPath}`, method: 'GET',
      headers: { 'ctyun-eop-request-id': requestId, 'eop-date': date, tenantId,
        'Eop-Authorization': signAuthorization(accessKey, securityKey, requestId, date), Accept: 'application/json' },
      timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes,
      addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        if (error.status === 429 || error.status === 408 || error.status >= 500) return retryForStatus(error.status, error.headers ?? {});
        return { branch: 'invalidResponse', failure: failure('RESPONSE_TOO_LARGE', 'CTYun knowledge-base response exceeded the configured limit') };
      }
      return { retry: true, failure: failure('AVAILABILITY', 'CTYun knowledge-base transport failed') };
    }
    if (response.status === 429 || response.status === 408 || response.status >= 500) return retryForStatus(response.status, response.headers);
    if (response.body.length > this.maxResponseBytes) return { branch: 'invalidResponse', failure: failure('RESPONSE_TOO_LARGE', 'CTYun knowledge-base response exceeded the configured limit') };
    return { response };
  }
}
/** Read accessible knowledge-base metadata without document contents. */
export class ListKnowledgeBasesOperation implements Query<ListKnowledgeBasesInput, ListKnowledgeBasesOutput> {
  /** Bind the provider client. */ constructor(private readonly client: CTYunClient) {}
  /** Generated Query contract. */ definition(): QueryDefinition { return ListKnowledgeBasesDefinition; }
  /** Dispatch a signed request and classify the provider result. */
  async invoke(call: Call, input: ListKnowledgeBasesInput): Promise<QueryAttempt<ListKnowledgeBasesOutput>> {
    void input;
    const exchange = await this.client.read(call);
    if ('failure' in exchange) return exchange.retry ? newQueryRetry(exchange.failure, exchange.retryAfterMs ?? 0) : newQueryBranch(exchange.branch!, emptyOutput(), {}, exchange.failure);
    const response = exchange.response;
    if (response.status !== 200) return terminal('providerRejected', response.status === 401 || response.status === 403 ? 'AUTHENTICATION' : 'PROVIDER_REJECTION', `CTYun rejected knowledge-base request with HTTP ${response.status}`);
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; }
    catch { return terminal('invalidResponse', 'PROTOCOL', 'CTYun returned invalid knowledge-base JSON'); }
    if (!wire || typeof wire !== 'object' || Array.isArray(wire)) return terminal('invalidResponse', 'PROTOCOL', 'CTYun returned invalid knowledge-base JSON');
    const fields = wire as Record<string, unknown>;
    const status = statusCode(fields.statusCode);
    if (!status) return terminal('invalidResponse', 'PROTOCOL', 'CTYun returned invalid status code');
    if (status !== '200') {
      if (status === '40002' || status === '40004') return newQueryRetry(failure('AVAILABILITY', 'CTYun knowledge-base service reported a temporary error'), 0);
      const kind = ['40001', '40003', '40016'].includes(status) || (typeof fields.error === 'string' && fields.error.startsWith('CTAPI_100')) ? 'AUTHENTICATION' : 'PROVIDER_REJECTION';
      return terminal('providerRejected', kind, `CTYun rejected knowledge-base request with status ${status}`);
    }
    if (!Array.isArray(fields.returnObj)) return terminal('invalidResponse', 'PROTOCOL', 'CTYun knowledge-base list is missing');
    const knowledgeBases: KnowledgeBaseSummary[] = [];
    for (const entry of fields.returnObj) {
      if (!entry || typeof entry !== 'object' || Array.isArray(entry)) return terminal('invalidResponse', 'PROTOCOL', 'CTYun returned invalid knowledge-base metadata');
      const item = entry as Record<string, unknown>;
      if (typeof item.id !== 'number' || !Number.isSafeInteger(item.id) || item.id <= 0 || typeof item.name !== 'string' || !item.name || Buffer.byteLength(item.name) > 1024 || typeof item.count !== 'number' || !Number.isSafeInteger(item.count) || item.count < 0) return terminal('invalidResponse', 'PROTOCOL', 'CTYun returned invalid knowledge-base metadata');
      knowledgeBases.push({ id: item.id, name: item.name, fileCount: item.count });
    }
    return newQueryBranch('listed', { knowledgeBases });
  }
}
/** Build CTYun Eop-Authorization from the documented canonical request. */
export function signAuthorization(accessKey: string, securityKey: string, requestId: string, date: string): string {
  const emptyBodyHash = createHash('sha256').digest('hex');
  const canonical = `ctyun-eop-request-id:${requestId}\neop-date:${date}\n\n${emptyBodyHash}`;
  const dateSecret = hmac(securityKey, date);
  const accessSecret = hmac(dateSecret, accessKey);
  const signatureSecret = hmac(accessSecret, date.slice(0, 8));
  return `${accessKey} headers=ctyun-eop-request-id;eop-date Signature=${hmac(signatureSecret, canonical).toString('base64')}`;
}
function hmac(key: string | Buffer, value: string): Buffer { return createHmac('sha256', key).update(value).digest(); }
function beijingEopDate(now: Date): string {
  const beijing = new Date(now.getTime() + 8 * 3_600_000);
  return beijing.toISOString().slice(0, 19).replace(/[-:]/g, '') + 'Z';
}
function revealCredential(value: unknown): string { return value instanceof SecretString ? value.reveal() : ''; }
function validCredential(value: string): boolean { return value.length > 0 && value.length <= 2048 && /^[!-~]+$/.test(value); }
function statusCode(value: unknown): string | undefined { return typeof value === 'string' && value ? value : typeof value === 'number' && Number.isSafeInteger(value) && value > 0 ? String(value) : undefined; }
function failure(kind: Failure['kind'], message: string): Failure { return { kind, provider: 'ctyun', operation: 'listKnowledgeBases', message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<ListKnowledgeBasesOutput> { return newQueryBranch(branch, emptyOutput(), {}, failure(kind, message)); }
function retryForStatus(status: number, headers: ProviderResponse['headers']): { retry: true; failure: Failure; retryAfterMs: number } {
  const entry = Object.entries(headers).find(([name]) => name.toLowerCase() === 'retry-after')?.[1];
  return { retry: true, retryAfterMs: parseRetryAfter(Array.isArray(entry) ? entry[0] ?? '' : entry ?? '', new Date()),
    failure: status === 429 ? failure('RATE_LIMIT', 'CTYun knowledge-base request rate limited') : failure('AVAILABILITY', 'CTYun knowledge-base service temporarily unavailable') };
}
