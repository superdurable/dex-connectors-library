// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomBytes } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { DescribeRegionsDefinition } from './generated/definitions.js';
import type { Config, Credentials, DescribeRegionsInput, DescribeRegionsOutput, ECSRegion } from './generated/models.js';
import { EmptyPayloadSHA256, SignedHeaderNames, signACS3Request } from './signature.js';

const emptyOutput: DescribeRegionsOutput = { regions: [] };

/** Send one ECS request through a caller-owned transport. */
export type AlibabaRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;

/** Optional request dependencies for deterministic provider tests. */
export interface AlibabaClientOptions {
  /** Request sender; production uses the Connector SDK transport. */
  send?: AlibabaRequestSender;
  /** UTC request clock; production uses the current system time. */
  clock?: () => Date;
  /** Sixteen random nonce bytes; production uses cryptographic randomness. */
  nonceSource?: () => Uint8Array;
}

/** Resolve Alibaba Cloud RAM credentials for each ECS query. */
export class AlibabaClient {
  readonly baseUrl: string;
  readonly endpointHost: string;
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly credentials: CredentialProvider<Credentials>;
  readonly send: AlibabaRequestSender;
  readonly clock: () => Date;
  readonly nonceSource: () => Uint8Array;

  /** Validate settings without contacting Alibaba Cloud. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: AlibabaClientOptions = {}) {
    if (!credentials) throw new TypeError('Alibaba Cloud credential source is required');
    const baseUrl = validateBaseURL(config.baseUrl || 'https://ecs.cn-hangzhou.aliyuncs.com');
    const parsed = new URL(baseUrl);
    if (parsed.pathname !== '/' || parsed.search || parsed.hash) throw new TypeError('Alibaba Cloud ECS base URL must be a host root');
    this.baseUrl = baseUrl.replace(/\/$/, '');
    this.endpointHost = parsed.host;
    this.timeoutMs = config.timeoutMs || 10_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Alibaba Cloud timeout and maxResponseBytes must be positive');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
    this.clock = options.clock ?? (() => new Date());
    this.nonceSource = options.nonceSource ?? (() => randomBytes(16));
  }

  /** Bind the ECS region Query. */
  describeRegions(): DescribeRegionsOperation { return new DescribeRegionsOperation(this); }
}

/** List ECS regions without storing RAM credentials in Flow state. */
export class DescribeRegionsOperation implements Query<DescribeRegionsInput, DescribeRegionsOutput> {
  /** Bind an authenticated Alibaba Cloud client. */
  constructor(private readonly client: AlibabaClient) {}

  /** Return the stable generated Query contract. */
  definition(): QueryDefinition { return DescribeRegionsDefinition; }

