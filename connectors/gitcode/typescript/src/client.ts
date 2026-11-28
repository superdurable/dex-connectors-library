// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetRepositoryDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetRepositoryInput, GetRepositoryOutput } from './generated/models.js';

const repositoriesPath = '/api/v5/repos/';

/** Optional provider sender for local tests. */
export interface GitcodeClientOptions {
  /** Sender; omitted uses the SDK HTTP transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

/** Read repository identity and visibility from GitCode. */
export class GitcodeClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate local settings without contacting GitCode. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: GitcodeClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('GitCode configuration and credentials are required');
    this.timeoutMs = config.timeoutMs || 10_000;
    this.maxResponseBytes = config.maxResponseBytes || 262_144;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('GitCode request limits are invalid');
    let parsedUrl: URL;
    try { parsedUrl = new URL(config.baseUrl || 'https://api.gitcode.com'); } catch { throw new TypeError('GitCode baseUrl must be an HTTPS origin'); }
    if (parsedUrl.protocol !== 'https:' || !parsedUrl.hostname || parsedUrl.username || parsedUrl.password || parsedUrl.search || parsedUrl.hash || (parsedUrl.pathname !== '/' && parsedUrl.pathname !== '') || parsedUrl.hostname === 'localhost') throw new TypeError('GitCode baseUrl must be an HTTPS origin');
    this.baseUrl = parsedUrl.origin;
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the repository Query. */ getRepository(): GetRepositoryOperation { return new GetRepositoryOperation(this); }

  /** Send one bounded repository request. */
  async get(call: Call, owner: string, repo: string): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let accessToken = '';
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      accessToken = credentials.accessToken instanceof SecretString ? credentials.accessToken.reveal() : '';
    } catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!accessToken || accessToken.length > 512 || /[^\x21-\x7e]/.test(accessToken)) return { error: 'credential', retryAfterMs: 0 };
    const request: ProviderRequest = { url: this.baseUrl + repositoriesPath + owner + '/' + repo, method: 'GET', headers: { Accept: 'application/json', Authorization: 'Bearer ' + accessToken }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** Read one GitCode repository. */
export class GetRepositoryOperation implements Query<GetRepositoryInput, GetRepositoryOutput> {
  /** Bind the provider client. */ constructor(private readonly client: GitcodeClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return GetRepositoryDefinition; }
  /** Classify one repository lookup using the Go connector contract. */
  async invoke(call: Call, input: GetRepositoryInput): Promise<QueryAttempt<GetRepositoryOutput>> {
    if (!validSegment(input?.owner) || !validSegment(input?.repo)) return branch('defect', 'VALIDATION');
    const exchange = await this.client.get(call, input.owner, input.repo);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status === 404) return branch('notFound', 'PROVIDER_REJECTION');
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 400 ? 'VALIDATION' : 'PROVIDER_REJECTION');
    try {
      const record: unknown = JSON.parse(
        new TextDecoder('utf-8', { fatal: true }).decode(body), readIntegerToken,
      );
      if (!isObject(record)) return branch('invalidResponse', 'PROTOCOL');
      const fields = Object.fromEntries(Object.entries(record).map(([key, value]) => [key.toLowerCase(), value]));
      const id = repositoryInteger(fields.id);
      const fullName = repositoryText(fields.full_name);
      const defaultBranch = repositoryText(fields.default_branch);
      if (!id || !fullName || typeof fields.private !== 'boolean') return branch('invalidResponse', 'PROTOCOL');
      return newQueryBranch('fetched', { id, fullName, private: fields.private, defaultBranch });
    } catch {
      return branch('invalidResponse', 'PROTOCOL');
    }
  }
}

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
function validSegment(value: unknown): value is string { return typeof value === 'string' && !!value && value.length <= 128 && value !== '.' && value !== '..' && /^[A-Za-z0-9_.-]+$/.test(value); }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'gitcode', operation: 'getRepository', kind, message: 'GitCode repository lookup failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<GetRepositoryOutput> { return newQueryBranch(name, { id: 0, fullName: '', private: false, defaultBranch: '' }, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<GetRepositoryOutput> { return newQueryRetry(failure(kind), delay); }
