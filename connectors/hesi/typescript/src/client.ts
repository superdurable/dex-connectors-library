// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { integerCode, parseBalanceResponse } from './provider-json.js';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListDepartmentsDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListDepartmentsInput, ListDepartmentsOutput } from './generated/models.js';

const allowedHosts = new Set(['app.ekuaibao.com', 'dd2.hosecloud.com', 'wx2.ekuaibao.com', 'kdcloud2.ekuaibao.com', 'ddenterprise.ekuaibao.com', 'feishu.ekuaibao.com']);
const emptyOutput: ListDepartmentsOutput = { departments: [], total: 0, nextStart: 0 };

/** Send one Hesi request through a caller-owned transport. */
export type DepartmentRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;

/** Optional transport dependency for provider tests. */
export interface HesiClientOptions {
  /** Request sender; production uses the Connector SDK transport. */
  send?: DepartmentRequestSender;
}

/** Resolve Hesi credentials and own bounded HTTP settings. */
export class HesiClient {
  readonly baseUrl: string;
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly credentials: CredentialProvider<Credentials>;
  readonly send: DepartmentRequestSender;

  /** Validate the documented HTTPS platform origin without making a request. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: HesiClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Hesi configuration and credential source are required');
    let parsed: URL;
    try { parsed = new URL(config.baseUrl ?? 'https://app.ekuaibao.com'); } catch { throw new TypeError('Hesi base URL is invalid'); }
    if (parsed.protocol !== 'https:' || !allowedHosts.has(parsed.hostname) || parsed.port || (parsed.pathname !== '/' && parsed.pathname !== '') || parsed.search || parsed.hash || parsed.username || parsed.password) throw new TypeError('Hesi base URL must be a documented HTTPS platform origin');
    this.baseUrl = parsed.origin;
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('Hesi timeout or response limit is invalid');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the bounded department Query. */
  listDepartments(): ListDepartmentsOperation { return new ListDepartmentsOperation(this); }
}

/** Exchange credentials for an access token, then list one department page. */
export class ListDepartmentsOperation implements Query<ListDepartmentsInput, ListDepartmentsOutput> {
  /** Bind an authenticated Hesi client. */
  constructor(private readonly client: HesiClient) {}

  /** Return the stable generated Query contract. */
  definition(): QueryDefinition { return ListDepartmentsDefinition; }