  /** Sign, send, and classify one ECS DescribeRegions request. */
  async invoke(call: Call, input: DescribeRegionsInput): Promise<QueryAttempt<DescribeRegionsOutput>> {
    if (input === null || typeof input !== 'object' || Array.isArray(input)) return branch('defect', 'VALIDATION', 'ECS request is invalid');
    let credentials: Credentials;
    try { credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION', 'connection credentials are unavailable or invalid'); }
    const accessKeyId = credentials.accessKeyId instanceof SecretString ? credentials.accessKeyId.reveal() : '';
    const accessKeySecret = credentials.accessKeySecret instanceof SecretString ? credentials.accessKeySecret.reveal() : '';
    if (!isValidCredential(accessKeyId) || !isValidCredential(accessKeySecret)) return branch('defect', 'AUTHENTICATION', 'connection credentials are unavailable or invalid');
    let nonce: string;
    try {
      const bytes = this.client.nonceSource();
      if (bytes.length !== 16) throw new TypeError('nonce must contain sixteen bytes');
      nonce = Buffer.from(bytes).toString('hex');
    } catch { return retry('AVAILABILITY', 'request nonce unavailable'); }
    const currentTime = this.client.clock();
    if (Number.isNaN(currentTime.getTime())) return branch('defect', 'VALIDATION', 'ECS request date is invalid');
    const requestDate = currentTime.toISOString().replace(/\.\d{3}Z$/, 'Z');
    const signature = signACS3Request(accessKeySecret, 'GET', this.client.endpointHost, 'DescribeRegions', '', requestDate, nonce);
    const authorization = `ACS3-HMAC-SHA256 Credential=${accessKeyId},SignedHeaders=${SignedHeaderNames},Signature=${signature}`;
    let response: ProviderResponse;
    try {
      response = await this.client.send({
        url: `${this.client.baseUrl}/`, method: 'GET',
        headers: {
          'x-acs-action': 'DescribeRegions', 'x-acs-content-sha256': EmptyPayloadSHA256,
          'x-acs-date': requestDate, 'x-acs-signature-nonce': nonce, 'x-acs-version': '2014-05-26',
          Authorization: authorization, Accept: 'application/json',
        },
        maxResponseBytes: this.client.maxResponseBytes, timeoutMs: this.client.timeoutMs,
        addressPolicy: this.client.baseUrl === 'https://ecs.cn-hangzhou.aliyuncs.com' ? 'publicHTTPS' : 'privateNetworkHTTP',
        signal: call.context.cancellationSignal,
      });
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() };
        if (response.status !== 429 && response.status !== 408 && response.status < 500) return branch('invalidResponse', 'RESPONSE_TOO_LARGE', 'ECS response exceeded the configured limit');
      } else return retry('AVAILABILITY', 'ECS request failed');
    }
    const retryAfterHeader = response.headers['retry-after'];
    const retryAfterMs = parseRetryAfter(Array.isArray(retryAfterHeader) ? retryAfterHeader[0] ?? '' : retryAfterHeader ?? '');
    if (response.status === 429) return retry('RATE_LIMIT', 'ECS rate limited the request', retryAfterMs);
    if (response.status === 408 || response.status >= 500) return retry('AVAILABILITY', 'ECS is temporarily unavailable', retryAfterMs);
    let document: unknown;
    try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL', 'ECS returned invalid JSON'); }
    if (!isRecord(document) || (document.Code !== undefined && document.Code !== null && typeof document.Code !== 'string')) return branch('invalidResponse', 'PROTOCOL', 'ECS returned invalid JSON');
    const code = typeof document.Code === 'string' ? document.Code : '';
    if (code.startsWith('Throttling')) return retry('RATE_LIMIT', 'ECS rate limited the request', retryAfterMs);
    if (code || response.status !== 200) return branch('providerRejected', 'PROVIDER_REJECTION', 'ECS rejected the regions request');
    const regionsValue = document.Regions;
    if (!isRecord(regionsValue) || !Array.isArray(regionsValue.Region)) return branch('invalidResponse', 'PROTOCOL', 'ECS omitted its regions list');
    const regions: ECSRegion[] = [];
    for (const row of regionsValue.Region) {
      if (!isRecord(row) || typeof row.RegionId !== 'string' || !row.RegionId || typeof row.LocalName !== 'string' || !row.LocalName || typeof row.RegionEndpoint !== 'string' || !row.RegionEndpoint || (row.Status !== 'available' && row.Status !== 'soldOut')) {
        return branch('invalidResponse', 'PROTOCOL', 'ECS returned an incomplete region');
      }
      regions.push({ regionId: row.RegionId, localName: row.LocalName, endpoint: row.RegionEndpoint, status: row.Status });
    }
    return newQueryBranch('fetched', { regions });
  }
}

function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function isValidCredential(value: string): boolean { return /^[!-~]{1,512}$/.test(value); }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'alibaba', operation: 'describeRegions', kind, message }; }
function branch(id: string, kind: Failure['kind'], message: string): QueryAttempt<DescribeRegionsOutput> { return newQueryBranch(id, emptyOutput, {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delayMs = 0): QueryAttempt<DescribeRegionsOutput> { return newQueryRetry(failure(kind, message), delayMs); }
