// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListDatabasesDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListDatabasesInput, ListDatabasesOutput } from './generated/models.js';

const metadataSQL = 'select name, ntables, status from information_schema.ins_databases;';
/** Optional sender for local provider tests. */
export interface TaosdataClientOptions { /** Sender; omitted uses the SDK HTTP transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>; }

/** Read TDengine Cloud database metadata through one fixed query. */
export class TaosdataClient {
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Validate local limits without contacting the cloud instance. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: TaosdataClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('TDengine Cloud configuration and credentials are required');
    this.timeoutMs = config.timeoutMs || 10_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('TDengine Cloud request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the database-list Query. */ listDatabases(): ListDatabasesOperation { return new ListDatabasesOperation(this); }
  /** Send the fixed metadata query with current connection credentials. */
  async request(call: Call): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number; token: string }> {
    let cloudUrl = '', token = '';
    try { const credentials = await this.credentials.resolve(call, call.context.cancellationSignal); cloudUrl = credentials.cloudUrl instanceof SecretString ? credentials.cloudUrl.reveal() : ''; token = credentials.cloudToken instanceof SecretString ? credentials.cloudToken.reveal() : ''; }
    catch { return { error: 'credential', retryAfterMs: 0, token }; }
    if (!validCloudUrl(cloudUrl) || !token || token.length > 1024 || /[^\x21-\x7e]/.test(token)) return { error: 'credential', retryAfterMs: 0, token: '' };
    const request: ProviderRequest = { url: `${cloudUrl}/rest/sql?token=${encodeURIComponent(token)}`, method: 'POST', headers: { 'Content-Type': 'text/plain' }, body: new TextEncoder().encode(metadataSQL), timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0, token };
      return { error: 'transport', retryAfterMs: 0, token };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')), token };
  }
}

/** List databases without accepting arbitrary SQL. */
export class ListDatabasesOperation implements Query<ListDatabasesInput, ListDatabasesOutput> {
  /** Bind the provider client. */ constructor(private readonly client: TaosdataClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return ListDatabasesDefinition; }
  /** Classify one metadata response using the Go connector contract. */
  async invoke(call: Call): Promise<QueryAttempt<ListDatabasesOutput>> {
    const exchange = await this.client.request(call);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 408 || status === 429 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isObject(wire) || typeof wire.code !== 'number' || !Number.isInteger(wire.code)) return branch('invalidResponse', 'PROTOCOL');
    if (wire.code !== 0) return branch('providerRejected', 'PROVIDER_REJECTION');
    const columns = wire.column_meta;
    if (!Array.isArray(columns) || columns.length !== 3 || !['name', 'ntables', 'status'].every((column, index) => Array.isArray(columns[index]) && columns[index][0] === column) || !Array.isArray(wire.data) || !Number.isInteger(wire.rows) || wire.rows !== wire.data.length) return branch('invalidResponse', 'PROTOCOL');
    const databases: ListDatabasesOutput['databases'] = [];
    for (const row of wire.data) {
      if (!Array.isArray(row) || row.length !== 3 || typeof row[0] !== 'string' || !row[0] || row[0].length > 256 || row[0].includes(exchange.token) || !Number.isSafeInteger(row[1]) || row[1] < 0 || typeof row[2] !== 'string' || !row[2] || row[2].length > 64 || row[2].includes(exchange.token)) return branch('invalidResponse', 'PROTOCOL');
      databases.push({ name: row[0], tableCount: row[1], status: row[2] });
    }
    return newQueryBranch('listed', { databases });
  }
}

function validCloudUrl(value: string): boolean { try { const url = new URL(value); return url.protocol === 'https:' && !url.username && !url.password && url.pathname === '/' && !url.search && !url.hash && !url.port && !value.endsWith('/') && /^[a-z0-9-]+\.(?:cloud\.tdengine\.com|cloud\.taosdata\.com)$/.test(url.hostname); } catch { return false; } }
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'taosdata', operation: 'listDatabases', kind, message: 'TDengine Cloud database query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ListDatabasesOutput> { return newQueryBranch(name, { databases: [] }, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<ListDatabasesOutput> { return newQueryRetry(failure(kind), delay); }
