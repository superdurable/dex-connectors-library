// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash, createHmac } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetServiceUsageDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetServiceUsageInput, ServiceUsage } from './generated/models.js';

/** Optional HTTP sender and clock for UPYUN provider tests. */
export interface UpyunClientOptions {
  /** Sender; omitted uses the SDK HTTP transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Clock; omitted uses the current UTC time. */ now?: () => Date;
}

/** Sign reads of one configured UPYUN storage service. */
export class UpyunClient {
  private readonly baseUrl: string;
  private readonly bucket: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;

  /** Validate bucket, API origin, and response bounds. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: UpyunClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('UPYUN configuration and credentials are required');
    this.bucket = config.bucket;
    if (!/^[a-z][a-z0-9-]{4,19}$/.test(this.bucket)) throw new TypeError('UPYUN bucket is invalid');
    this.baseUrl = config.baseUrl || 'https://v0.api.upyun.com';
    const parsed = new URL(this.baseUrl);
    if (parsed.protocol !== 'https:' || parsed.username || parsed.password || parsed.pathname !== '/' || parsed.search || parsed.hash) throw new TypeError('UPYUN baseUrl must be an HTTPS origin');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_024;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('UPYUN request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the service-usage Query. */ getServiceUsage(): GetServiceUsageOperation { return new GetServiceUsageOperation(this); }

  /** Sign one usage request with the current operator password. */
  async read(call: Call): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let operator = '';
    let password = '';
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      operator = credentials.operator;
      password = credentials.password instanceof SecretString ? credentials.password.reveal() : '';
    } catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!operator || operator.length > 128 || !/^[!-~]+$/.test(operator) || operator.includes(':') || !password) return { error: 'credential', retryAfterMs: 0 };
    const uri = `/${this.bucket}/?usage`;
    const date = this.now().toUTCString();
    const key = createHash('md5').update(password).digest('hex');
    const signature = createHmac('sha1', key).update(`GET&${uri}&${date}`).digest('base64');
    const request: ProviderRequest = { url: this.baseUrl.replace(/\/$/, '') + uri, method: 'GET', headers: { Date: date, Authorization: `UPYUN ${operator}:${signature}` }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** Classify UPYUN usage responses and temporary failures. */
export class GetServiceUsageOperation implements Query<GetServiceUsageInput, ServiceUsage> {
  /** Bind a validated UPYUN client. */ constructor(private readonly client: UpyunClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return GetServiceUsageDefinition; }
  /** Return used bytes for the configured service. */
  async invoke(call: Call): Promise<QueryAttempt<ServiceUsage>> {
    const exchange = await this.client.read(call);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('TRANSPORT');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', 'PROVIDER_REJECTION');
    let text: string;
    try { text = new TextDecoder('utf-8', { fatal: true }).decode(body).trim(); } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!/^[+]?\d+$/.test(text)) return branch('invalidResponse', 'PROTOCOL');
    const bytes = Number(text);
    if (!Number.isSafeInteger(bytes) || bytes < 0) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('read', { bytes });
  }
}

function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'upyun', operation: 'getServiceUsage', kind, message: 'UPYUN usage query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ServiceUsage> { return newQueryBranch(name, { bytes: 0 }, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<ServiceUsage> { return newQueryRetry(failure(kind), delay); }
