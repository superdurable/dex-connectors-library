// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash, randomUUID } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListContractsDefinition } from './generated/definitions.js';
import type { Config, ContractSummary, Credentials, ListContractsInput, ListContractsOutput } from './generated/models.js';

const emptyOutput: ListContractsOutput = { contracts: [], totalCount: 0, nextOffset: 0 };
const allowedStatus = new Set(['DRAFT', 'RECALLED', 'SIGNING', 'REJECTED', 'COMPLETE', 'EXPIRED', 'FILLING', 'INVALIDING', 'INVALIDED', 'FORCE_END']);
/** Send one bounded Qiyuesuo HTTP request. */
export type ContractRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Optional caller-owned transport, clock, and nonce source. */
export interface QiyuesuoClientOptions { send?: ContractRequestSender; now?: () => Date; nonce?: () => string }
/** Resolve Qiyuesuo application credentials per Query attempt. */
export class QiyuesuoClient {
  readonly baseURL: string;
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly credentials: CredentialProvider<Credentials>;
  readonly send: ContractRequestSender;
  readonly now: () => Date;
  readonly nonce: () => string;
  /** Validate the official HTTPS origin and local limits. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: QiyuesuoClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Qiyuesuo configuration and credentials are required');
    const url = new URL(config.baseUrl ?? 'https://openapi.qiyuesuo.com');
    if (url.protocol !== 'https:' || !['openapi.qiyuesuo.com', 'openapi.qiyuesuo.cn'].includes(url.hostname) || url.port || url.pathname !== '/' || url.search || url.hash || url.username || url.password) throw new TypeError('Qiyuesuo base URL is invalid');
    this.baseURL = url.origin;
    this.timeoutMs = config.timeoutMs ?? 10_000; this.maxResponseBytes = config.maxResponseBytes ?? 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > 16 << 20) throw new TypeError('Qiyuesuo configuration is invalid');
    this.credentials = credentials; this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date()); this.nonce = options.nonce ?? randomUUID;
  }
  /** Bind the contract-status Query. */ listContracts(): ListContractsOperation { return new ListContractsOperation(this); }
}
/** Read one bounded window of contract statuses. */
export class ListContractsOperation implements Query<ListContractsInput, ListContractsOutput> {
  /** Bind an authenticated client. */ constructor(private readonly client: QiyuesuoClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return ListContractsDefinition; }
  /** Sign and classify a contract-list response. */
  async invoke(call: Call, input: ListContractsInput): Promise<QueryAttempt<ListContractsOutput>> {
    let credentials: Credentials;
    try { credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION'); }
    const token = credentials.appToken instanceof SecretString ? credentials.appToken.reveal() : '';
    const secret = credentials.appSecret instanceof SecretString ? credentials.appSecret.reveal() : '';
    if (![token, secret].every(validCredential)) return branch('defect', 'AUTHENTICATION');
    const limit = input?.limit || 20;
    if (!input || !Number.isSafeInteger(input.offset) || input.offset < 0 || input.offset > 1_000_000 || !Number.isSafeInteger(limit) || limit < 1 || limit > 100) return branch('defect', 'VALIDATION');
    const nonce = this.client.nonce(); const timestamp = String(this.client.now().getTime());
    const signature = createHash('md5').update(token + secret + timestamp + nonce).digest('hex');
    const query = new URLSearchParams({ selectLimit: String(limit), selectOffset: String(input.offset) });
    let response: ProviderResponse;
    try { response = await this.client.send({ url: `${this.client.baseURL}/v2/contract/list?${query}`, method: 'GET', headers: { Accept: 'application/json', 'x-qys-open-accesstoken': token, 'x-qys-open-timestamp': timestamp, 'x-qys-open-nonce': nonce, 'x-qys-open-signature': signature }, timeoutMs: this.client.timeoutMs, maxResponseBytes: this.client.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal }); }
    catch (error) { if (error instanceof BodyTooLargeError && error.status === 200) return branch('invalidResponse', 'RESPONSE_TOO_LARGE'); if (error instanceof BodyTooLargeError && error.status !== undefined) return classifyStatus(error.status, error.headers ?? {}); return retry('AVAILABILITY'); }
    if (response.status === 429 || response.status === 408 || response.status >= 500) return classifyStatus(response.status, response.headers);
    if (response.body.byteLength > this.client.maxResponseBytes) return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (response.status !== 200) return branch('providerRejected', [401, 440, 441, 442, 446].includes(response.status) ? 'AUTHENTICATION' : [403, 449].includes(response.status) ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    let wire: unknown;
    try { const raw = new TextDecoder('utf-8', { fatal: true }).decode(response.body); wire = JSON.parse(raw.replace(/("id"\s*:\s*)(-?\d+)/g, '$1"$2"')) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(wire) || typeof wire.responseCode !== 'string' || !wire.responseCode) return branch('invalidResponse', 'PROTOCOL');
    if (wire.responseCode === '11990006') return retry('RATE_LIMIT');
    if (wire.responseCode === '21990001') return retry('AVAILABILITY');
    if (wire.responseCode !== '00000000') return branch('providerRejected', ['11990009', '11990440', '11990441', '11990442', '11990446', '11990448'].includes(wire.responseCode) ? 'AUTHENTICATION' : ['14990002', '14990021', '11990403', '11990449'].includes(wire.responseCode) ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    if (!isRecord(wire.result) || !Array.isArray(wire.result.list) || !Number.isSafeInteger(wire.result.totalCount) || (wire.result.totalCount as number) < input.offset + wire.result.list.length || wire.result.list.length > limit) return branch('invalidResponse', 'PROTOCOL');
    const contracts: ContractSummary[] = [];
    for (const item of wire.result.list) {
      if (!isRecord(item) || typeof item.id !== 'string' || !/^\d+$/.test(item.id) || typeof item.status !== 'string' || !allowedStatus.has(item.status)) return branch('invalidResponse', 'PROTOCOL');
      const id = BigInt(item.id);
      if (id <= 0n || id > 9_223_372_036_854_775_807n) return branch('invalidResponse', 'PROTOCOL');
      contracts.push({ id: id.toString(), status: item.status });
    }
    const totalCount = wire.result.totalCount as number;
    let nextOffset = 0;
    if (input.offset + contracts.length < totalCount) { if (contracts.length === 0 || input.offset + contracts.length > 1_000_000) return branch('invalidResponse', 'PROTOCOL'); nextOffset = input.offset + contracts.length; }
    return newQueryBranch('listed', { contracts, totalCount, nextOffset });
  }
}
function validCredential(value: string): boolean { return value.length > 0 && value.length <= 512 && /^[\x21-\x7e]+$/.test(value); }
function classifyStatus(status: number, headers: ProviderResponse['headers']): QueryAttempt<ListContractsOutput> { const value = headers['retry-after']; const delay = parseRetryAfter(Array.isArray(value) ? value[0] ?? '' : value ?? ''); if (status === 429) return retry('RATE_LIMIT', delay); if (status === 408 || status >= 500) return retry('AVAILABILITY', delay); return branch('providerRejected', [401, 440, 441, 442, 446].includes(status) ? 'AUTHENTICATION' : [403, 449].includes(status) ? 'AUTHORIZATION' : 'PROVIDER_REJECTION'); }
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'qiyuesuo', operation: 'listContracts', kind, message: 'Qiyuesuo contract query failed' }; }
function branch(id: string, kind: Failure['kind']): QueryAttempt<ListContractsOutput> { return newQueryBranch(id, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delayMs = 0): QueryAttempt<ListContractsOutput> { return newQueryRetry(failure(kind), delayMs); }
