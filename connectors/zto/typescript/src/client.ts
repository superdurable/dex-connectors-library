// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { QueryWaybillTraceDefinition } from './generated/definitions.js';
import type { Config, Credentials, QueryWaybillTraceInput, WaybillTrace, WaybillEvent } from './generated/models.js';

const emptyOutput: WaybillTrace = { billCode: '', events: [] };
/** HTTP transport used for signed ZTO requests. */
export type ZtoRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Optional transport dependency for deterministic tests. */
export interface ZtoClientOptions { /** Production uses the SDK HTTP sender. */ send?: ZtoRequestSender; }
/** Query ZTO waybills using credentials resolved per call. */
export class ZtoClient {
  readonly baseUrl: string;
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly credentials: CredentialProvider<Credentials>;
  readonly send: ZtoRequestSender;
  /** Validate the provider origin and bounded request settings. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: ZtoClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('ZTO configuration and credentials are required');
    this.baseUrl = validateBaseURL(config.baseUrl || 'https://japi.zto.com').replace(/\/$/, '');
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 65_536;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('ZTO request limits must be positive');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the waybill trace Query. */
  queryWaybillTrace(): QueryWaybillTraceOperation { return new QueryWaybillTraceOperation(this); }
}
/** Fetch a privacy-minimized waybill tracking history. */
export class QueryWaybillTraceOperation implements Query<QueryWaybillTraceInput, WaybillTrace> {
  /** Bind this operation to its client. */ constructor(private readonly client: ZtoClient) {}
  /** Stable manifest definition. */ definition(): QueryDefinition { return QueryWaybillTraceDefinition; }
  /** Send the signed tracking request and classify its outcome. */
  async invoke(call: Call, input: QueryWaybillTraceInput): Promise<QueryAttempt<WaybillTrace>> {
    if (!validOpaque(input.billCode, 128)) return branch('defect', 'VALIDATION');
    let credentials: Credentials;
    try { credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION'); }
    const appKey = credentials.appKey;
    const appSecret = credentials.appSecret instanceof SecretString ? credentials.appSecret.reveal() : '';
    if (!validOpaque(appKey, 4096) || !validOpaque(appSecret, 4096)) return branch('defect', 'AUTHENTICATION');
    const body = new TextEncoder().encode(JSON.stringify({ billCode: input.billCode }));
    const digest = createHash('md5').update(body).update(appSecret).digest('base64');
    let response: ProviderResponse;
    try {
      response = await this.client.send({ url: this.client.baseUrl + '/zto.merchant.waybill.track.query', method: 'POST',
        headers: { 'Content-Type': 'application/json', Accept: 'application/json', 'x-appKey': appKey, 'x-dataDigest': digest },
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
    if (!isRecord(document) || typeof document.status !== 'boolean' || typeof document.statusCode !== 'string' || !document.statusCode) return branch('invalidResponse', 'PROTOCOL');
    if (!document.status) return document.statusCode === 'E500' ? retry('AVAILABILITY') : branch('providerRejected', 'PROVIDER_REJECTION');
    if (document.statusCode !== 'SYS000' || !Array.isArray(document.result) || document.result.length > 1000) return branch('invalidResponse', 'PROTOCOL');
    const events: WaybillEvent[] = [];
    for (const scan of document.result as unknown[]) {
      if (!isRecord(scan) || (scan.billCode !== undefined && scan.billCode !== '' && scan.billCode !== input.billCode) || typeof scan.scanType !== 'string' || !scan.scanType || !Number.isSafeInteger(scan.scanDate) || (scan.scanDate as number) <= 0) return branch('invalidResponse', 'PROTOCOL');
      const event: WaybillEvent = { scanType: scan.scanType, scanDate: scan.scanDate as number };
      if (scan.scanSite !== undefined && scan.scanSite !== null) {
        if (!isRecord(scan.scanSite) || typeof scan.scanSite.name !== 'string') return branch('invalidResponse', 'PROTOCOL');
        event.scanSiteName = scan.scanSite.name;
      }
      events.push(event);
    }
    return newQueryBranch('found', { billCode: input.billCode, events });
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
function failure(kind: Failure['kind']): Failure { return { provider: 'zto', operation: 'queryWaybillTrace', kind, message: 'ZTO waybill trace request failed' }; }
function branch(id: string, kind: Failure['kind']): QueryAttempt<WaybillTrace> { return newQueryBranch(id, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delayMs = 0): QueryAttempt<WaybillTrace> { return newQueryRetry(failure(kind), delayMs); }
