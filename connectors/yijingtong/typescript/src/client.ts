// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListWarehousesDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListWarehousesInput, WarehousePage, Warehouse } from './generated/models.js';

const emptyOutput: WarehousePage = { warehouses: [] };
/** Sender used for Yijingtong warehouse requests. */
export type YijingtongRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Optional HTTP sender for provider tests. */
export interface YijingtongClientOptions { /** Production uses the SDK sender. */ send?: YijingtongRequestSender; }
/** Query the tenant's authorized Yijingtong warehouses. */
export class YijingtongClient {
  readonly baseUrl: string;
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly credentials: CredentialProvider<Credentials>;
  readonly send: YijingtongRequestSender;
  /** Require a tenant system origin and bounded request settings. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: YijingtongClientOptions = {}) {
    if (!config || !credentials || !config.baseUrl) throw new TypeError('Yijingtong system origin and credentials are required');
    this.baseUrl = validateBaseURL(config.baseUrl).replace(/\/$/, '');
    if (new URL(this.baseUrl).pathname !== '/') throw new TypeError('Yijingtong URL must be an origin');
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Yijingtong limits must be positive');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the warehouse-list Query. */ listWarehouses(): ListWarehousesOperation { return new ListWarehousesOperation(this); }
}
/** Read authorized warehouse names and countries. */
export class ListWarehousesOperation implements Query<ListWarehousesInput, WarehousePage> {
  /** Bind the Query client. */ constructor(private readonly client: YijingtongClient) {}
  /** Stable manifest definition. */ definition(): QueryDefinition { return ListWarehousesDefinition; }
  /** Execute one warehouse request and classify the provider result. */
  async invoke(call: Call, input: ListWarehousesInput): Promise<QueryAttempt<WarehousePage>> {
    void input;
    let credentials: Credentials;
    try { credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION'); }
    const token = credentials.token instanceof SecretString ? credentials.token.reveal() : '';
    if (!token || token.length > 2048 || !/^[!-~]+$/.test(token)) return branch('defect', 'AUTHENTICATION');
    const body = new TextEncoder().encode(JSON.stringify({ token }));
    let response: ProviderResponse;
    try { response = await this.client.send({ url: this.client.baseUrl + '/hwc_api/hwc_storage_list.php', method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json; charset=utf-8' }, body,
      timeoutMs: this.client.timeoutMs, maxResponseBytes: this.client.maxResponseBytes,
      addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal }); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        if (retryable(error.status)) return classifyStatus(error.status, error.headers ?? {});
        return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
      }
      return retry('AVAILABILITY');
    }
    if (retryable(response.status)) return classifyStatus(response.status, response.headers);
    if (response.body.length > this.client.maxResponseBytes) return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (response.status !== 200) return classifyStatus(response.status, response.headers);
    let document: unknown;
    try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(document)) return branch('invalidResponse', 'PROTOCOL');
    const envelope = lowerCaseFields(document);
    if (envelope.error !== 'T' && envelope.error !== 'F') return branch('invalidResponse', 'PROTOCOL');
    const data = envelope.data;
    if (data != null && !Array.isArray(data)) return branch('invalidResponse', 'PROTOCOL');
    const warehouses: Warehouse[] = [];
    for (const item of (data ?? []) as unknown[]) {
      if (item !== null && !isRecord(item)) return branch('invalidResponse', 'PROTOCOL');
      const fields = lowerCaseFields(item ?? {});
      const id = fields.nums ?? '', name = fields.pname ?? '', country = fields.country ?? '';
      if (typeof id !== 'string' || typeof name !== 'string' || typeof country !== 'string') return branch('invalidResponse', 'PROTOCOL');
      warehouses.push({ id, name, country });
    }
    if (envelope.error === 'F') return branch('providerRejected', 'PROVIDER_REJECTION');
    if (!data || warehouses.length > 200 || warehouses.some(({ id, name }) => !/[^\p{White_Space}]/u.test(id) || !/[^\p{White_Space}]/u.test(name))) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('listed', { warehouses });
  }
}
function retryable(status: number): boolean { return status === 408 || status === 429 || status >= 500; }
function lowerCaseFields(record: Record<string, unknown>): Record<string, unknown> {
  return Object.fromEntries(Object.entries(record).map(([key, value]) => [key.toLowerCase(), value]));
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function classifyStatus(status: number, headers: ProviderResponse['headers']): QueryAttempt<WarehousePage> {
  const value = headers['retry-after']; const delay = parseRetryAfter(Array.isArray(value) ? value[0] ?? '' : value ?? '');
  if (status === 429) return retry('RATE_LIMIT', delay);
  if (status === 408 || status >= 500) return retry('AVAILABILITY', delay);
  const kind: Failure['kind'] = status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : [400, 404, 412].includes(status) ? 'VALIDATION' : 'PROVIDER_REJECTION';
  return branch('providerRejected', kind);
}
function failure(kind: Failure['kind']): Failure { return { provider: 'yijingtong', operation: 'listWarehouses', kind, message: 'Yijingtong warehouse request failed' }; }
function branch(id: string, kind: Failure['kind']): QueryAttempt<WarehousePage> { return newQueryBranch(id, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<WarehousePage> { return newQueryRetry(failure(kind), delay); }
