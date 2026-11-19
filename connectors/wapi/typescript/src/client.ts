// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListVehicleBrandsDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListVehicleBrandsInput, ListVehicleBrandsOutput } from './generated/models.js';

const empty: ListVehicleBrandsOutput = { total: 0, brands: [] };
/** Optional deterministic transport and clock for provider verification. */
export interface WapiClientOptions {
  /** Send one HTTPS request. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Supply the time used to sign a request. */ now?: () => Date;
}
/** Resolve current Wapi app credentials for each brand-price Query. */
export class WapiClient {
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;
  /** Validate request bounds without contacting Wapi. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: WapiClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Wapi configuration and credential source are required');
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('Wapi request bounds are invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }
  /** Bind the brand-price Query. */ listVehicleBrands(): ListVehicleBrandsOperation { return new ListVehicleBrandsOperation(this); }
  /** Sign one bounded provider request and classify its response. */
  async request(call: Call): Promise<QueryAttempt<ListVehicleBrandsOutput>> {
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION'); }
    const appID = credentials.appID;
    const secret = credentials.appSecret instanceof SecretString ? credentials.appSecret.reveal() : '';
    if (typeof appID !== 'string' || !/^\d{1,20}$/.test(appID) || !/^[A-Za-z0-9]{32}$/.test(secret)) return branch('defect', 'AUTHENTICATION');
    const timestamp = String(Math.floor(this.now().getTime() / 1000));
    const signature = createHash('md5').update(`appid${appID}formatjsontime${timestamp}${secret}`).digest('hex');
    const query = new URLSearchParams({ appid: appID, format: 'json', time: timestamp, sign: signature });
    const request: ProviderRequest = { url: `https://uaqy.api.storeapi.net/pyi/170/324?${query}`, method: 'GET', headers: { Accept: 'application/json' }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status === 200) return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
      return retry('AVAILABILITY');
    }
    const retryAfter = Object.entries(response.headers).find(([name]) => name.toLowerCase() === 'retry-after')?.[1];
    const delay = parseRetryAfter(Array.isArray(retryAfter) ? retryAfter[0] ?? '' : retryAfter ?? '');
    if (response.status === 429) return retry('RATE_LIMIT', delay);
    if (response.status === 408 || response.status >= 500) return retry('AVAILABILITY', delay);
    if (response.status !== 200) return branch('providerRejected', response.status === 401 ? 'AUTHENTICATION' : response.status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    if (response.body.length > this.maxResponseBytes) return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let data: unknown;
    try { data = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(data) || typeof data.codeid !== 'string' || !data.codeid) return branch('invalidResponse', 'PROTOCOL');
    if (data.codeid === '10021' || data.codeid === '10014') return retry('AVAILABILITY');
    if (data.codeid !== '10000') return branch('providerRejected', 'PROVIDER_REJECTION');
    if (!Array.isArray(data.retdata) || data.retdata.length > 5000) return branch('invalidResponse', 'PROTOCOL');
    const brands: NonNullable<ListVehicleBrandsOutput['brands']> = [];
    for (const item of data.retdata) {
      if (!isRecord(item) || typeof item.ppid !== 'string' || !item.ppid || item.ppid.length > 50 || typeof item.pp_name !== 'string' || !item.pp_name || item.pp_name.length > 256 || typeof item.pp_price !== 'number' || !Number.isSafeInteger(item.pp_price) || item.pp_price < 0) return branch('invalidResponse', 'PROTOCOL');
      if (brands.length < 200) brands.push({ id: item.ppid, name: item.pp_name, priceFen: item.pp_price });
    }
    return newQueryBranch('found', { total: data.retdata.length, brands });
  }
}
/** Read public vehicle brands and quoted maintenance-record prices. */
export class ListVehicleBrandsOperation implements Query<ListVehicleBrandsInput, ListVehicleBrandsOutput> {
  /** Bind one validated client. */ constructor(private readonly client: WapiClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return ListVehicleBrandsDefinition; }
  /** Invoke one safe provider read. */ invoke(call: Call): Promise<QueryAttempt<ListVehicleBrandsOutput>> { return this.client.request(call); }
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'wapi', operation: 'listVehicleBrands', kind, message: 'Wapi brand request failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ListVehicleBrandsOutput> { return newQueryBranch(name, empty, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<ListVehicleBrandsOutput> { return newQueryRetry(failure(kind), delay); }
