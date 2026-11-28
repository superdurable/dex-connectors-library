// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetRepositoryDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetRepositoryInput, GetRepositoryOutput } from './generated/models.js';

/** Optional provider sender for recorded scenarios. */
export interface GiteeClientOptions {
  /** Sender; omitted uses the SDK HTTP transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

/** Read bounded repository metadata from Gitee. */
export class GiteeClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate connection settings before contacting Gitee. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: GiteeClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Gitee configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || 'https://gitee.com');
    this.timeoutMs = config.timeoutMs || 10_000;
    this.maxResponseBytes = config.maxResponseBytes || 262_144;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Gitee request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the repository lookup Query. */ getRepository(): GetRepositoryOperation { return new GetRepositoryOperation(this); }

  /** Send one request with a freshly resolved private token. */
  async lookup(call: Call, input: GetRepositoryInput): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let token = '';
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      token = credentials.accessToken instanceof SecretString ? credentials.accessToken.reveal() : '';
    } catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!token || token.length > 512 || /[^\x21-\x7e]/.test(token)) return { error: 'credential', retryAfterMs: 0 };
    const endpoint = new URL(`${this.baseURL}/api/v5/repos/${encodeURIComponent(input.owner)}/${encodeURIComponent(input.repo)}`);
    endpoint.searchParams.set('access_token', token);
    const request: ProviderRequest = { url: endpoint.toString(), method: 'GET', headers: { Accept: 'application/json' }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** Read one repository's public metadata. */
export class GetRepositoryOperation implements Query<GetRepositoryInput, GetRepositoryOutput> {
  /** Bind a validated provider client. */ constructor(private readonly client: GiteeClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return GetRepositoryDefinition; }
  /** Classify provider, transport, and response outcomes. */
  async invoke(call: Call, input: GetRepositoryInput): Promise<QueryAttempt<GetRepositoryOutput>> {
    if (!validSegment(input?.owner) || !validSegment(input?.repo)) return branch('defect', 'VALIDATION');
    const exchange = await this.client.lookup(call, input);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (status === 404) return branch('notFound', 'PROVIDER_REJECTION');
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    try {
      const record: unknown = JSON.parse(
        new TextDecoder('utf-8', { fatal: true }).decode(body), readIntegerToken,
      );
      if (!isObject(record)) return branch('invalidResponse', 'PROTOCOL');
      const fields = Object.fromEntries(Object.entries(record).map(([key, value]) => [key.toLowerCase(), value]));
      const id = repositoryInteger(fields.id);
      const fullName = repositoryText(fields.full_name);
      const defaultBranch = repositoryText(fields.default_branch);
      const name = repositoryText(fields.name);
      const isPrivate: unknown = fields.private ?? false;
      if (!id || !fullName || !name || typeof isPrivate !== 'boolean') return branch('invalidResponse', 'PROTOCOL');
      return newQueryBranch('found', { repository: {
        id, fullName, name, defaultBranch, private: isPrivate,
        description: repositoryText(fields.description), htmlUrl: repositoryText(fields.html_url),
        starsCount: repositoryInteger(fields.stargazers_count), forksCount: repositoryInteger(fields.forks_count),
      } });
    } catch {
      return branch('invalidResponse', 'PROTOCOL');
    }
  }
}

function validSegment(value: unknown): value is string { return typeof value === 'string' && value.length > 0 && value.length <= 128 && value !== '.' && value !== '..' && /^[a-zA-Z0-9_.-]+$/.test(value); }
// Go integer fields reject decimal and exponent tokens even when their mathematical value is integral.
function readIntegerToken(_key: string, value: unknown, context?: { source?: string }): unknown {
  if (typeof value === 'number' && context?.source && !/^-?\d+$/.test(context.source)) return NaN;
  return value;
}

function repositoryText(value: unknown): string {
  if (value == null) return '';
  if (typeof value !== 'string') throw new TypeError('invalid repository text');
  return value;
}

function repositoryInteger(value: unknown): number {
  if (value == null) return 0;
  if (typeof value !== 'number' || !Number.isSafeInteger(value) || value < 0) {
    throw new TypeError('invalid repository integer');
  }
  return value;
}

function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'gitee', operation: 'getRepository', kind, message: 'Gitee repository lookup failed' }; }
function emptyOutput(): GetRepositoryOutput { return { repository: { id: 0, fullName: '', name: '', description: '', htmlUrl: '', private: false, defaultBranch: '', starsCount: 0, forksCount: 0 } }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<GetRepositoryOutput> { return newQueryBranch(name, emptyOutput(), {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<GetRepositoryOutput> { return newQueryRetry(failure(kind), delay); }
