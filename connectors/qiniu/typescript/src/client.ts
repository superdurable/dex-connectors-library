// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHmac } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListBucketsDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListBucketsInput, ListBucketsOutput } from './generated/models.js';

/** Optional HTTP sender and clock for Qiniu provider tests. */
export interface QiniuClientOptions {
  /** Sender; omitted uses the SDK HTTP transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Clock; omitted uses the current UTC time. */ now?: () => Date;
}

/** Resolve current credentials and query Qiniu Kodo bucket names. */
export class QiniuClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;

  /** Validate configuration without contacting the provider. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: QiniuClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Qiniu configuration and credentials are required');
    this.baseUrl = config.baseUrl || 'https://uc.qiniuapi.com';
    const parsed = new URL(this.baseUrl);
    if (!(parsed.protocol === 'https:' || (options.send && parsed.protocol === 'http:' && parsed.hostname === '127.0.0.1')) || parsed.username || parsed.password || parsed.search || parsed.hash || parsed.pathname !== '/') throw new TypeError('Qiniu baseUrl must be an HTTPS origin');
    this.timeoutMs = config.timeoutMs || 10_000;
    this.maxResponseBytes = config.maxResponseBytes || 262_144;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('Qiniu request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the read-only bucket-list Query. */ listBuckets(): ListBucketsOperation { return new ListBucketsOperation(this); }

  /** Sign one GET request and return its bounded provider response. */
  async list(call: Call): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let accessKey = '';
    let secretKey = '';
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      accessKey = credentials.accessKey instanceof SecretString ? credentials.accessKey.reveal() : '';
      secretKey = credentials.secretKey instanceof SecretString ? credentials.secretKey.reveal() : '';
    } catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!validCredential(accessKey) || !validCredential(secretKey)) return { error: 'credential', retryAfterMs: 0 };
    const date = this.now().toISOString().replace(/[-:]/g, '').replace(/\.\d{3}Z$/, 'Z');
    const origin = new URL(this.baseUrl).host;
    const canonical = `GET /buckets\nHost: ${origin}\nX-Qiniu-Date: ${date}\n\n`;
    const signature = createHmac('sha1', secretKey).update(canonical).digest('base64url') + '=';
    const request: ProviderRequest = { url: this.baseUrl.replace(/\/$/, '') + '/buckets', method: 'GET', headers: { Accept: 'application/json', 'X-Qiniu-Date': date, Authorization: `Qiniu ${accessKey}:${signature}` }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** Classify one signed bucket-list attempt using the Go contract. */
export class ListBucketsOperation implements Query<ListBucketsInput, ListBucketsOutput> {
  /** Bind a validated Qiniu client. */ constructor(private readonly client: QiniuClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return ListBucketsDefinition; }
  /** Read a bucket list, retrying only temporary unknown outcomes. */
  async invoke(call: Call): Promise<QueryAttempt<ListBucketsOutput>> {
    const exchange = await this.client.list(call);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let names: unknown;
    try { names = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { names = undefined; }
    if (!Array.isArray(names) || names.some(name => typeof name !== 'string' || !name || Buffer.byteLength(name, 'utf8') > 255)) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('listed', { buckets: names as string[] });
  }
}

function validCredential(value: string): boolean { return !!value && Buffer.byteLength(value, 'utf8') <= 256 && /^[!-~]+$/.test(value) && !value.includes(':'); }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'qiniu', operation: 'listBuckets', kind, message: 'Qiniu bucket lookup failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ListBucketsOutput> { return newQueryBranch(name, { buckets: [] }, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<ListBucketsOutput> { return newQueryRetry(failure(kind), delay); }
