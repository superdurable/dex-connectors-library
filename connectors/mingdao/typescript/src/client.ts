// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListWorksheetsDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListWorksheetsInput, ListWorksheetsOutput } from './generated/models.js';

const endpoint = 'https://api.mingdao.com/v3/app/worksheets/list';
const emptyOutput: ListWorksheetsOutput = { worksheets: [] };
/** Optional HTTP sender for deterministic provider tests. */
export interface MingdaoClientOptions { /** Sender; defaults to the Connector SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>; }
/** Read the worksheet metadata with private credentials resolved per call. */
export class MingdaoClient {
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Validate local settings without contacting Mingdao. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: MingdaoClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Mingdao configuration and credential source are required');
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('Mingdao request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the read-only worksheet-list Query. */ listWorksheets(): ListWorksheetsOperation { return new ListWorksheetsOperation(this); }
  /** Dispatch one bounded provider request. */
  async request(call: Call): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    const appKey = credentials.appKey instanceof SecretString ? credentials.appKey.reveal() : '';
    const sign = credentials.sign instanceof SecretString ? credentials.sign.reveal() : '';
    if (!isValidSecret(appKey) || !isValidSecret(sign)) return { error: 'credential', retryAfterMs: 0 };
    const request: ProviderRequest = { url: endpoint, method: 'POST', headers: { Accept: 'application/json', 'Content-Type': 'application/json', 'Hap-Appkey': appKey, 'Hap-Sign': sign }, body: new TextEncoder().encode('{"responseFormat":"json"}'), timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    const retryAfterValue = Object.entries(response.headers).find(([key]) => key.toLowerCase() === 'retry-after')?.[1];
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(Array.isArray(retryAfterValue) ? retryAfterValue[0] ?? '' : retryAfterValue ?? '') };
  }
}
/** Classify Mingdao worksheet-list results without retaining secrets or metadata. */
export class ListWorksheetsOperation implements Query<ListWorksheetsInput, ListWorksheetsOutput> {
  /** Bind one validated client. */ constructor(private readonly client: MingdaoClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return ListWorksheetsDefinition; }
  /** Read and validate the worksheet metadata. */
  async invoke(call: Call, _input: ListWorksheetsInput): Promise<QueryAttempt<ListWorksheetsOutput>> {
    void _input;
    const exchange = await this.client.request(call);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    let document: unknown;
    try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(document) || typeof document.success !== 'boolean') return branch('invalidResponse', 'PROTOCOL');
    if (!document.success) {
      if (document.error_code === 51 || document.error_code === 90000) return retry('RATE_LIMIT');
      if (document.error_code === 99999) return retry('AVAILABILITY');
      return branch('providerRejected', 'PROVIDER_REJECTION');
    }
    if ((document.error_code !== undefined && document.error_code !== 1) || !Array.isArray(document.data)) return branch('invalidResponse', 'PROTOCOL');
    const worksheets: Array<{ id: string; name: string }> = [];
    for (const worksheet of document.data) {
      if (!isRecord(worksheet) || typeof worksheet.id !== 'string' || !worksheet.id || typeof worksheet.name !== 'string' || !worksheet.name) return branch('invalidResponse', 'PROTOCOL');
      worksheets.push({ id: worksheet.id, name: worksheet.name });
    }
    return newQueryBranch('listed', { worksheets });
  }
}
function isValidSecret(value: string): boolean { return value.length > 0 && value.length <= 1024 && /^[!-~]+$/.test(value); }
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'mingdao', operation: 'listWorksheets', kind, message: 'Mingdao provider request failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ListWorksheetsOutput> { return newQueryBranch(name, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<ListWorksheetsOutput> { return newQueryRetry(failure(kind), delay); }
