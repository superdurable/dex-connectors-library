// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListWarehousesDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListWarehousesInput, Warehouse, WarehousePage } from './generated/models.js';

const emptyPage = (): WarehousePage => ({ warehouses: [] });
/** Provider HTTP sender, injectable for deterministic tests. */
export type Chukou1RequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Client dependencies for tests and approved transports. */
export interface Chukou1ClientOptions {
  /** SDK transport when omitted. */ send?: Chukou1RequestSender;
  /** Clock used to interpret HTTP Retry-After dates. */ now?: () => Date;
}
/** Authenticated Chukou1 overseas warehouse client. */
export class Chukou1Client {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: Chukou1RequestSender;
  private readonly now: () => Date;
  /** Validate settings without contacting the provider; credentials resolve for each call. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: Chukou1ClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Chukou1 configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || 'https://openapi.chukou1.cn').replace(/\/$/, '');
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Chukou1 timeout and maxResponseBytes must be positive whole numbers');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }
  /** Bind the read-only listWarehouses Query. */ listWarehouses(): ListWarehousesOperation { return new ListWarehousesOperation(this); }
  /** Resolve private credentials and send one bounded warehouse request. */
  async list(call: Call): Promise<{ response: ProviderResponse } | { failure: Failure; retry?: true; retryAfterMs?: number; branch?: string }> {
    let token: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      token = credentials.accessToken instanceof SecretString ? credentials.accessToken.reveal() : '';
    } catch { return { branch: 'defect', failure: failure('AUTHENTICATION', 'Chukou1 access token is unavailable or invalid') }; }
    if (!validToken(token)) return { branch: 'defect', failure: failure('AUTHENTICATION', 'Chukou1 access token is unavailable or invalid') };
    const request: ProviderRequest = {
      url: `${this.baseURL}/v1/warehouses`, method: 'GET',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json; charset=utf-8', Authorization: `Bearer ${token}` },
      timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes,
      addressPolicy: this.baseURL.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        if (error.status === 429 || error.status === 408 || error.status >= 500) return statusOutcome(error.status, error.headers ?? {}, this.now());
        return { branch: 'invalidResponse', failure: failure('RESPONSE_TOO_LARGE', 'Chukou1 response exceeded the configured limit') };
      }
      return { retry: true, failure: failure('AVAILABILITY', 'Chukou1 warehouse request failed') };
    }
    if (response.status === 429 || response.status === 408 || response.status >= 500) return statusOutcome(response.status, response.headers, this.now());
    if (response.body.length > this.maxResponseBytes) return { branch: 'invalidResponse', failure: failure('RESPONSE_TOO_LARGE', 'Chukou1 response exceeded the configured limit') };
    return { response };
  }
}
/** Read the authorized overseas warehouse catalog. */
export class ListWarehousesOperation implements Query<ListWarehousesInput, WarehousePage> {
  /** Bind the provider client. */ constructor(private readonly client: Chukou1Client) {}
  /** Generated Query definition. */ definition(): QueryDefinition { return ListWarehousesDefinition; }
  /** Send one read-only request and classify its result. */
  async invoke(call: Call, input: ListWarehousesInput): Promise<QueryAttempt<WarehousePage>> {
    if (!input || Object.keys(input).length !== 0) return terminal('defect', 'VALIDATION', 'warehouse list input must be empty');
    const exchange = await this.client.list(call);
    if ('failure' in exchange) return exchange.retry ? newQueryRetry(exchange.failure, exchange.retryAfterMs ?? 0) : newQueryBranch(exchange.branch!, emptyPage(), {}, exchange.failure);
    const response = exchange.response;
    if (response.status !== 200) return terminal('providerRejected', httpFailureKind(response.status), `Chukou1 rejected warehouse query with HTTP ${response.status}`);
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; }
    catch { return terminal('invalidResponse', 'PROTOCOL', 'Chukou1 returned an invalid warehouse response'); }
    if (!Array.isArray(wire) || wire.length > 200) return terminal('invalidResponse', 'PROTOCOL', 'Chukou1 returned an invalid warehouse response');
    const warehouses: Warehouse[] = [];
    for (const item of wire) {
      if (!item || typeof item !== 'object' || Array.isArray(item)) return terminal('invalidResponse', 'PROTOCOL', 'Chukou1 returned an invalid warehouse response');
      const record = item as Record<string, unknown>;
      const id = stringField(record.WarehouseId);
      const name = stringField(record.WarehouseName);
      const country = stringField(record.Country);
      const countryCode = stringField(record.CountryCode);
      if (id === undefined || name === undefined || country === undefined || countryCode === undefined || !id || !name || !countryCode)
        return terminal('invalidResponse', 'PROTOCOL', 'Chukou1 returned an invalid warehouse response');
      warehouses.push({ id, name, country, countryCode });
    }
    return newQueryBranch('listed', { warehouses });
  }
}
function stringField(value: unknown): string | undefined { return value == null ? '' : typeof value === 'string' ? value : undefined; }
function validToken(value: string): boolean { return value.length > 0 && value.length <= 2048 && /^[!-~]+$/.test(value) && !value.toLowerCase().startsWith('bearer '); }
function failure(kind: Failure['kind'], message: string): Failure { return { kind, provider: 'chukou1', operation: 'listWarehouses', message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<WarehousePage> { return newQueryBranch(branch, emptyPage(), {}, failure(kind, message)); }
function httpFailureKind(status: number): Failure['kind'] { return status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : [400, 404, 412].includes(status) ? 'VALIDATION' : 'PROVIDER_REJECTION'; }
function statusOutcome(status: number, headers: ProviderResponse['headers'], now: Date): { retry: true; failure: Failure; retryAfterMs: number } {
  const entry = Object.entries(headers).find(([name]) => name.toLowerCase() === 'retry-after')?.[1];
  const retryAfter = Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
  return { retry: true, retryAfterMs: parseRetryAfter(retryAfter, now), failure: status === 429 ? failure('RATE_LIMIT', 'Chukou1 rate limited warehouse query') : failure('AVAILABILITY', 'Chukou1 warehouse API is temporarily unavailable') };
}
