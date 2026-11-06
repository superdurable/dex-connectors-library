// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { isIP } from 'node:net';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { resolveCredential, resolveCredentialAfterRejection, type Call, type Failure, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { Config, Credentials, CredentialSource } from './generated/models.js';
import { GetAuthenticatedProfileOperation, ListPublicRepositoriesOperation, ListMergedPullRequestsOperation, ListPullRequestFilesOperation, ListReleasesOperation, ListCommitsOperation } from './operations.js';

const defaultBaseUrl = 'https://api.github.com';
const defaultApiVersion = '2026-03-10';
const userAgent = 'superdurable-dex-github-connector/0.1';

/** Injectable sender and clock for deterministic provider verification. */
export interface GitHubClientOptions {
  send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  now?: () => Date;
}

/** A successful exchange or classified provider failure. */
export type GitHubExchange =
  | { response: ProviderResponse; receipt: ReceiptDraft }
  | { branch?: string; failure: Failure; retryAfterMs?: number; receipt: ReceiptDraft };

/** GitHub REST client bound to host managed OAuth credentials. */
export class GitHubClient {
  readonly baseUrl: string;
  readonly apiVersion: string;
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly defaultRepositoryLimit: number;
  readonly maxRepositories: number;
  readonly maxPullRequestBodyCharacters: number;
  readonly maxPatchCharacters: number;
  readonly maxCommitMessageCharacters: number;
  readonly now: () => Date;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly accessTokens = new WeakMap<Call, string>();
  private readonly refreshDriver = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'GitHub', auth: Authorization, credentialFields: CredentialFields });

  /** Validate connection settings without contacting GitHub. */
  constructor(config: Config, readonly credentials: CredentialSource, options: GitHubClientOptions = {}) {
    if (!credentials) throw new TypeError('GitHub credential provider is required');
    const baseUrl = config.baseUrl || defaultBaseUrl;
    let parsed: URL;
    try { parsed = new URL(baseUrl); } catch { throw new TypeError('GitHub base URL must be absolute'); }
    const host = parsed.hostname.replace(/^\[|\]$/g, '').toLowerCase();
    const isLoopback = host === 'localhost' || isIP(host) === 4 && host.startsWith('127.') || isIP(host) === 6 && (host === '::1' || host.startsWith('::ffff:127.'));
    if (!parsed.hostname || (parsed.protocol !== 'https:' && !(parsed.protocol === 'http:' && isLoopback)) || parsed.username || parsed.password || parsed.search || parsed.hash) throw new TypeError('GitHub base URL is invalid');
    this.baseUrl = baseUrl.replace(/\/+$/, '');
    this.apiVersion = config.apiVersion || defaultApiVersion;
    if (!/^\d{4}-\d{2}-\d{2}$/.test(this.apiVersion)) throw new TypeError('GitHub API version must use YYYY-MM-DD');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 2_097_152;
    this.defaultRepositoryLimit = config.defaultRepositoryLimit || 100;
    this.maxRepositories = config.maxRepositories || 500;
    this.maxPullRequestBodyCharacters = config.maxPullRequestBodyCharacters || 4000;
    this.maxPatchCharacters = config.maxPatchCharacters || 4000;
    this.maxCommitMessageCharacters = config.maxCommitMessageCharacters || 4000;
    for (const [name, value] of Object.entries({ timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, defaultRepositoryLimit: this.defaultRepositoryLimit, maxRepositories: this.maxRepositories, maxPullRequestBodyCharacters: this.maxPullRequestBodyCharacters, maxPatchCharacters: this.maxPatchCharacters, maxCommitMessageCharacters: this.maxCommitMessageCharacters })) {
      if (!Number.isSafeInteger(value) || value <= 0) throw new TypeError(`GitHub ${name} must be positive`);
    }
    if (this.maxRepositories > 500 || this.defaultRepositoryLimit > this.maxRepositories) throw new TypeError('GitHub repository limits are invalid');
    this.now = options.now ?? (() => new Date());
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the authenticated profile query. */
  getAuthenticatedProfile(): GetAuthenticatedProfileOperation { return new GetAuthenticatedProfileOperation(this); }
  /** Bind the public repository query. */
  listPublicRepositories(): ListPublicRepositoriesOperation { return new ListPublicRepositoriesOperation(this); }
  /** Bind the merged pull request query. */
  listMergedPullRequests(): ListMergedPullRequestsOperation { return new ListMergedPullRequestsOperation(this); }
  /** Bind the changed file query. */
  listPullRequestFiles(): ListPullRequestFilesOperation { return new ListPullRequestFilesOperation(this); }
  /** Bind the release query. */
  listReleases(): ListReleasesOperation { return new ListReleasesOperation(this); }
  /** Bind the commit query. */
  listCommits(): ListCommitsOperation { return new ListCommitsOperation(this); }

  /** Resolve credentials, send a single GitHub GET, and classify the result. */
  async get(call: Call, operation: string, path: string, query: Record<string, string> = {}): Promise<GitHubExchange> {
    let token = this.accessTokens.get(call);
    if (!token) {
      try {
        const credential = await resolveCredential(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
        token = credential.accessToken.reveal();
        if (!token) throw new TypeError('missing access token');
        this.accessTokens.set(call, token);
      } catch { return { branch: 'defect', failure: failure(operation, 'AUTHENTICATION', 'GitHub authorization is unavailable or revoked'), receipt: {} }; }
    }
    const search = encodeGoQuery(query);
    const url = `${this.baseUrl}${path}${search ? `?${search}` : ''}`;
    for (let attempt = 0; attempt < 2; attempt++) {
      let response: ProviderResponse;
      try {
        response = await this.send({
          url, method: 'GET', headers: {
            Accept: 'application/vnd.github+json', Authorization: `Bearer ${token}`,
            'User-Agent': userAgent, 'X-GitHub-Api-Version': this.apiVersion,
          }, addressPolicy: 'unrestricted',
          maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
          signal: call.context.cancellationSignal,
        });
      } catch (error) {
        if (error instanceof BodyTooLargeError) return { branch: 'invalidResponse', failure: failure(operation, 'RESPONSE_TOO_LARGE', `${operation === 'getAuthenticatedProfile' ? path === '/user/emails' ? 'GitHub email' : 'GitHub profile' : operation === 'listPublicRepositories' ? 'GitHub repository' : 'GitHub'} response exceeds the configured size limit`), receipt: requestReceipt(error.headers ?? {}) };
        return { failure: failure(operation, 'AVAILABILITY', 'GitHub is unavailable'), retryAfterMs: 0, receipt: {} };
      }
      const receipt = requestReceipt(response.headers);
      if (response.status === 401 && attempt === 0 && 'resolveAfterRejection' in this.credentials) {
        try {
          const replacement = await resolveCredentialAfterRejection(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
          token = replacement.accessToken.reveal();
          if (token) { this.accessTokens.set(call, token); continue; }
        } catch { /* A failed refresh preserves GitHub's original 401 classification. */ }
      }
      if (response.status >= 200 && response.status < 300 || operation === 'listCommits' && response.status === 409) return { response, receipt };
      const header = (name: string) => responseHeader(response.headers, name);
      let rateLimited = response.status === 429 || response.status === 403 && (header('retry-after') !== '' || header('x-ratelimit-remaining') === '0');
      if (!rateLimited && response.status === 403) {
        try {
          const body: unknown = JSON.parse(Buffer.from(response.body).toString());
          rateLimited = typeof body === 'object' && body !== null && 'message' in body && typeof body.message === 'string' && body.message.toLowerCase().includes('rate limit');
        } catch { /* Non-JSON errors cannot identify a secondary rate limit. */ }
      }
      if (rateLimited) {
        let delay = parseRetryAfter(header('retry-after'), this.now());
        if (!Number.isSafeInteger(delay) || delay <= 0) {
          const reset = header('x-ratelimit-reset');
          delay = header('x-ratelimit-remaining') === '0' && /^[+-]?\d+$/.test(reset) && Number.isSafeInteger(Number(reset)) ? Math.max(Number(reset) * 1000 - this.now().getTime(), 0) : 0;
          if (!Number.isFinite(delay) || delay <= 0) delay = 60_000;
        }
        return { failure: failure(operation, 'RATE_LIMIT', 'GitHub rate limit was reached'), retryAfterMs: delay, receipt };
      }
      if (response.status >= 500) return { failure: failure(operation, 'AVAILABILITY', 'GitHub is unavailable'), retryAfterMs: 0, receipt };
      if (response.status === 401) return { branch: 'authorizationRevoked', failure: failure(operation, 'AUTHENTICATION', 'GitHub authorization is invalid or revoked'), receipt };
      if (response.status === 403) return { branch: 'insufficientScope', failure: failure(operation, 'AUTHORIZATION', 'GitHub authorization is forbidden or lacks scope'), receipt };
      if (response.status === 404) return { branch: 'notFound', failure: failure(operation, 'NOT_FOUND', 'GitHub resource was not found'), receipt };
      return { branch: 'providerRejected', failure: failure(operation, 'PROVIDER_REJECTION', 'GitHub rejected the request'), receipt };
    }
    throw new Error('GitHub authenticated request retry was exhausted');
  }
}

/** Produce a provider-safe failure. */
export function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { provider: 'github', operation, kind, message }; }

/** Read one case-insensitive response header. */
export function responseHeader(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase());
  const value = entry?.[1];
  return Array.isArray(value) ? value[0] ?? '' : value ?? '';
}

