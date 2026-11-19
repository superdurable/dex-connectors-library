// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListChannelsDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListChannelsInput, ListChannelsOutput } from './generated/models.js';

const emptyOutput: ListChannelsOutput = { channelIds: [] };
/** Optional Polyv transport and signing clock for deterministic tests. */
export interface PolyvClientOptions {
  /** Provider HTTP sender. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Clock used for the signed request timestamp. */ now?: () => Date;
}
/** Read signed Polyv live channel IDs. */
export class PolyvClient {
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;
  /** Validate Polyv connection settings without provider I/O. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: PolyvClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Polyv configuration and credential source are required');
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('Polyv request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }
  /** Bind the channel-list Query. */ listChannels(): ListChannelsOperation { return new ListChannelsOperation(this); }
  /** Resolve credentials and dispatch one signed request. */
  async request(call: Call): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); } catch { return { error: 'credential', retryAfterMs: 0 }; }
    const appId = credentials.appId;
    const secret = credentials.appSecret instanceof SecretString ? credentials.appSecret.reveal() : '';
    if (typeof appId !== 'string' || ![appId, secret].every(value => !!value && value.length <= 512 && /^[!-~]+$/.test(value))) return { error: 'credential', retryAfterMs: 0 };
    const timestamp = this.now().getTime().toString();
    const signature = createHash('md5').update(secret + 'appId' + appId + 'timestamp' + timestamp + secret).digest('hex').toUpperCase();
    const url = `https://api.polyv.net/live/v3/user/channels?${new URLSearchParams({ appId, sign: signature, timestamp }).toString()}`;
    const request: ProviderRequest = { url, method: 'GET', headers: { Accept: 'application/json' }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); } catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    const retryAfterValue = Object.entries(response.headers).find(([key]) => key.toLowerCase() === 'retry-after')?.[1];
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(Array.isArray(retryAfterValue) ? retryAfterValue[0] ?? '' : retryAfterValue ?? '') };
  }
}
/** Classify the Polyv channel-list response. */
export class ListChannelsOperation implements Query<ListChannelsInput, ListChannelsOutput> {
  /** Bind a validated client. */ constructor(private readonly client: PolyvClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return ListChannelsDefinition; }
  /** Read only channel identifiers, never channel configuration. */
  async invoke(call: Call, _input: ListChannelsInput): Promise<QueryAttempt<ListChannelsOutput>> {
    void _input;
    const exchange = await this.client.request(call);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(wire) || !Number.isInteger(wire.code)) return branch('invalidResponse', 'PROTOCOL');
    if (wire.code !== 200) {
      if (wire.message === 'qps exceeds number of calls') return retry('RATE_LIMIT', exchange.retryAfterMs);
      return branch('providerRejected', wire.message === 'invalid signature.' || wire.message === 'need to login' ? 'AUTHENTICATION' : wire.message === 'access forbidden' ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    }
    if (!isRecord(wire.data) || !Array.isArray(wire.data.channels)) return branch('invalidResponse', 'PROTOCOL');
    const channelIds: string[] = [];
    for (const raw of wire.data.channels) {
      const channelId = typeof raw === 'string' ? raw : Number.isSafeInteger(raw) ? String(raw) : '';
      if (!/^\d{1,64}$/.test(channelId)) return branch('invalidResponse', 'PROTOCOL');
      channelIds.push(channelId);
    }
    return newQueryBranch('listed', { channelIds });
  }
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'polyv', operation: 'listChannels', kind, message: 'Polyv provider request failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ListChannelsOutput> { return newQueryBranch(name, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<ListChannelsOutput> { return newQueryRetry(failure(kind), delay); }
