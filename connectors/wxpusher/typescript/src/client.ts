// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetSubscriberCountDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetSubscriberCountInput, SubscriberCountResult } from './generated/models.js';

const emptyOutput: SubscriberCountResult = { total: 0 };
/** Send a bounded subscriber-count request. */
export type SubscriberCountRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Optional caller-owned transport for provider tests. */
export interface WxPusherClientOptions { send?: SubscriberCountRequestSender }
/** Resolve the private app token on every request. */
export class WxPusherClient {
  readonly baseUrl: string;
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly credentials: CredentialProvider<Credentials>;
  readonly send: SubscriberCountRequestSender;
  /** Validate configuration without contacting WxPusher. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: WxPusherClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('WxPusher configuration and credentials are required');
    this.baseUrl = config.baseUrl ?? 'https://wxpusher.zjiecode.com';
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 65_536;
    const url = new URL(this.baseUrl);
    if (url.protocol !== 'https:' || url.pathname !== '/' || url.search || url.hash || !Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('WxPusher configuration is invalid');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the subscriber-count Query. */ getSubscriberCount(): GetSubscriberCountOperation { return new GetSubscriberCountOperation(this); }
}
/** Return the aggregate count without exposing subscriber identities. */
export class GetSubscriberCountOperation implements Query<GetSubscriberCountInput, SubscriberCountResult> {
  /** Bind an authenticated client. */ constructor(private readonly client: WxPusherClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return GetSubscriberCountDefinition; }
  /** Send and classify one count request. */
  async invoke(call: Call, input: GetSubscriberCountInput): Promise<QueryAttempt<SubscriberCountResult>> {
    if (input === null || typeof input !== 'object' || Array.isArray(input)) return branch('defect', 'VALIDATION', 'WxPusher subscriber request is invalid');
    let credentials: Credentials;
    try { credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION', 'WxPusher application token is unavailable or invalid'); }
    const token = credentials.appToken instanceof SecretString ? credentials.appToken.reveal() : '';
    if (!token.startsWith('AT_') || token.length > 256 || !/^[!-~]+$/.test(token)) return branch('defect', 'AUTHENTICATION', 'WxPusher application token is unavailable or invalid');
    const query = new URLSearchParams({ appToken: token, page: '1', pageSize: '1' });
    let response: ProviderResponse;
    try { response = await this.client.send({ url: `${this.client.baseUrl}/api/fun/wxuser/v2?${query}`, method: 'GET', headers: { Accept: 'application/json' }, timeoutMs: this.client.timeoutMs, maxResponseBytes: this.client.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal }); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status === 200) return branch('invalidResponse', 'RESPONSE_TOO_LARGE', 'WxPusher response exceeded the configured limit');
      if (error instanceof BodyTooLargeError && error.status !== undefined) return classifyStatus(error.status, error.headers ?? {});
      return retry('AVAILABILITY', 'WxPusher subscriber request failed');
    }
    if (response.status !== 200) return classifyStatus(response.status, response.headers);
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL', 'WxPusher returned an invalid subscriber response'); }
    if (!isRecord(wire) || !Number.isInteger(wire.code)) return branch('invalidResponse', 'PROTOCOL', 'WxPusher returned an invalid subscriber response');
    if (wire.code !== 1000) return branch('providerRejected', 'PROVIDER_REJECTION', 'WxPusher rejected subscriber query');
    if (wire.success !== true || !isRecord(wire.data) || !Number.isSafeInteger(wire.data.total) || (wire.data.total as number) < 0 || wire.data.page !== 1 || wire.data.pageSize !== 1 || !Array.isArray(wire.data.records)) return branch('invalidResponse', 'PROTOCOL', 'WxPusher returned an invalid subscriber response');
    return newQueryBranch('counted', { total: wire.data.total as number });
  }
}
function classifyStatus(status: number, headers: ProviderResponse['headers']): QueryAttempt<SubscriberCountResult> {
  const value = headers['retry-after']; const delay = parseRetryAfter(Array.isArray(value) ? value[0] ?? '' : value ?? '');
  if (status === 429) return retry('RATE_LIMIT', 'WxPusher rate limited subscriber query', delay);
  if (status === 408 || status >= 500) return retry('AVAILABILITY', 'WxPusher subscriber API is temporarily unavailable', delay);
  const kind: Failure['kind'] = status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : [400, 404, 412].includes(status) ? 'VALIDATION' : 'PROVIDER_REJECTION';
  return branch('providerRejected', kind, `WxPusher rejected subscriber query with HTTP ${status}`);
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'wxpusher', operation: 'getSubscriberCount', kind, message }; }
function branch(id: string, kind: Failure['kind'], message: string): QueryAttempt<SubscriberCountResult> { return newQueryBranch(id, emptyOutput, {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delayMs = 0): QueryAttempt<SubscriberCountResult> { return newQueryRetry(failure(kind, message), delayMs); }
