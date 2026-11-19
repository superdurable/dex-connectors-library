// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash, createHmac } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetServiceUsageDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetServiceUsageInput, ServiceUsage } from './generated/models.js';

/** Inject provider transport and time for deterministic verification. */
export interface UPYUNClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Date source for signed requests. */ now?: () => Date;
}

/** Read one UPYUN storage service's usage. */
export class UPYUNClient {
  private readonly baseURL: string;
  private readonly bucket: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;

  /** Validate the storage service connection. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: UPYUNClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('UPYUN configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || 'https://v0.api.upyun.com');
    this.bucket = config.bucket;
    if (!/^[a-z][a-z0-9-]{4,19}$/.test(this.bucket)) throw new TypeError('UPYUN bucket is invalid');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1024;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('UPYUN request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the getServiceUsage Query. */ getServiceUsage(): GetServiceUsageOperation { return new GetServiceUsageOperation(this); }

  /** Sign and send one bounded usage request. */
  async get(call: Call): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let operator: string;
    let password: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      operator = credentials.operator;
      password = credentials.password instanceof SecretString ? credentials.password.reveal() : '';
    } catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!operator || operator.length > 128 || !/^[\x21-\x7e]+$/.test(operator) || operator.includes(':') || !password) return { error: 'credential', retryAfterMs: 0 };
    const uri = `/${this.bucket}/?usage`;
    const date = this.now().toUTCString();
    const signingKey = createHash('md5').update(password).digest('hex');
    const signature = createHmac('sha1', signingKey).update(`GET&${uri}&${date}`).digest('base64');
    const request: ProviderRequest = { url: `${this.baseURL}${uri}`, method: 'GET', headers: { Date: date, Authorization: `UPYUN ${operator}:${signature}` }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: this.baseURL.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    const retryAfterMs = parseRetryAfter(header(response.headers, 'retry-after'));
    if (response.body.length > this.maxResponseBytes) return { response, error: 'tooLarge', retryAfterMs };
    return { response, retryAfterMs };
  }
}

/** Return used bytes for the configured UPYUN storage service. */
export class GetServiceUsageOperation implements Query<GetServiceUsageInput, ServiceUsage> {
  /** Bind the client. */ constructor(private readonly client: UPYUNClient) {}
  /** Return the generated operation definition. */ definition(): QueryDefinition { return GetServiceUsageDefinition; }
  /** Classify the provider result according to the Go connector. */
  async invoke(call: Call, input: GetServiceUsageInput): Promise<QueryAttempt<ServiceUsage>> {
    void input;
    const exchange = await this.client.get(call);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION', 'UPYUN operator credentials are unavailable or invalid');
    if (exchange.error === 'transport' || !exchange.response) return retry('TRANSPORT', 'UPYUN request failed');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', 'UPYUN rate limited the request', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', 'UPYUN is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE', 'UPYUN usage response exceeded the configured limit');
    if (status !== 200) return branch('providerRejected', 'PROVIDER_REJECTION', `UPYUN rejected HTTP ${status}`);
    const value = Buffer.from(body).toString('utf8').trim();
    if (!/^(?:0|[1-9][0-9]*)$/.test(value)) return branch('invalidResponse', 'PROTOCOL', 'UPYUN returned invalid usage bytes');
    const bytes = Number(value);
    if (!Number.isSafeInteger(bytes)) return branch('invalidResponse', 'PROTOCOL', 'UPYUN returned invalid usage bytes');
    return newQueryBranch('read', { bytes });
  }
}

function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'upyun', operation: 'getServiceUsage', kind, message }; }
function branch(name: string, kind: Failure['kind'], message: string): QueryAttempt<ServiceUsage> { return newQueryBranch(name, { bytes: 0 }, {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<ServiceUsage> { return newQueryRetry(failure(kind, message), delay); }
