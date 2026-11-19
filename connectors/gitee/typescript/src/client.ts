// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { isIP } from 'node:net';
import { newQueryBranch, newQueryRetry, SecretString, zeroReceipt, type Call, type Failure, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetRepositoryDefinition } from './generated/definitions.js';
import type { Config, CredentialSource, GetRepositoryInput, GetRepositoryOutput, RepositoryMetadata } from './generated/models.js';

/** Provider transport override for tests and approved gateways. */
export interface GiteeClientOptions { /** SDK transport when omitted. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>; }

/** Gitee REST client for bounded repository metadata queries. */
export class GiteeClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate the API origin and request limits. */
  constructor(config: Config, private readonly credentials: CredentialSource, options: GiteeClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Gitee configuration and credential source are required');
    const url = new URL(config.baseUrl ?? 'https://gitee.com');
    const hostname = url.hostname.toLowerCase();
    const isLoopback = hostname === 'localhost' || (isIP(hostname) === 4 && hostname.startsWith('127.')) || hostname === '[::1]';
    if ((url.protocol !== 'https:' && !(url.protocol === 'http:' && isLoopback)) || url.username || url.password || url.search || url.hash || url.pathname !== '/')
      throw new TypeError('Gitee API URL must be an HTTPS origin or loopback test origin');
    this.baseURL = url.origin;
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 262_144;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Gitee request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the getRepository Query. */ getRepository(): GetRepositoryOperation { return new GetRepositoryOperation(this); }

  /** Resolve the token and send one repository request. */
  async requestRepository(call: Call, input: GetRepositoryInput): Promise<{ response?: ProviderResponse; tooLarge?: boolean; credentialFailed?: boolean; transportFailed?: boolean }> {
    let token: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      token = credentials.accessToken instanceof SecretString ? credentials.accessToken.reveal() : '';
    } catch { return { credentialFailed: true }; }
    if (!validToken(token)) return { credentialFailed: true };
    const url = new URL(this.baseURL + '/api/v5/repos/' + encodeURIComponent(input.owner) + '/' + encodeURIComponent(input.repo));
    url.searchParams.set('access_token', token);
    const request: ProviderRequest = {
      url: url.toString(), method: 'GET', headers: { Accept: 'application/json' },
      timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes,
      addressPolicy: this.baseURL.startsWith('https:') ? 'publicHTTPS' : 'privateNetworkHTTP', signal: call.context.cancellationSignal,
    };
    try { return { response: await this.send(request) }; }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined)
        return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, tooLarge: true };
      return { transportFailed: true };
    }
  }
}

/** Read one repository selected by owner and repository path. */
export class GetRepositoryOperation implements Query<GetRepositoryInput, GetRepositoryOutput> {
  /** Bind the client. */ constructor(private readonly client: GiteeClient) {}
  /** Return the generated Query definition. */ definition() { return GetRepositoryDefinition; }
  /** Classify one repository response. */
  async invoke(call: Call, input: GetRepositoryInput): Promise<QueryAttempt<GetRepositoryOutput>> {
    if (!validPathSegment(input.owner) || !validPathSegment(input.repo)) return terminal('defect', 'VALIDATION', 'owner or repo path is invalid');
    const result = await this.client.requestRepository(call, input);
    if (result.credentialFailed) return terminal('defect', 'AUTHENTICATION', 'connection token is unavailable or invalid');
    if (result.transportFailed || !result.response) return newQueryRetry(failure('AVAILABILITY', 'Gitee request failed'));
    const response = result.response;
    const retryAfter = retryDelay(response);
    if (response.status === 429) return newQueryRetry(failure('RATE_LIMIT', 'Gitee rate limited the request'), retryAfter);
    if (response.status === 408 || response.status >= 500) return newQueryRetry(failure('AVAILABILITY', 'Gitee is temporarily unavailable'), retryAfter);
    if (result.tooLarge) return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'Gitee response exceeded the configured limit');
    if (response.status === 404) return terminal('notFound', 'PROVIDER_REJECTION', 'repository was not found or is not visible');
    if (response.status !== 200) {
      const kind: Failure['kind'] = response.status === 401 ? 'AUTHENTICATION' : response.status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION';
      return terminal('providerRejected', kind, 'Gitee rejected the request with HTTP ' + response.status);
    }
    let value: unknown;
    try { value = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)); }
    catch { return terminal('invalidResponse', 'PROTOCOL', 'Gitee returned invalid repository metadata'); }
    if (!value || typeof value !== 'object' || Array.isArray(value)) return terminal('invalidResponse', 'PROTOCOL', 'Gitee returned invalid repository metadata');
    const wire = value as Record<string, unknown>;
    if (!Number.isSafeInteger(wire.id) || (wire.id as number) <= 0 || typeof wire.full_name !== 'string' || !wire.full_name || typeof wire.name !== 'string' || !wire.name ||
        !Number.isSafeInteger(wire.stargazers_count) || (wire.stargazers_count as number) < 0 || !Number.isSafeInteger(wire.forks_count) || (wire.forks_count as number) < 0)
      return terminal('invalidResponse', 'PROTOCOL', 'Gitee returned invalid repository metadata');
    const repository: RepositoryMetadata = {
      id: wire.id as number, fullName: wire.full_name, name: wire.name,
      description: typeof wire.description === 'string' ? wire.description : '',
      htmlUrl: typeof wire.html_url === 'string' ? wire.html_url : '',
      private: wire.private === true,
      defaultBranch: typeof wire.default_branch === 'string' ? wire.default_branch : '',
      starsCount: wire.stargazers_count as number, forksCount: wire.forks_count as number,
    };
    return newQueryBranch('found', { repository }, zeroReceipt());
  }
}

const emptyRepository = (): RepositoryMetadata => ({ id: 0, fullName: '', name: '', description: '', htmlUrl: '', private: false, defaultBranch: '', starsCount: 0, forksCount: 0 });
function validPathSegment(value: unknown): value is string { return typeof value === 'string' && value.length > 0 && value.length <= 128 && value !== '.' && value !== '..' && /^[A-Za-z0-9._-]+$/.test(value); }
function validToken(value: string): boolean { return value.length > 0 && value.length <= 512 && /^[!-~]+$/.test(value); }
function failure(kind: Failure['kind'], message: string): Failure { return { kind, provider: 'gitee', operation: 'getRepository', message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<GetRepositoryOutput> { return newQueryBranch(branch, { repository: emptyRepository() }, zeroReceipt(), failure(kind, message)); }
function retryDelay(response: ProviderResponse): number {
  const value = Object.entries(response.headers).find(([name]) => name.toLowerCase() === 'retry-after')?.[1];
  return parseRetryAfter(Array.isArray(value) ? value[0] ?? '' : value ?? '') || 0;
}
