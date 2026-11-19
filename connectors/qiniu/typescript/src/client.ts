// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHmac } from 'node:crypto';
import { isIP } from 'node:net';
import { newQueryBranch, newQueryRetry, SecretString, zeroReceipt, type Call, type Failure, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListBucketsDefinition } from './generated/definitions.js';
import type { Config, CredentialSource, ListBucketsInput, ListBucketsOutput } from './generated/models.js';

/** Dependencies for deterministic signing and approved provider transports. */
export interface QiniuClientOptions {
  /** SDK transport when omitted. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Signing clock; system time when omitted. */ now?: () => Date;
}

/** Qiniu Kodo management client for bounded bucket listing. */
export class QiniuClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;

  /** Validate the Kodo UC origin and request limits. */
  constructor(config: Config, private readonly credentials: CredentialSource, options: QiniuClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Qiniu configuration and credential source are required');
    const url = new URL(config.baseUrl ?? 'https://uc.qiniuapi.com');
    const hostname = url.hostname.toLowerCase();
    const isLoopback = hostname === 'localhost' || (isIP(hostname) === 4 && hostname.startsWith('127.')) || hostname === '[::1]';
    if ((url.protocol !== 'https:' && !(url.protocol === 'http:' && isLoopback)) || url.username || url.password || url.search || url.hash || url.pathname !== '/')
      throw new TypeError('Qiniu UC URL must be an HTTPS origin or loopback test origin');
    this.baseURL = url.origin;
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 262_144;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Qiniu request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the listBuckets Query. */ listBuckets(): ListBucketsOperation { return new ListBucketsOperation(this); }

  /** Sign and send one bucket-list request. */
  async requestBuckets(call: Call): Promise<{ response?: ProviderResponse; tooLarge?: boolean; credentialFailed?: boolean; transportFailed?: boolean }> {
    let accessKey: string; let secretKey: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      accessKey = credentials.accessKey instanceof SecretString ? credentials.accessKey.reveal() : '';
      secretKey = credentials.secretKey instanceof SecretString ? credentials.secretKey.reveal() : '';
    } catch { return { credentialFailed: true }; }
    if (!validCredential(accessKey) || !validCredential(secretKey)) return { credentialFailed: true };
    const url = new URL(this.baseURL + '/buckets');
    const date = this.now().toISOString().replace(/[-:]/g, '').replace(/\.\d{3}Z$/, 'Z');
    const canonical = 'GET /buckets\nHost: ' + url.host + '\nX-Qiniu-Date: ' + date + '\n\n';
    const signature = createHmac('sha1', secretKey).update(canonical).digest('base64').replace(/\+/g, '-').replace(/\//g, '_');
    const request: ProviderRequest = {
      url: url.toString(), method: 'GET',
      headers: { Accept: 'application/json', 'X-Qiniu-Date': date, Authorization: 'Qiniu ' + accessKey + ':' + signature },
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

/** List names of buckets owned by the signing Qiniu account. */
export class ListBucketsOperation implements Query<ListBucketsInput, ListBucketsOutput> {
  /** Bind the client. */ constructor(private readonly client: QiniuClient) {}
  /** Return the generated Query definition. */ definition() { return ListBucketsDefinition; }
  /** Classify one bucket-list response. */
  async invoke(call: Call, input: ListBucketsInput): Promise<QueryAttempt<ListBucketsOutput>> {
    void input;
    const result = await this.client.requestBuckets(call);
    if (result.credentialFailed) return terminal('defect', 'AUTHENTICATION', 'connection credentials are unavailable or invalid');
    if (result.transportFailed || !result.response) return newQueryRetry(failure('AVAILABILITY', 'Qiniu request failed'));
    const response = result.response;
    const retryAfter = retryDelay(response);
    if (response.status === 429) return newQueryRetry(failure('RATE_LIMIT', 'Qiniu rate limited the request'), retryAfter);
    if (response.status === 408 || response.status >= 500) return newQueryRetry(failure('AVAILABILITY', 'Qiniu is temporarily unavailable'), retryAfter);
    if (result.tooLarge) return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'Qiniu response exceeded the configured limit');
    if (response.status !== 200) {
      const kind: Failure['kind'] = response.status === 401 ? 'AUTHENTICATION' : response.status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION';
      return terminal('providerRejected', kind, 'Qiniu rejected the request with HTTP ' + response.status);
    }
    let value: unknown;
    try { value = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)); }
    catch { return terminal('invalidResponse', 'PROTOCOL', 'Qiniu returned an invalid bucket list'); }
    if (!Array.isArray(value) || !value.every(name => typeof name === 'string' && name.length > 0 && name.length <= 255))
      return terminal('invalidResponse', 'PROTOCOL', 'Qiniu returned an invalid bucket list');
    return newQueryBranch('listed', { buckets: value as string[] }, zeroReceipt());
  }
}

function validCredential(value: string): boolean { return value.length > 0 && value.length <= 512 && /^[!-~]+$/.test(value); }
function failure(kind: Failure['kind'], message: string): Failure { return { kind, provider: 'qiniu', operation: 'listBuckets', message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<ListBucketsOutput> { return newQueryBranch(branch, { buckets: [] }, zeroReceipt(), failure(kind, message)); }
function retryDelay(response: ProviderResponse): number {
  const value = Object.entries(response.headers).find(([name]) => name.toLowerCase() === 'retry-after')?.[1];
  return parseRetryAfter(Array.isArray(value) ? value[0] ?? '' : value ?? '') || 0;
}
