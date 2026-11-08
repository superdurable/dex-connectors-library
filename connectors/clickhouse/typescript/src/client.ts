// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListTablesDefinition } from './generated/definitions.js';
import type { Config, Credentials, TablesPage, ListTablesInput } from './generated/models.js';

const listTablesSQL = 'SELECT name FROM system.tables WHERE database = {database:String} ORDER BY name LIMIT 101 FORMAT JSON';
const emptyTables = (): TablesPage => ({ database: '', names: [], hasMore: false });

/** Injectable provider sender for tests and local applications. */
export interface ClickHouseClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface Exchange {
  status: number;
  body: Uint8Array;
  retryAfterMs: number;
  error?: 'credential' | 'request' | 'tooLarge';
}

/** Authenticated ClickHouse HTTP client for table names. */
export class ClickHouseClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate provider configuration without contacting ClickHouse. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: ClickHouseClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('ClickHouse configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl).replace(/\/$/, '');
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 ||
      !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('ClickHouse request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the listTables Query. */ listTables(): ListTablesOperation { return new ListTablesOperation(this); }

  /** Resolve credentials and send one bounded table query. */
  async get(call: Call, database: string): Promise<Exchange> {
    let username: string;
    let password: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      username = credentials.username;
      password = credentials.password instanceof SecretString ? credentials.password.reveal() : '';
    } catch { return emptyExchange('credential'); }
    if (!isHeaderSafeCredential(username) || !isHeaderSafeCredential(password)) return emptyExchange('credential');
    const url = new URL(`${this.baseURL}/`);
    url.searchParams.set('query', listTablesSQL);
    url.searchParams.set('param_database', database);
    const request: ProviderRequest = {
      url: url.toString(), method: 'GET',
      headers: { Accept: 'application/json', 'X-ClickHouse-User': username, 'X-ClickHouse-Key': password },
      maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: this.baseURL.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    let isTooLarge = false;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() };
        isTooLarge = true;
      } else return emptyExchange('request');
    }
    const retryAfterMs = parseRetryAfter(header(response.headers, 'retry-after'));
    if (isTooLarge || response.body.length > this.maxResponseBytes)
      return { status: response.status, body: new Uint8Array(), retryAfterMs, error: 'tooLarge' };
    return { status: response.status, body: response.body, retryAfterMs };
  }
}

/** List up to 100 sorted table names in one database. */
export class ListTablesOperation implements Query<ListTablesInput, TablesPage> {
  /** Bind the provider client. */ constructor(private readonly client: ClickHouseClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return ListTablesDefinition; }
  /** Classify provider outcomes consistently with Go. */
  async invoke(call: Call, input: ListTablesInput): Promise<QueryAttempt<TablesPage>> {
    if (!isValidDatabase(input?.database))
      return terminal('defect', 'VALIDATION', 'database must contain 1–128 printable characters');
    const exchange = await this.client.get(call, input.database);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'ClickHouse credentials are unavailable or invalid');
    if (exchange.error === 'request') return retry('TRANSPORT', 'ClickHouse request failed');
    if (exchange.status === 429) return retry('RATE_LIMIT', 'ClickHouse rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'ClickHouse is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `ClickHouse rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'ClickHouse response exceeded the configured limit');
    const tables = decodeTables(exchange.body, input.database);
    if (!tables) return terminal('invalidResponse', 'PROTOCOL', 'ClickHouse returned invalid table-list JSON');
    return newQueryBranch('listed', tables);
  }
}

function isValidDatabase(value: unknown): value is string {
  return typeof value === 'string' && value.length > 0 && value.trim() === value &&
    new TextEncoder().encode(value).length <= 128 &&
    [...value].every(character => character.codePointAt(0)! >= 0x20 && character.codePointAt(0)! !== 0x7f) &&
    !/(?:[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF])/.test(value);
}
function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'clickhouse', operation: 'listTables', kind, message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<TablesPage> { return newQueryBranch(branch, emptyTables(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<TablesPage> { return newQueryRetry(failure(kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function decodeTables(body: Uint8Array, database: string): TablesPage | undefined {
  let raw: unknown;
  try { raw = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return undefined; }
  if (!isObject(raw) || !Array.isArray(raw.data) || raw.data.length > 101 ||
    !Number.isSafeInteger(raw.rows) || raw.rows !== raw.data.length) return undefined;
  const names: string[] = [];
  for (const row of raw.data) {
    if (!isObject(row) || typeof row.name !== 'string' || !row.name) return undefined;
    if (names.length < 100) names.push(row.name);
  }
  return { database, names, hasMore: raw.data.length > 100 };
}
