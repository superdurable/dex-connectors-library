// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHmac, randomInt } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListAgentsDefinition } from './generated/definitions.js';
import type { AgentSummary, Config, Credentials, ListAgentsInput, ListAgentsOutput } from './generated/models.js';

const emptyOutput: ListAgentsOutput = { agents: [], page: 0, total: 0, totalPages: 0 };
/** Send one bounded Useasy HTTP request. */
export type AgentRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Optional caller-owned transport, clock, and nonce source. */
export interface UseasyClientOptions { send?: AgentRequestSender; now?: () => Date; nonce?: () => string }
/** Resolve signed Useasy credentials for each Query attempt. */
export class UseasyClient {
  readonly endpoint: string;
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly credentials: CredentialProvider<Credentials>;
  readonly send: AgentRequestSender;
  readonly now: () => Date;
  readonly nonce: () => string;
  /** Validate the documented HTTPS origin and local limits. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: UseasyClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Useasy configuration and credentials are required');
    const url = new URL(config.baseUrl ?? 'https://openapi.useasy.cn');
    if (url.protocol !== 'https:' || url.hostname !== 'openapi.useasy.cn' || url.port || url.pathname !== '/' || url.search || url.hash || url.username || url.password) throw new TypeError('Useasy base URL is invalid');
    this.endpoint = `${url.origin}/openapi/platform/v1/agent/list`;
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > 16 << 20) throw new TypeError('Useasy configuration is invalid');
    this.credentials = credentials; this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date()); this.nonce = options.nonce ?? (() => String(randomInt(100_000, 1_000_000)));
  }
  /** Bind the listAgents Query. */ listAgents(): ListAgentsOperation { return new ListAgentsOperation(this); }
}
/** Read one page of agent metadata. */
export class ListAgentsOperation implements Query<ListAgentsInput, ListAgentsOutput> {
  /** Bind an authenticated client. */ constructor(private readonly client: UseasyClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return ListAgentsDefinition; }
  /** Sign and classify one provider response. */
  async invoke(call: Call, input: ListAgentsInput): Promise<QueryAttempt<ListAgentsOutput>> {
    let credentials: Credentials;
    try { credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION'); }
    const appID = credentials.appId instanceof SecretString ? credentials.appId.reveal() : '';
    const secretKey = credentials.secretKey instanceof SecretString ? credentials.secretKey.reveal() : '';
    if (!/^\d{1,32}$/.test(appID) || !secretKey || secretKey.length > 512 || !/^[\x21-\x7e]+$/.test(secretKey)) return branch('defect', 'AUTHENTICATION');
    const page = input?.page || 1; const pageSize = input?.pageSize || 20;
    if (!Number.isSafeInteger(page) || page < 1 || page > 50_000 || !Number.isSafeInteger(pageSize) || pageSize < 1 || pageSize > 100) return branch('defect', 'VALIDATION');
    const nonce = this.client.nonce(); const timestamp = String(Math.floor(this.client.now().getTime() / 1000));
    const signature = createHmac('sha256', secretKey).update(appID + timestamp + nonce).digest('base64');
    const requestBody = JSON.stringify({ page, pageSize });
    let response: ProviderResponse;
    try { response = await this.client.send({ url: this.client.endpoint, method: 'POST', headers: { 'Content-Type': 'application/json;charset=utf-8', Accept: 'application/json', Appid: appID, Nonce: nonce, Timestamp: timestamp, Signature: signature }, body: new TextEncoder().encode(requestBody), timeoutMs: this.client.timeoutMs, maxResponseBytes: this.client.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal }); }
    catch (error) { if (error instanceof BodyTooLargeError && error.status === 200) return branch('invalidResponse', 'RESPONSE_TOO_LARGE'); if (error instanceof BodyTooLargeError && error.status !== undefined) return classifyStatus(error.status, error.headers ?? {}); return retry('AVAILABILITY'); }
    if (response.status === 429 || response.status === 408 || response.status >= 500) return classifyStatus(response.status, response.headers);
    if (response.body.byteLength > this.client.maxResponseBytes) return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (response.status !== 200) return branch('providerRejected', response.status === 401 ? 'AUTHENTICATION' : response.status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(wire) || typeof wire.code !== 'string' || !wire.code) return branch('invalidResponse', 'PROTOCOL');
    if (wire.success !== true || wire.code !== '200') return branch('providerRejected', 'PROVIDER_REJECTION');
    if (!isRecord(wire.data) || !Array.isArray(wire.data.list) || !isRecord(wire.data.page)) return branch('invalidResponse', 'PROTOCOL');
    const pagination = wire.data.page;
    if (pagination.pageNumber !== page || !Number.isSafeInteger(pagination.pageSize) || (pagination.pageSize as number) <= 0 || !Number.isSafeInteger(pagination.total) || (pagination.total as number) < 0 || !Number.isSafeInteger(pagination.totalPage) || (pagination.totalPage as number) < 0 || wire.data.list.length > pageSize) return branch('invalidResponse', 'PROTOCOL');
    const agents: AgentSummary[] = [];
    for (const item of wire.data.list) {
      if (!isRecord(item) || typeof item._id !== 'string' || !item._id || item._id.length > 128 || typeof item.agentNumber !== 'string' || !item.agentNumber || item.agentNumber.length > 128 || (item.status !== 0 && item.status !== 1)) return branch('invalidResponse', 'PROTOCOL');
      agents.push({ id: item._id, number: item.agentNumber, enabled: item.status === 1 });
    }
    return newQueryBranch('listed', { agents, page, total: pagination.total as number, totalPages: pagination.totalPage as number });
  }
}
function classifyStatus(status: number, headers: ProviderResponse['headers']): QueryAttempt<ListAgentsOutput> { const value = headers['retry-after']; const delay = parseRetryAfter(Array.isArray(value) ? value[0] ?? '' : value ?? ''); if (status === 429) return retry('RATE_LIMIT', delay); if (status === 408 || status >= 500) return retry('AVAILABILITY', delay); return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION'); }
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'useasy', operation: 'listAgents', kind, message: 'Useasy agent query failed' }; }
function branch(id: string, kind: Failure['kind']): QueryAttempt<ListAgentsOutput> { return newQueryBranch(id, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delayMs = 0): QueryAttempt<ListAgentsOutput> { return newQueryRetry(failure(kind), delayMs); }
