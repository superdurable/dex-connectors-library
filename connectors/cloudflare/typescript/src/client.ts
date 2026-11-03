// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { SecretString, type Call, type CredentialProvider, type Failure, type FailureKind } from '@superdurable/connectors-sdk';
import type { Config, Credentials } from './generated/models.js';
import { ListZonesOperation, PurgeCacheByURLsOperation } from './operations.js';

const defaultBaseURL = 'https://api.cloudflare.com/client/v4';
const defaultTimeoutMs = 15_000;
const defaultMaxResponseBytes = 2_097_152;

/** Injectable provider sender for deterministic transport tests. */
export type CloudflareRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;

/** Transport and clock dependencies for one client. */
export interface CloudflareClientOptions {
  /** Provider sender; production uses the SDK's bounded, redirect-safe transport. */
  send?: CloudflareRequestSender;
  /** Clock for HTTP Retry-After dates. */
  now?: () => Date;
}

/** Classified provider exchange, independent of operation output. */
export type CloudflareExchange = { response: ProviderResponse } | {
  disposition: 'retry' | 'notFound' | 'providerRejected' | 'invalidResponse' | 'defect';
  failure: Failure;
  retryAfterMs: number;
};

/** Authenticated Cloudflare client resolving credentials for each invocation. */
export class CloudflareClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly credentials: CredentialProvider<Credentials>;
  private readonly send: CloudflareRequestSender;
  private readonly now: () => Date;

  /** Validate immutable configuration without contacting the provider. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: CloudflareClientOptions = {}) {
    if (!config) throw new TypeError('Cloudflare configuration is required');
    if (!credentials) throw new TypeError('Cloudflare credential source is required');
    this.baseURL = validateBaseURL(config.baseUrl || defaultBaseURL);
    this.timeoutMs = config.timeoutMs || defaultTimeoutMs;
    this.maxResponseBytes = config.maxResponseBytes || defaultMaxResponseBytes;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) {
      throw new TypeError('Cloudflare timeout and maxResponseBytes must be positive');
    }
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the zone query. */
  listZones(): ListZonesOperation { return new ListZonesOperation(this); }
  /** Bind the repeat-safe URL purge mutation. */
  purgeCacheByURLs(): PurgeCacheByURLsOperation { return new PurgeCacheByURLsOperation(this); }

  /** Send one provider request and classify status and transport failures. */
  async exchange(call: Call, operation: string, method: string, path: string, body?: Uint8Array, hasNotFound = false): Promise<CloudflareExchange> {
    let token: string;
    try {
      const resolved = await this.credentials.resolve(call, call.context.cancellationSignal);
      if (!(resolved.apiToken instanceof SecretString)) return outcome('defect', operation, 'AUTHENTICATION', 'connection credentials are invalid');
      token = resolved.apiToken.reveal();
      if (!token || !isHeaderSafeCredential(token)) return outcome('defect', operation, 'AUTHENTICATION', 'connection credentials are invalid');
    } catch {
      return outcome('defect', operation, 'AUTHENTICATION', 'connection credentials are unavailable');
    }
    const request: ProviderRequest = {
      url: this.baseURL + path, method,
      headers: { Authorization: `Bearer ${token}`, Accept: 'application/json', ...(body ? { 'Content-Type': 'application/json' } : {}) },
      ...(body ? { body } : {}), maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: this.baseURL === defaultBaseURL ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError) {
        if (error.status !== undefined && error.headers) {
          const classified = this.classifyStatus(operation, error.status, error.headers, hasNotFound);
          if (classified) return classified;
        }
        return outcome('invalidResponse', operation, 'RESPONSE_TOO_LARGE', 'response exceeded the configured limit');
      }
      return outcome('retry', operation, 'AVAILABILITY', 'Cloudflare request failed');
    }
    const classified = this.classifyStatus(operation, response.status, response.headers, hasNotFound);
    if (classified) return classified;
    try {
      const document: unknown = JSON.parse(Buffer.from(response.body).toString('utf8'));
      if (document !== null && typeof document === 'object' && !Array.isArray(document) && 'success' in document && document.success === false) {
        return outcome('providerRejected', operation, 'PROVIDER_REJECTION', 'Cloudflare rejected the request');
      }
    } catch { /* Operation decoder classifies malformed JSON. */ }
    return { response };
  }

  private classifyStatus(operation: string, status: number, headers: ProviderResponse['headers'], hasNotFound: boolean): CloudflareExchange | undefined {
    if (status === 408 || status === 409 || status === 429 || status >= 500) {
      const retryAfter = headers['retry-after'];
      return outcome('retry', operation, status === 429 ? 'RATE_LIMIT' : 'AVAILABILITY', `Cloudflare returned HTTP ${status}`,
        parseRetryAfter(Array.isArray(retryAfter) ? retryAfter[0] ?? '' : retryAfter ?? '', this.now()));
    }
    if (status !== 200) {
      const kind: FailureKind = status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : status === 404 ? 'NOT_FOUND' : status === 400 || status === 422 ? 'VALIDATION' : 'PROVIDER_REJECTION';
      return outcome(status === 404 && hasNotFound ? 'notFound' : 'providerRejected', operation, kind, `Cloudflare returned HTTP ${status}`);
    }
    return undefined;
  }
}

function outcome(disposition: Exclude<CloudflareExchange, { response: ProviderResponse }>['disposition'], operation: string, kind: FailureKind, message: string, retryAfterMs = 0): CloudflareExchange {
  return { disposition, failure: { kind, provider: 'cloudflare', operation, message }, retryAfterMs };
}
