// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetWaybillBalanceDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetWaybillBalanceInput, GetWaybillBalanceOutput } from './generated/models.js';

const endpoint = 'https://openapi.yundaex.com/openapi-api/v1/accountOrder/searchCount';
const emptyOutput: GetWaybillBalanceOutput = { freeCount: 0, paidCount: 0 };

/** Optional transport and clock dependencies for deterministic provider tests. */
export interface YundaClientOptions {
  /** Production uses the Connector SDK HTTPS sender. */
  send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Production uses the current Unix timestamp. */
  now?: () => number;
}

/** Resolve Yunda application and Baima credentials for each balance read. */
export class YundaClient {
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  readonly now: () => number;
  /** Validate transport limits without contacting Yunda. */
  constructor(config: Config, readonly credentials: CredentialProvider<Credentials>, options: YundaClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Yunda configuration and credentials are required');
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('Yunda request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? Date.now;
  }
  /** Bind the read-only Baima waybill balance Query. */
  getWaybillBalance(): GetWaybillBalanceOperation { return new GetWaybillBalanceOperation(this); }
}

/** Read the free and paid ordinary electronic waybill counts. */
export class GetWaybillBalanceOperation implements Query<GetWaybillBalanceInput, GetWaybillBalanceOutput> {
  /** Bind the configured provider client. */
  constructor(private readonly client: YundaClient) {}
  /** Return the generated Query contract. */
  definition(): QueryDefinition { return GetWaybillBalanceDefinition; }
  /** Send one signed balance request and classify its response. */
  async invoke(call: Call, input: GetWaybillBalanceInput): Promise<QueryAttempt<GetWaybillBalanceOutput>> {
    if (input === null || typeof input !== 'object' || Array.isArray(input)) return branch('defect', 'VALIDATION');
    let credentials: Credentials;
    try { credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION'); }
    const appKey = revealCredential(credentials.appKey);
    const appSecret = revealCredential(credentials.appSecret);
    const partnerID = revealCredential(credentials.partnerID);
    const baimaSecret = revealCredential(credentials.baimaSecret);
    if (![appKey, appSecret, partnerID, baimaSecret].every(isValidCredential)) return branch('defect', 'AUTHENTICATION');
    const body = new TextEncoder().encode(JSON.stringify({ appid: appKey, partner_id: partnerID, secret: baimaSecret, type: 'common' }));
    const sign = createHash('md5').update(body).update('_').update(appSecret).digest('hex');
    let response: ProviderResponse;
    try {
      response = await this.client.send({ url: endpoint, method: 'POST',
        headers: { 'Content-Type': 'application/json', Accept: 'application/json', 'app-key': appKey, sign, 'req-time': String(this.client.now()) },
        body, timeoutMs: this.client.timeoutMs, maxResponseBytes: this.client.maxResponseBytes,
        addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal });
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status === 200) return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
      if (error instanceof BodyTooLargeError && error.status !== undefined) return classifyStatus(error.status, error.headers ?? {});
      return retry('AVAILABILITY');
    }
    if (response.status !== 200) return classifyStatus(response.status, response.headers);
    if (response.body.length > this.client.maxResponseBytes) return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let document: unknown;
    try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(document) || typeof document.code !== 'string' || typeof document.result !== 'boolean') return branch('invalidResponse', 'PROTOCOL');
    if (document.code === '7777') return retry('AVAILABILITY', retryDelay(response.headers));
    if (document.code !== '0000' || !document.result) return branch('providerRejected', 'PROVIDER_REJECTION');
    if (!isRecord(document.data) || document.data.status !== '1') return branch('invalidResponse', 'PROTOCOL');
    const freeCount = parseCount(document.data.free_num);
    const paidCount = parseCount(document.data.remain_num);
    if (freeCount === undefined || paidCount === undefined) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('found', { freeCount, paidCount });
  }
}

function classifyStatus(status: number, headers: ProviderResponse['headers']): QueryAttempt<GetWaybillBalanceOutput> {
  if (status === 429) return retry('RATE_LIMIT', retryDelay(headers));
  if (status === 408 || status >= 500) return retry('AVAILABILITY', retryDelay(headers));
  return branch('providerRejected', 'PROVIDER_REJECTION');
}
function retryDelay(headers: ProviderResponse['headers']): number {
  const value = headers['retry-after']; return parseRetryAfter(Array.isArray(value) ? value[0] ?? '' : value ?? '');
}
function revealCredential(value: unknown): string { return value instanceof SecretString ? value.reveal() : ''; }
function isValidCredential(value: string): boolean { return value.length > 0 && value.length <= 1024 && /^[!-~]+$/.test(value); }
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function parseCount(value: unknown): number | undefined {
  if (typeof value !== 'string' || value.length === 0 || value.length > 19 || !/^[0-9]+$/.test(value)) return undefined;
  const count = Number(value); return Number.isSafeInteger(count) && count >= 0 ? count : undefined;
}
function failure(kind: Failure['kind']): Failure { return { provider: 'yunda', operation: 'getWaybillBalance', kind, message: 'Yunda balance request failed' }; }
function branch(id: string, kind: Failure['kind']): QueryAttempt<GetWaybillBalanceOutput> { return newQueryBranch(id, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delayMs = 0): QueryAttempt<GetWaybillBalanceOutput> { return newQueryRetry(failure(kind), delayMs); }
