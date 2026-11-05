// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type Call, type CredentialProvider, type Failure, type QueryAttempt, newQueryBranch, newQueryRetry } from '@superdurable/connectors-sdk';
import { AddressRefusal, BodyTooLargeError, MaxErrorBodyBytes, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import type { Config, Credentials, DeploysPage, SitesPage } from './generated/models.js';
import { ListDeploysOperation, ListSitesOperation } from './operations.js';

const defaultBaseUrl = 'https://api.netlify.com/api/v1';
const emptySites: SitesPage = { sites: [] };
const emptyDeploys: DeploysPage = { deploys: [] };

/** HTTP sender for deterministic provider tests. */
export type NetlifyRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Optional transport and clock used by provider tests. */
export interface NetlifyClientOptions {
  /** Sender; defaults to the SDK provider transport. */
  send?: NetlifyRequestSender;
  /** Clock for Retry-After dates. */
  now?: () => Date;
}
/** Immutable Netlify client resolving credentials for each request. */
export class NetlifyClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: NetlifyRequestSender;
  private readonly now: () => Date;
  /** Validate configuration without contacting Netlify. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: NetlifyClientOptions = {}) {
    if (!credentials) throw new TypeError('Netlify credential provider is required');
    this.baseUrl = validateBaseURL(config.baseUrl || defaultBaseUrl);
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs < 0) throw new TypeError('Netlify timeout is invalid');
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes < 0) throw new TypeError('Netlify response limit is invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }
  /** Bind the site list query. */
  listSites(): ListSitesOperation { return new ListSitesOperation(this); }
  /** Bind the deploy list query. */
  listDeploys(): ListDeploysOperation { return new ListDeploysOperation(this); }
  /** Send one GET and classify its response like the Go connector. */
  async query<Output>(call: Call, operation: string, path: string, empty: Output, hasNotFound: boolean, decode: (body: Uint8Array) => Output): Promise<QueryAttempt<Output>> {
    let token: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      if (!(credentials.accessToken instanceof SecretString) || !isHeaderSafeCredential(credentials.accessToken.reveal())) throw new TypeError('invalid token');
      token = credentials.accessToken.reveal();
    } catch {
      return this.branch(operation, 'defect', empty, 'AUTHENTICATION', 'connection credentials are unavailable');
    }
    let response: ProviderResponse;
    try {
      response = await this.send({ url: this.baseUrl + path, method: 'GET', headers: { Authorization: `Bearer ${token}`, Accept: 'application/json' }, maxResponseBytes: Math.max(this.maxResponseBytes, MaxErrorBodyBytes), timeoutMs: this.timeoutMs, addressPolicy: 'privateNetworkHTTP', signal: call.context.cancellationSignal });
    } catch (error) {
      if (error instanceof BodyTooLargeError) {
        if (error.status !== undefined && error.status !== 200) return this.statusOutcome(operation, error.status, error.headers?.['retry-after'], empty, hasNotFound);
        return this.branch(operation, 'invalidResponse', empty, 'RESPONSE_TOO_LARGE', 'response exceeded the configured limit');
      }
      if (error instanceof AddressRefusal) return this.branch(operation, 'defect', empty, 'LOCAL_DEFECT', 'request could not be built');
      return newQueryRetry(this.failure(operation, 'AVAILABILITY', 'Netlify request failed'));
    }
    if (response.status !== 200) return this.statusOutcome(operation, response.status, response.headers['retry-after'], empty, hasNotFound);
    if (response.body.length > this.maxResponseBytes) return this.branch(operation, 'invalidResponse', empty, 'RESPONSE_TOO_LARGE', 'response exceeded the configured limit');
    try { return newQueryBranch('listed', decode(response.body), {}); }
    catch { return this.branch(operation, 'invalidResponse', empty, 'PROTOCOL', 'response was malformed'); }
  }
  private statusOutcome<Output>(operation: string, status: number, retryAfter: string | string[] | undefined, empty: Output, hasNotFound: boolean): QueryAttempt<Output> {
    if (status === 408 || status === 409 || status === 429 || status >= 500) {
      const kind = status === 429 ? 'RATE_LIMIT' : 'AVAILABILITY';
      return newQueryRetry(this.failure(operation, kind, `Netlify returned HTTP ${status}`), parseRetryAfter(Array.isArray(retryAfter) ? retryAfter[0] ?? '' : retryAfter ?? '', this.now()));
    }
    const branch = status === 404 && hasNotFound ? 'notFound' : 'providerRejected';
    const kind = status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : status === 404 ? 'NOT_FOUND' : status === 400 || status === 422 ? 'VALIDATION' : 'PROVIDER_REJECTION';
    return this.branch(operation, branch, empty, kind, `Netlify returned HTTP ${status}`);
  }
  private failure(operation: string, kind: Failure['kind'], message: string): Failure { return { kind, provider: 'netlify', operation, message }; }
  private branch<Output>(operation: string, branch: string, empty: Output, kind: Failure['kind'], message: string): QueryAttempt<Output> {
    return newQueryBranch(branch, empty, {}, this.failure(operation, kind, message));
  }
}
export { emptySites, emptyDeploys };
