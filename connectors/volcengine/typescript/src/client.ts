// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash, createHmac } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { DescribeRegionsDefinition } from './generated/definitions.js';
import type { Config, Credentials, DescribeRegionsInput, DescribeRegionsOutput } from './generated/models.js';

/** Optional sender and clock for local provider tests. */
export interface VolcengineClientOptions {
  /** Sender; omitted uses the SDK HTTP transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** UTC clock; omitted uses current time. */ now?: () => Date;
}

/** Sign and read a bounded ECS region page. */
export class VolcengineClient {
  private readonly baseUrl: string;
  private readonly region: string;
  private readonly service: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;
  /** Validate local settings without contacting ECS. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: VolcengineClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Volcengine configuration and credentials are required');
    this.baseUrl = config.baseUrl || 'https://ecs.cn-beijing.volcengineapi.com';
    this.region = config.region || 'cn-beijing';
    this.service = config.service || 'ecs';
    this.timeoutMs = config.timeoutMs || 10_000;
    this.maxResponseBytes = config.maxResponseBytes || 262_144;
    const parsed = new URL(this.baseUrl);
    if (!(parsed.protocol === 'https:' || (parsed.protocol === 'http:' && parsed.hostname === '127.0.0.1')) || parsed.username || parsed.password || parsed.pathname !== '/' || parsed.search || parsed.hash || !/^[a-z0-9_-]{1,64}$/.test(this.region) || !/^[a-z0-9_-]{1,64}$/.test(this.service) || !Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('Volcengine request configuration is invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }
  /** Bind the region-list Query. */ describeRegions(): DescribeRegionsOperation { return new DescribeRegionsOperation(this); }
  /** Sign and send one ECS request with current credentials. */
  async request(call: Call, input: DescribeRegionsInput): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let accessKeyId = '', secretAccessKey = '';
    try { const credentials = await this.credentials.resolve(call, call.context.cancellationSignal); accessKeyId = credentials.accessKeyId instanceof SecretString ? credentials.accessKeyId.reveal() : ''; secretAccessKey = credentials.secretAccessKey instanceof SecretString ? credentials.secretAccessKey.reveal() : ''; }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!validAccessKey(accessKeyId) || !validAccessKey(secretAccessKey)) return { error: 'credential', retryAfterMs: 0 };
    const query = new URLSearchParams({ Action: 'DescribeRegions', MaxResults: String(input.maxResults || 10), ...(input.nextToken ? { NextToken: input.nextToken } : {}), Version: '2020-04-01' }).toString();
    const host = new URL(this.baseUrl).host;
    const requestDate = this.now().toISOString().replace(/[-:]/g, '').replace(/\.\d{3}Z$/, 'Z');
    const authorization = signRequest(query, host, requestDate, this.region, this.service, accessKeyId, secretAccessKey);
    const request: ProviderRequest = { url: `${this.baseUrl}/?${query}`, method: 'GET', headers: { 'X-Date': requestDate, Region: this.region, Service: this.service, Authorization: authorization, Accept: 'application/json' }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** List one page of ECS region IDs. */
export class DescribeRegionsOperation implements Query<DescribeRegionsInput, DescribeRegionsOutput> {
  /** Bind the provider client. */ constructor(private readonly client: VolcengineClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return DescribeRegionsDefinition; }
  /** Classify one signed ECS response using the Go connector contract. */
  async invoke(call: Call, input: DescribeRegionsInput): Promise<QueryAttempt<DescribeRegionsOutput>> {
    if ((input.maxResults ?? 0) < 0 || (input.maxResults ?? 0) > 20 || !Number.isInteger(input.maxResults ?? 0) || (input.nextToken ?? '').length > 2048 || (input.nextToken ?? '').trim() !== (input.nextToken ?? '')) return branch('defect', 'VALIDATION');
    const exchange = await this.client.request(call, input);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { wire = undefined; }
    if (!isObject(wire) || !isObject(wire.ResponseMetadata)) return status === 200 ? branch('invalidResponse', 'PROTOCOL') : branch('providerRejected', 'PROVIDER_REJECTION');
    const providerError = isObject(wire.ResponseMetadata.Error) ? wire.ResponseMetadata.Error.Code : undefined;
    if (providerError !== undefined && (typeof providerError !== 'string' || !providerError)) return status === 200 ? branch('invalidResponse', 'PROTOCOL') : branch('providerRejected', 'PROVIDER_REJECTION');
    if (status !== 200 || providerError) {
      if (providerError === 'FlowLimitExceeded') return retry('RATE_LIMIT', exchange.retryAfterMs);
      if (providerError === 'InternalError') return retry('AVAILABILITY', exchange.retryAfterMs);
      const code = typeof providerError === 'string' ? providerError : '';
      return branch('providerRejected', status === 401 || code.includes('Auth') || code.includes('Signature') ? 'AUTHENTICATION' : status === 403 || code.includes('Permission') ? 'AUTHORIZATION' : status === 400 ? 'VALIDATION' : 'PROVIDER_REJECTION');
    }
    if (!isObject(wire.Result) || !Array.isArray(wire.Result.Regions) || wire.Result.Regions.length > 20) return branch('invalidResponse', 'PROTOCOL');
    const regionIds: string[] = [];
    for (const region of wire.Result.Regions) { if (!isObject(region) || typeof region.RegionId !== 'string' || !region.RegionId) return branch('invalidResponse', 'PROTOCOL'); regionIds.push(region.RegionId); }
    return newQueryBranch('listed', { regionIds, nextToken: typeof wire.Result.NextToken === 'string' ? wire.Result.NextToken : '' });
  }
}

function sha256(value: string): string { return createHash('sha256').update(value).digest('hex'); }
function hmac(key: Buffer | string, value: string): Buffer { return createHmac('sha256', key).update(value).digest(); }
function signRequest(query: string, host: string, requestDate: string, region: string, service: string, accessKeyId: string, secretAccessKey: string): string {
  const canonicalHeaders = `host:${host}\nx-date:${requestDate}\n`;
  const canonicalRequest = ['GET', '/', query, canonicalHeaders, 'host;x-date', sha256('')].join('\n');
  const scope = `${requestDate.slice(0, 8)}/${region}/${service}/request`;
  const stringToSign = `HMAC-SHA256\n${requestDate}\n${scope}\n${sha256(canonicalRequest)}`;
  let signingKey = hmac(secretAccessKey, requestDate.slice(0, 8));
  signingKey = hmac(signingKey, region);
  signingKey = hmac(signingKey, service);
  signingKey = hmac(signingKey, 'request');
  const signature = hmac(signingKey, stringToSign).toString('hex');
  return `HMAC-SHA256 Credential=${accessKeyId}/${scope}, SignedHeaders=host;x-date, Signature=${signature}`;
}
function validAccessKey(value: string): boolean { return !!value && value.length <= 512 && !/[^\x21-\x7e]/.test(value); }
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'volcengine', operation: 'describeRegions', kind, message: 'Volcengine ECS region query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<DescribeRegionsOutput> { return newQueryBranch(name, { regionIds: [], nextToken: '' }, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<DescribeRegionsOutput> { return newQueryRetry(failure(kind), delay); }
