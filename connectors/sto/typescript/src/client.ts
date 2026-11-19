// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { QueryWaybillTraceDefinition } from './generated/definitions.js';
import type { Config, Credentials, QueryWaybillTraceInput, WaybillTrace, WaybillEvent } from './generated/models.js';

const emptyOutput: WaybillTrace = { waybillNo: '', events: [] };
/** HTTP transport used for signed STO requests. */
export type StoRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Optional transport dependency for deterministic tests. */
export interface StoClientOptions { /** Production uses the SDK HTTP sender. */ send?: StoRequestSender; }
/** Query STO waybills using credentials resolved per call. */
export class StoClient {
  readonly baseUrl: string;
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly credentials: CredentialProvider<Credentials>;
  readonly send: StoRequestSender;
  /** Validate the provider origin and bounded request settings. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: StoClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('STO configuration and credentials are required');
    this.baseUrl = validateBaseURL(config.baseUrl || 'https://cloudinter-linkgateway.sto.cn/gateway/link.do');
    if (new URL(this.baseUrl).pathname !== '/gateway/link.do') throw new TypeError('STO gateway path must be /gateway/link.do');
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 65_536;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('STO request limits must be positive');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the waybill trace Query. */
  queryWaybillTrace(): QueryWaybillTraceOperation { return new QueryWaybillTraceOperation(this); }
}
/** Fetch a privacy-minimized waybill tracking history. */
export class QueryWaybillTraceOperation implements Query<QueryWaybillTraceInput, WaybillTrace> {
  /** Bind this operation to its client. */ constructor(private readonly client: StoClient) {}
  /** Stable manifest definition. */ definition(): QueryDefinition { return QueryWaybillTraceDefinition; }
  /** Send the signed tracking request and classify its outcome. */
  async invoke(call: Call, input: QueryWaybillTraceInput): Promise<QueryAttempt<WaybillTrace>> {
    if (!validOpaque(input.waybillNo, 128)) return branch('defect', 'VALIDATION');
    let credentials: Credentials;
    try { credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION'); }
    const appKey = credentials.appKey;
    const appSecret = credentials.appSecret instanceof SecretString ? credentials.appSecret.reveal() : '';
    const fromCode = credentials.fromCode;
    if (!validOpaque(appKey, 4096) || !validOpaque(appSecret, 4096) || !validOpaque(fromCode, 4096)) return branch('defect', 'AUTHENTICATION');
    const content = JSON.stringify({ waybillNoList: [input.waybillNo] });
    const digest = createHash('md5').update(content).update(appSecret).digest('base64');
    const form = new URLSearchParams({ api_name: 'STO_TRACE_QUERY_COMMON', content, from_appkey: appKey, from_code: fromCode, to_appkey: 'sto_trace_query', to_code: 'sto_trace_query', data_digest: digest });
    const body = new TextEncoder().encode(form.toString());
    let response: ProviderResponse;
    try {
      response = await this.client.send({ url: this.client.baseUrl, method: 'POST',
        headers: { 'Content-Type': 'application/x-www-form-urlencoded', Accept: 'application/json' },
        body, timeoutMs: this.client.timeoutMs, maxResponseBytes: this.client.maxResponseBytes,
        addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal });
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status === 200) return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
      if (error instanceof BodyTooLargeError && error.status !== undefined) return classifyStatus(error.status, error.headers ?? {});
      return retry('TRANSPORT');
    }
    if (response.status !== 200) return classifyStatus(response.status, response.headers);
    if (response.body.length > this.client.maxResponseBytes) return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let document: unknown;
    try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(document) || typeof document.success !== 'boolean') return branch('invalidResponse', 'PROTOCOL');
    if (!document.success) {
      const transient = document.needRetry === true || (typeof document.errorCode === 'string' && ['S04', 'S06', 'S07', 'S20', 'S30', 'S69', 'S302'].includes(document.errorCode));
      return transient ? retry('AVAILABILITY') : branch('providerRejected', 'PROVIDER_REJECTION');
    }
    if (!isRecord(document.data) || Object.keys(document.data).length > 1 || Object.keys(document.data).some(key => key !== input.waybillNo)) return branch('invalidResponse', 'PROTOCOL');
    const scans: unknown = document.data[input.waybillNo];
    if (!Array.isArray(scans) || scans.length > 1000) return branch('invalidResponse', 'PROTOCOL');
    const events: WaybillEvent[] = [];
    for (const scan of scans as unknown[]) {
      if (!isRecord(scan) || (scan.waybillNo !== undefined && scan.waybillNo !== '' && scan.waybillNo !== input.waybillNo) || typeof scan.scanType !== 'string' || !scan.scanType || typeof scan.opTime !== 'string' || !scan.opTime) return branch('invalidResponse', 'PROTOCOL');
      events.push({ scanType: scan.scanType, opTime: scan.opTime, ...(typeof scan.opOrgName === 'string' && scan.opOrgName ? { opOrgName: scan.opOrgName } : {}) });
    }
    return newQueryBranch('found', { waybillNo: input.waybillNo, events });
  }
}
function validOpaque(value: unknown, limit: number): value is string { return typeof value === 'string' && value.length > 0 && value.length <= limit && /^[!-~]+$/.test(value); }
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function classifyStatus(status: number, headers: ProviderResponse['headers']): QueryAttempt<WaybillTrace> {
  const value = headers['retry-after']; const delay = parseRetryAfter(Array.isArray(value) ? value[0] ?? '' : value ?? '');
  if (status === 429) return retry('RATE_LIMIT', delay);
  if (status === 408 || status >= 500) return retry('AVAILABILITY', delay);
  return branch('providerRejected', 'PROVIDER_REJECTION');
}
function failure(kind: Failure['kind']): Failure { return { provider: 'sto', operation: 'queryWaybillTrace', kind, message: 'STO waybill trace request failed' }; }
function branch(id: string, kind: Failure['kind']): QueryAttempt<WaybillTrace> { return newQueryBranch(id, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delayMs = 0): QueryAttempt<WaybillTrace> { return newQueryRetry(failure(kind), delayMs); }
