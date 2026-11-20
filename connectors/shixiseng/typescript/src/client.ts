// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListIndustriesDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListIndustriesInput, ListIndustriesOutput } from './generated/models.js';

/** Optional provider sender and signature clock for deterministic tests. */
export interface ShixisengClientOptions {
  /** Send a bounded provider request. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Return the signature timestamp. */ now?: () => Date;
}

/** Read public industry categories with merchant credentials. */
export class ShixisengClient {
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;
  /** Validate immutable request configuration. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: ShixisengClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Shixiseng configuration and credentials are required');
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > 16 << 20) throw new TypeError('Shixiseng request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }
  /** Bind the industry-list Query. */ listIndustries(): ListIndustriesOperation { return new ListIndustriesOperation(this); }
  /** Resolve credentials and send one signed industry query. */
  async request(call: Call): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    const appID = credentials.appID instanceof SecretString ? credentials.appID.reveal() : '';
    const appSecret = credentials.appSecret instanceof SecretString ? credentials.appSecret.reveal() : '';
    if (![appID, appSecret].every(isValidCredential)) return { error: 'credential', retryAfterMs: 0 };
    const timestamp = String(Math.floor(this.now().getTime() / 1000));
    const signature = createHash('md5').update(appID + appSecret + timestamp).digest('hex').toUpperCase();
    const request: ProviderRequest = {
      url: `https://open.shixiseng.com/industry/list?${new URLSearchParams({ sign: signature })}`,
      method: 'GET', headers: { Authorization: Buffer.from(`${appID}:${timestamp}`).toString('base64'), Accept: 'text/html' },
      timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    const retryAfter = Object.entries(response.headers).find(([name]) => name.toLowerCase() === 'retry-after')?.[1];
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(Array.isArray(retryAfter) ? retryAfter[0] ?? '' : retryAfter ?? '') };
  }
}

/** Classify signed industry-list outcomes. */
export class ListIndustriesOperation implements Query<ListIndustriesInput, ListIndustriesOutput> {
  /** Bind the validated client. */ constructor(private readonly client: ShixisengClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return ListIndustriesDefinition; }
  /** Return public category names or a retry decision. */
  async invoke(call: Call, input: ListIndustriesInput): Promise<QueryAttempt<ListIndustriesOutput>> {
    void input;
    const exchange = await this.client.request(call);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (status !== 200) return branch('providerRejected', 'PROVIDER_REJECTION');
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(wire) || !Number.isInteger(wire.code)) return branch('invalidResponse', 'PROTOCOL');
    if (wire.code === 300) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (wire.code !== 100) return branch('providerRejected', 'PROVIDER_REJECTION');
    const message = wire.msg;
    if (!isRecord(message) || !Array.isArray(message.industry_list) || message.industry_list.length < 1 || message.industry_list.length > 256 || !message.industry_list.every(value => typeof value === 'string' && value.length > 0 && value.length <= 256 && value.trim() === value)) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('found', { industries: message.industry_list as string[] });
  }
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function isValidCredential(value: string): boolean { return !!value && value.length <= 1024 && value.trim() === value && ![...value].some(character => character.charCodeAt(0) < 32 || character.charCodeAt(0) === 127); }
function failure(kind: Failure['kind']): Failure { return { provider: 'shixiseng', operation: 'listIndustries', kind, message: 'Shixiseng industry query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ListIndustriesOutput> { return newQueryBranch(name, { industries: [] }, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<ListIndustriesOutput> { return newQueryRetry(failure(kind), delay); }