  /** Keep the access token out of Flow output, receipts, and failures. */
  async invoke(call: Call, input: ListDepartmentsInput): Promise<QueryAttempt<ListDepartmentsOutput>> {
    let credentials: Credentials;
    try { credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION', 'Hesi credentials are unavailable or invalid'); }
    const appKey = credentials.appKey instanceof SecretString ? credentials.appKey.reveal() : '';
    const appSecurity = credentials.appSecurity instanceof SecretString ? credentials.appSecurity.reveal() : '';
    if (!isValidCredential(appKey) || !isValidCredential(appSecurity)) return branch('defect', 'AUTHENTICATION', 'Hesi credentials are unavailable or invalid');
    if (!input || !Number.isSafeInteger(input.start) || !Number.isSafeInteger(input.count)) return branch('defect', 'VALIDATION', 'start or count is outside the supported range');
    const count = input.count || 20;
    if (input.start < 0 || input.start > 1_000_000 || count < 1 || count > 100) return branch('defect', 'VALIDATION', 'start or count is outside the supported range');
    const authResult = await this.sendStage(call, { url: `${this.client.baseUrl}/api/openapi/v1/auth/getAccessToken`, method: 'POST', headers: { Accept: 'application/json', 'Content-Type': 'application/json' }, body: new TextEncoder().encode(JSON.stringify({ appKey, appSecurity })), timeoutMs: this.client.timeoutMs, maxResponseBytes: this.client.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal }, 'authorization');
    if (authResult.attempt) return authResult.attempt;
    let token: unknown;
    try { const document = parseJSON(authResult.body); token = isRecord(document) && isRecord(document.value) ? document.value.accesstoken : undefined; }
    catch { return branch('invalidResponse', 'PROTOCOL', 'Hesi returned invalid authorization data'); }
    if (typeof token !== 'string' || !isValidCredential(token)) return branch('invalidResponse', 'PROTOCOL', 'Hesi returned invalid authorization data');
    const url = new URL(`${this.client.baseUrl}/api/openapi/v1/departments`);
    url.searchParams.set('accessToken', token);
    url.searchParams.set('start', String(input.start));
    url.searchParams.set('count', String(count));
    const departmentResult = await this.sendStage(call, { url: url.toString(), method: 'GET', headers: { Accept: 'application/json' }, timeoutMs: this.client.timeoutMs, maxResponseBytes: this.client.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal }, 'department');
    if (departmentResult.attempt) return departmentResult.attempt;
    try {
      const document = parseJSON(departmentResult.body);
      if (!isRecord(document) || !Array.isArray(document.items) || document.items.length > count) throw new TypeError('invalid page');
      const reported = document.count == null ? 0n : integerCode(document.count);
      if (reported === undefined || reported < 0n || reported > BigInt(Number.MAX_SAFE_INTEGER)) throw new TypeError('invalid count');
      const departments: NonNullable<ListDepartmentsOutput['departments']> = [];
      for (const item of document.items) {
        if (!isRecord(item) || typeof item.id !== 'string' || !item.id || Buffer.byteLength(item.id) > 256 || typeof item.name !== 'string' || !item.name || Buffer.byteLength(item.name) > 512 || (item.code != null && (typeof item.code !== 'string' || Buffer.byteLength(item.code) > 256)) || typeof item.active !== 'boolean') throw new TypeError('invalid department');
        departments.push({ id: item.id, name: item.name, code: item.code ?? '', active: item.active });
      }
      const total = Number(reported);
      let nextStart = 0;
      if (input.start + departments.length < total) {
        if (departments.length === 0 || input.start + departments.length > 1_000_000) throw new TypeError('pagination cannot advance');
        nextStart = input.start + departments.length;
      }
      return newQueryBranch('listed', { departments, total, nextStart });
    } catch { return branch('invalidResponse', 'PROTOCOL', 'Hesi returned invalid department data'); }
  }

  private async sendStage(call: Call, request: ProviderRequest, stage: string): Promise<{ body?: Uint8Array; attempt?: QueryAttempt<ListDepartmentsOutput> }> {
    try {
      const response = await this.client.send(request);
      if (response.status === 429) return { attempt: retry('RATE_LIMIT', `Hesi ${stage} request rate limited`, retryAfter(response.headers)) };
      if (response.status === 408 || response.status >= 500) return { attempt: retry('AVAILABILITY', `Hesi ${stage} temporarily unavailable`, retryAfter(response.headers)) };
      if (response.body.byteLength > this.client.maxResponseBytes) return { attempt: branch('invalidResponse', 'RESPONSE_TOO_LARGE', 'Hesi response exceeded the configured limit') };
      if (response.status !== 200) return { attempt: branch('providerRejected', response.status === 401 || response.status === 403 ? 'AUTHENTICATION' : 'PROVIDER_REJECTION', `Hesi rejected ${stage} with HTTP ${response.status}`) };
      return { body: response.body };
    } catch (error) {
      if (error instanceof BodyTooLargeError) {
        if (error.status === 429) return { attempt: retry('RATE_LIMIT', `Hesi ${stage} request rate limited`, retryAfter(error.headers ?? {})) };
        if (error.status === 408 || (error.status ?? 0) >= 500) return { attempt: retry('AVAILABILITY', `Hesi ${stage} temporarily unavailable`, retryAfter(error.headers ?? {})) };
        return { attempt: branch('invalidResponse', 'RESPONSE_TOO_LARGE', `Hesi ${stage} response exceeded the configured limit`) };
      }
      return { attempt: retry('AVAILABILITY', `Hesi ${stage} transport failed`) };
    }
  }
}

function parseJSON(body: Uint8Array | undefined): unknown { if (!body) throw new TypeError('missing body'); return parseBalanceResponse(body); }
function retryAfter(headers: ProviderResponse['headers']): number { const value = Object.entries(headers).find(([name]) => name.toLowerCase() === 'retry-after')?.[1]; return parseRetryAfter(Array.isArray(value) ? value[0] ?? '' : value ?? ''); }
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function isValidCredential(value: string): boolean { return value.length > 0 && value.length <= 2048 && /^[!-~]+$/.test(value); }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'hesi', operation: 'listDepartments', kind, message }; }
function branch(id: string, kind: Failure['kind'], message: string): QueryAttempt<ListDepartmentsOutput> { return newQueryBranch(id, emptyOutput, {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delayMs = 0): QueryAttempt<ListDepartmentsOutput> { return newQueryRetry(failure(kind, message), delayMs); }
