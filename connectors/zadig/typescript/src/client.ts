// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListProjectsDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListProjectsInput, ListProjectsOutput } from './generated/models.js';

/** Optional provider sender and clock for deterministic tests. */
export interface ZadigClientOptions {
  /** Send one bounded provider request. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Return the current time for signed or retry requests. */ now?: () => Date;
}

/** Read Zadig provider data through Dex Queries. */
export class ZadigClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;
  /** Validate immutable provider settings. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: ZadigClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Zadig configuration and credentials are required');
    this.baseUrl = (config.baseUrl ?? '').replace(/\/$/, '');
    const url = new URL(this.baseUrl);
    if ((url.protocol !== 'https:' && !(url.protocol === 'http:' && ['localhost','127.0.0.1','[::1]'].includes(url.hostname))) || url.search || url.hash || url.pathname !== '/') throw new TypeError('Zadig base URL is invalid');
    this.timeoutMs = config.timeoutMs ?? 10000;
    this.maxResponseBytes = config.maxResponseBytes ?? 2097152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > 16 << 20) throw new TypeError('Zadig request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }
  /** Bind the provider Query. */ listProjects(): ListProjectsOperation { return new ListProjectsOperation(this); }
  /** Execute one provider request and classify its outcome. */
  async invoke(call: Call, input: ListProjectsInput): Promise<QueryAttempt<ListProjectsOutput>> {
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION'); }
    const secret = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    if (!isHeaderSafe(secret) || secret.length > 512) return branch('defect', 'AUTHENTICATION');
    const pageNum = input.pageNum || 1; const pageSize = input.pageSize || 20;
    if (!Number.isSafeInteger(pageNum) || pageNum < 1 || pageNum > 1000000 || !Number.isSafeInteger(pageSize) || pageSize < 1 || pageSize > 100) return branch('defect', 'VALIDATION');
    const url = new URL(this.baseUrl + '/openapi/projects/project');
    url.searchParams.set('pageNum', String(pageNum)); url.searchParams.set('pageSize', String(pageSize));
    const request: ProviderRequest = { url: url.toString(), method: 'GET', headers: { Accept: 'application/json', Authorization: 'Bearer ' + secret }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: this.baseUrl.startsWith('https:') ? 'publicHTTPS' : 'privateNetworkHTTP', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
      return retry('AVAILABILITY');
    }
    const { status, body } = response;
    const retryHeader = Object.entries(response.headers).find(([key]) => key.toLowerCase() === 'retry-after')?.[1];
    const delay = parseRetryAfter(Array.isArray(retryHeader) ? retryHeader[0] ?? '' : retryHeader ?? '', this.now());
    if (status === 429) return retry('RATE_LIMIT', delay);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', delay);
    if (body.length > this.maxResponseBytes) return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : status === 400 || status === 404 ? 'VALIDATION' : 'PROVIDER_REJECTION');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(wire) || !Number.isSafeInteger(wire.total) || (wire.total as number) < 0 || !Array.isArray(wire.projects) || wire.projects.length > (input.pageSize || 20)) return branch('invalidResponse', 'PROTOCOL');
    const projects: ListProjectsOutput['projects'] = [];
    for (const item of wire.projects) {
      if (!isRecord(item) || typeof item.project_key !== 'string' || !item.project_key || item.project_key.length > 256 || item.project_key.trim() !== item.project_key || typeof item.project_name !== 'string' || !item.project_name || item.project_name.length > 512 || typeof item.deploy_type !== 'string' || item.deploy_type.length > 64) return branch('invalidResponse', 'PROTOCOL');
      projects.push({ key: item.project_key, name: item.project_name, deployType: item.deploy_type });
    }
    return newQueryBranch('listed', { projects, total: wire.total as number });
  }
}

/** Query adapter backed by a validated Zadig client. */
export class ListProjectsOperation implements Query<ListProjectsInput, ListProjectsOutput> {
  /** Bind the provider client. */ constructor(private readonly client: ZadigClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return ListProjectsDefinition; }
  /** Invoke the provider Query. */ invoke(call: Call, input: ListProjectsInput): Promise<QueryAttempt<ListProjectsOutput>> { return this.client.invoke(call, input); }
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function isHeaderSafe(value: string): boolean { return !!value && value.trim() === value && /^[!-~]+$/.test(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'zadig', operation: 'listProjects', kind, message: 'Zadig query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ListProjectsOutput> { return newQueryBranch(name, { projects: [], total: 0 }, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<ListProjectsOutput> { return newQueryRetry(failure(kind), delay); }
