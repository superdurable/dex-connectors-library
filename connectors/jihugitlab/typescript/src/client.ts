// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListMemberProjectsDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListMemberProjectsInput, ListMemberProjectsOutput } from './generated/models.js';

const emptyOutput: ListMemberProjectsOutput = { projects: [], nextPage: 0 };
/** Optional provider sender for deterministic JiHu GitLab tests. */
export interface JiHuGitLabClientOptions { /** Sender; production uses Connector SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>; }
/** Resolve credentials and list the token owner's member projects. */
export class JiHuGitLabClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Validate JiHu GitLab configuration without contacting the provider. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: JiHuGitLabClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('JiHu GitLab configuration and credential source are required');
    this.baseUrl = config.baseUrl ?? 'https://jihulab.com';
    const origin = new URL(this.baseUrl);
    if (origin.protocol !== 'https:' || !origin.hostname || (origin.pathname !== '/' && origin.pathname !== '') || origin.search || origin.hash || origin.username || origin.password) throw new TypeError('JiHu GitLab base URL must be an HTTPS origin');
    this.baseUrl = origin.origin;
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('JiHu GitLab request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the read-only member project Query. */ listMemberProjects(): ListMemberProjectsOperation { return new ListMemberProjectsOperation(this); }
  /** Send one authenticated and bounded project request. */
  async request(call: Call, page: number, perPage: number): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let token = '';
    try { const credentials = await this.credentials.resolve(call, call.context.cancellationSignal); token = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : ''; }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!token || token.length > 512 || !/^[!-~]+$/.test(token)) return { error: 'credential', retryAfterMs: 0 };
    const url = new URL('/api/v4/projects', this.baseUrl);
    url.searchParams.set('membership', 'true'); url.searchParams.set('page', String(page)); url.searchParams.set('per_page', String(perPage));
    const request: ProviderRequest = { url: url.toString(), method: 'GET', headers: { Accept: 'application/json', 'PRIVATE-TOKEN': token }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    const retryAfterValue = header(response, 'retry-after');
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(retryAfterValue) };
  }
}
/** Query one page of projects without persisting the access token. */
export class ListMemberProjectsOperation implements Query<ListMemberProjectsInput, ListMemberProjectsOutput> {
  /** Bind a validated client. */ constructor(private readonly client: JiHuGitLabClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return ListMemberProjectsDefinition; }
  /** Classify provider and transport outcomes. */
  async invoke(call: Call, input: ListMemberProjectsInput): Promise<QueryAttempt<ListMemberProjectsOutput>> {
    const page = input.page || 1, perPage = input.perPage || 20;
    if (!Number.isSafeInteger(page) || page < 1 || page > 50_000 || !Number.isSafeInteger(perPage) || perPage < 1 || perPage > 100) return branch('defect', 'VALIDATION');
    const exchange = await this.client.request(call, page, perPage);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    let document: unknown;
    try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!Array.isArray(document) || document.length > perPage) return branch('invalidResponse', 'PROTOCOL');
    const projects: NonNullable<ListMemberProjectsOutput['projects']> = [];
    for (const item of document) {
      if (!isRecord(item) || !Number.isSafeInteger(item.id) || (item.id as number) <= 0 || typeof item.name !== 'string' || !item.name || item.name.length > 512 || typeof item.path_with_namespace !== 'string' || !item.path_with_namespace || item.path_with_namespace.length > 1024 || item.path_with_namespace.trim() !== item.path_with_namespace) return branch('invalidResponse', 'PROTOCOL');
      projects.push({ id: item.id as number, name: item.name, path: item.path_with_namespace });
    }
    const nextPageText = header(exchange.response, 'x-next-page');
    const nextPage = nextPageText ? Number(nextPageText) : 0;
    if (nextPageText && (!Number.isSafeInteger(nextPage) || nextPage <= page || nextPage > 50_000)) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('listed', { projects, nextPage });
  }
}
function header(response: ProviderResponse, name: string): string { const value = Object.entries(response.headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'jihugitlab', operation: 'listMemberProjects', kind, message: 'JiHu GitLab member projects request failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ListMemberProjectsOutput> { return newQueryBranch(name, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<ListMemberProjectsOutput> { return newQueryRetry(failure(kind), delay); }