/** Keep only GitHub's safe request correlation value. */
export function requestReceipt(headers: ProviderResponse['headers']): ReceiptDraft {
  for (const name of ['x-github-request-id', 'x-request-id']) {
    const requestId = responseHeader(headers, name).trim();
    if (/^[A-Za-z0-9_.:/-]{1,128}$/.test(requestId)) return { providerRequestId: requestId };
  }
  return {};
}

/** Verify the exact two scopes required by the Go connector. */
export function hasRequiredScopes(headers: ProviderResponse['headers']): boolean {
  const scopes = new Set(responseHeader(headers, 'x-oauth-scopes').split(',').map(value => value.trim()).filter(Boolean));
  return scopes.size === 2 && scopes.has('read:user') && scopes.has('user:email');
}

/** Encode a Go url.Values component. */
export function queryEscape(value: string): string { return encodeURIComponent(value).replace(/[!'()*]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`).replace(/%20/g, '+'); }

/** Encode one Go url.PathEscape path segment. */
export function pathEscape(value: string): string { return encodeURIComponent(value).replace(/%24/g, '$').replace(/%26/g, '&').replace(/%2B/g, '+').replace(/%3D/g, '=').replace(/%3A/g, ':').replace(/%40/g, '@').replace(/[!'()*]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`); }

/** Encode keys and ordered values like Go url.Values.Encode. */
export function encodeGoQuery(values: Record<string, string | string[]>): string {
  return Object.keys(values).sort((left, right) => Buffer.compare(Buffer.from(left), Buffer.from(right)))
    .flatMap(key => (Array.isArray(values[key]) ? values[key] : [values[key]!])!.map(value => `${queryEscape(key)}=${queryEscape(value)}`)).join('&');
}
