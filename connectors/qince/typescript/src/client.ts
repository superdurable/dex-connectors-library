// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash, randomBytes } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListDepartmentsDefinition } from './generated/definitions.js';
import type { Config, Credentials, Department, ListDepartmentsInput, ListDepartmentsOutput } from './generated/models.js';

const emptyOutput: ListDepartmentsOutput = { departments: [] };
/** Send one bounded Qince OpenAPI request. */
export type DepartmentRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Optional caller-owned transport, clock, and message ID source. */
export interface QinceClientOptions { send?: DepartmentRequestSender; now?: () => Date; messageID?: () => string }
/** Resolve Qince credentials for each department Query. */
export class QinceClient {
  readonly baseURL: string;
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly credentials: CredentialProvider<Credentials>;
  readonly send: DepartmentRequestSender;
  readonly now: () => Date;
  readonly messageID: () => string;
  /** Validate the official OpenAPI origin and local limits. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: QinceClientOptions = {}) {
    if (!config || !credentials || !config.baseUrl) throw new TypeError('Qince configuration and credentials are required');
    const url = new URL(config.baseUrl);
    if (url.protocol !== 'https:' || !/^openapi(?:\.[a-z0-9-]+)?\.qince\.com$/.test(url.hostname) || url.port || url.pathname !== '/' || url.search || url.hash || url.username || url.password) throw new TypeError('Qince base URL is invalid');
    this.baseURL = url.origin;
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > 16 << 20) throw new TypeError('Qince configuration is invalid');
    this.credentials = credentials; this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date()); this.messageID = options.messageID ?? (() => randomBytes(16).toString('hex'));
  }
  /** Bind the department Query. */ listDepartments(): ListDepartmentsOperation { return new ListDepartmentsOperation(this); }
}
/** Read authorized Qince organization metadata. */
export class ListDepartmentsOperation implements Query<ListDepartmentsInput, ListDepartmentsOutput> {
  /** Bind an authenticated client. */ constructor(private readonly client: QinceClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return ListDepartmentsDefinition; }
  /** Sign and classify one department-list request. */
  async invoke(call: Call, input: ListDepartmentsInput): Promise<QueryAttempt<ListDepartmentsOutput>> {
    if (!input || typeof input !== 'object' || Array.isArray(input)) return branch('defect', 'VALIDATION');
    let credentials: Credentials;
    try { credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION'); }
    const openID = credentials.openId;
    const appKey = credentials.appKey instanceof SecretString ? credentials.appKey.reveal() : '';
    if (typeof openID !== 'string' || !/^[a-zA-Z0-9]{19}$/.test(openID) || !/^[a-zA-Z0-9]{18}$/.test(appKey)) return branch('defect', 'AUTHENTICATION');
    const messageID = this.client.messageID();
    const timestamp = String(this.client.now().getTime());
    const digest = createHash('md5').update(`{}|${appKey}|${timestamp}`).digest('hex');
    const endpoint = `${this.client.baseURL}/api/organization/v1/queryOrganization/${openID}/${timestamp}/${digest}/${messageID}`;
    let response: ProviderResponse;
    try { response = await this.client.send({ url: endpoint, method: 'POST', headers: { Accept: 'application/json', 'Content-Type': 'application/json' }, body: new TextEncoder().encode('{}'), timeoutMs: this.client.timeoutMs, maxResponseBytes: this.client.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal }); }
    catch (error) { if (error instanceof BodyTooLargeError && error.status === 200) return branch('invalidResponse', 'RESPONSE_TOO_LARGE'); if (error instanceof BodyTooLargeError && error.status !== undefined) return classifyStatus(error.status, error.headers ?? {}); return retry('AVAILABILITY'); }
    if (response.status === 429 || response.status === 408 || response.status >= 500) return classifyStatus(response.status, response.headers);
    if (response.body.byteLength > this.client.maxResponseBytes) return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (response.status !== 200) return branch('providerRejected', response.status === 401 ? 'AUTHENTICATION' : response.status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(wire) || wire.msg_id !== messageID || (typeof wire.return_code !== 'string' && !Number.isInteger(wire.return_code))) return branch('invalidResponse', 'PROTOCOL');
    if (String(wire.return_code) !== '0') return branch('providerRejected', 'PROVIDER_REJECTION');
    let records: unknown = wire.response_data;
    if (typeof records === 'string') { try { records = JSON.parse(records) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); } }
    if (!Array.isArray(records) || records.length > 10_000) return branch('invalidResponse', 'PROTOCOL');
    const departments: Department[] = [];
    for (const item of records) {
      if (!isRecord(item) || !safeText(item.id, 32) || !safeText(item.org_code, 100) || !safeText(item.org_name, 100) || (item.org_status !== '0' && item.org_status !== '1')) return branch('invalidResponse', 'PROTOCOL');
      departments.push({ id: item.id, code: item.org_code, name: item.org_name, active: item.org_status === '1' });
    }
    return newQueryBranch('listed', { departments });
  }
}
function safeText(value: unknown, maximum: number): value is string { return typeof value === 'string' && value.length > 0 && [...value].length <= maximum && value.trim() === value && !value.includes('\r') && !value.includes('\n') && !value.includes('\x00'); }
function classifyStatus(status: number, headers: ProviderResponse['headers']): QueryAttempt<ListDepartmentsOutput> { const value = headers['retry-after']; const delay = parseRetryAfter(Array.isArray(value) ? value[0] ?? '' : value ?? ''); if (status === 429) return retry('RATE_LIMIT', delay); if (status === 408 || status >= 500) return retry('AVAILABILITY', delay); return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION'); }
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'qince', operation: 'listDepartments', kind, message: 'Qince department query failed' }; }
function branch(id: string, kind: Failure['kind']): QueryAttempt<ListDepartmentsOutput> { return newQueryBranch(id, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delayMs = 0): QueryAttempt<ListDepartmentsOutput> { return newQueryRetry(failure(kind), delayMs); }
