// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHmac } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { DescribeZonesDefinition } from './generated/definitions.js';
import type { Config, Credentials, DescribeZonesInput, ZonesPage, Zone } from './generated/models.js';

const empty: ZonesPage = { zones: [], totalCount: 0 };
/** Provider transport and timestamp dependencies for deterministic testing. */
export interface QingcloudClientOptions {
  /** Dispatch the provider request; production uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Supply signing time; production uses the current clock. */ now?: () => Date;
}
/** List accessible QingCloud zones through a signed read-only Query. */
export class QingcloudClient {
  readonly baseUrl: string;
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  readonly now: () => Date;
  /** Validate connection settings before any provider request. */
  constructor(config: Config, readonly credentials: CredentialProvider<Credentials>, options: QingcloudClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('QingCloud configuration and credential source are required');
    const url = new URL(config.baseUrl ?? 'https://api.qingcloud.com');
    if (url.protocol !== 'https:' || url.username || url.password || url.search || url.hash || url.pathname !== '/') throw new TypeError('QingCloud API root is invalid');
    this.baseUrl = url.origin;
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 65_536;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > 16 << 20) throw new TypeError('QingCloud limits are invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }
  /** Bind the zone listing Query. */ describeZones(): DescribeZonesOperation { return new DescribeZonesOperation(this); }
}
/** Classify zone listing responses and preserve only zone metadata. */
export class DescribeZonesOperation implements Query<DescribeZonesInput, ZonesPage> {
  /** Bind one validated client. */ constructor(private readonly client: QingcloudClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return DescribeZonesDefinition; }
  /** Read and validate one bounded provider response. */
  async invoke(call: Call, input: DescribeZonesInput): Promise<QueryAttempt<ZonesPage>> {
    void input;
    let credentials: Credentials;
    try { credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal); } catch { return branch('defect', 'AUTHENTICATION'); }
    const key = credentials.accessKeyId;
    const secret = credentials.secretAccessKey instanceof SecretString ? credentials.secretAccessKey.reveal() : '';
    if (typeof key !== 'string' || !key || key.length > 128 || !/^[!-~]+$/.test(key) || !secret) return branch('defect', 'AUTHENTICATION');
    const timestamp = this.client.now().toISOString().slice(0, 19) + 'Z';
    const url = new URL('/iaas/', this.client.baseUrl);
    const values = { access_key_id: key, action: 'DescribeZones', limit: '100', signature_method: 'HmacSHA256', signature_version: '1', time_stamp: timestamp, version: '1' };
    const canonical = new URLSearchParams(Object.entries(values).sort(([a], [b]) => a.localeCompare(b))).toString();
    const signature = createHmac('sha256', secret).update(`GET\n/iaas/\n${canonical}`).digest('base64');
    for (const [name, value] of Object.entries(values)) url.searchParams.set(name, value);
    url.searchParams.set('signature', signature);
    const request: ProviderRequest = { url: url.toString(), method: 'GET', headers: { Accept: 'application/json' }, timeoutMs: this.client.timeoutMs, maxResponseBytes: this.client.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.client.send(request); } catch (error) { return error instanceof BodyTooLargeError && error.status !== undefined ? branch('invalidResponse', 'RESPONSE_TOO_LARGE') : retry('TRANSPORT'); }
    const header = Object.entries(response.headers).find(([name]) => name.toLowerCase() === 'retry-after')?.[1];
    const delay = parseRetryAfter(Array.isArray(header) ? header[0] ?? '' : header ?? '');
    if (response.status === 429) return retry('RATE_LIMIT', delay);
    if (response.status === 408 || response.status >= 500) return retry('AVAILABILITY', delay);
    if (response.body.length > this.client.maxResponseBytes) return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (response.status !== 200) return branch('providerRejected', 'PROVIDER_REJECTION');
    let document: unknown;
    try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(document) || !Number.isSafeInteger(document.ret_code)) return branch('invalidResponse', 'PROTOCOL');
    if ([5000, 5100, 5200, 5300].includes(document.ret_code as number)) return retry('AVAILABILITY', delay);
    if (document.ret_code !== 0) return branch('providerRejected', 'PROVIDER_REJECTION');
    if (document.action !== 'DescribeZonesResponse' || !Number.isSafeInteger(document.total_count) || (document.total_count as number) < 0 || !Array.isArray(document.zone_set) || document.zone_set.length > 100 || (document.total_count as number) < document.zone_set.length) return branch('invalidResponse', 'PROTOCOL');
    const zones: Zone[] = [];
    for (const item of document.zone_set) {
      if (!isRecord(item) || typeof item.zone_id !== 'string' || !item.zone_id || !['active', 'faulty', 'defunct'].includes(String(item.status))) return branch('invalidResponse', 'PROTOCOL');
      zones.push({ zoneId: item.zone_id, status: item.status as string });
    }
    return newQueryBranch('listed', { zones, totalCount: document.total_count as number });
  }
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'qingcloud', operation: 'describeZones', kind, message: 'QingCloud zone query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ZonesPage> { return newQueryBranch(name, empty, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<ZonesPage> { return newQueryRetry(failure(kind), delay); }
