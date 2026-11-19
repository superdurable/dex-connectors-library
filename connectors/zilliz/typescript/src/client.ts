// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListClustersDefinition } from './generated/definitions.js';
import type { ClusterSummary, Config, Credentials, ListClustersInput, ListClustersOutput } from './generated/models.js';

const endpoint = 'https://api.cloud.zilliz.com/v2/clusters?pageSize=25&currentPage=1';
const emptyOutput: ListClustersOutput = { total: 0, clusters: [] };

/** Optional transport for deterministic Zilliz provider tests. */
export interface ZillizClientOptions { send?: (request: ProviderRequest) => Promise<ProviderResponse>; }

/** Resolve the Zilliz API Key for each cluster-list read. */
export class ZillizClient {
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Validate request limits without contacting the provider. */
  constructor(config: Config, readonly credentials: CredentialProvider<Credentials>, options: ZillizClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Zilliz configuration and credentials are required');
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('Zilliz request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the first-page cluster-list Query. */
  listClusters(): ListClustersOperation { return new ListClustersOperation(this); }
}

/** List nonsecret metadata for the first 25 visible clusters. */
export class ListClustersOperation implements Query<ListClustersInput, ListClustersOutput> {
  /** Bind the configured provider client. */
  constructor(private readonly client: ZillizClient) {}
  /** Return the generated Query contract. */
  definition(): QueryDefinition { return ListClustersDefinition; }
  /** Send and classify one cluster-list request. */
  async invoke(call: Call, input: ListClustersInput): Promise<QueryAttempt<ListClustersOutput>> {
    if (input === null || typeof input !== 'object' || Array.isArray(input)) return branch('defect', 'VALIDATION');
    let credentials: Credentials;
    try { credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION'); }
    const apiKey = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    if (!isValidAPIKey(apiKey)) return branch('defect', 'AUTHENTICATION');
    let response: ProviderResponse;
    try {
      response = await this.client.send({ url: endpoint, method: 'GET', headers: { Authorization: `Bearer ${apiKey}`, Accept: 'application/json' },
        timeoutMs: this.client.timeoutMs, maxResponseBytes: this.client.maxResponseBytes,
        addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal });
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status === 200) return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
      if (error instanceof BodyTooLargeError && error.status !== undefined) return classifyStatus(error.status, error.headers ?? {});
      return retry('AVAILABILITY');
    }
    if (response.status !== 200) return classifyStatus(response.status, response.headers);
    if (response.body.length > this.client.maxResponseBytes) return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let document: unknown;
    try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(document) || !Number.isInteger(document.code)) return branch('invalidResponse', 'PROTOCOL');
    if (document.code !== 0) return branch('providerRejected', 'PROVIDER_REJECTION');
    if (!isRecord(document.data) || !Number.isInteger(document.data.count) || (document.data.count as number) < 0 || !Array.isArray(document.data.clusters) || document.data.clusters.length > 25) return branch('invalidResponse', 'PROTOCOL');
    const clusters: ClusterSummary[] = [];
    for (const item of document.data.clusters) {
      if (!isRecord(item) || typeof item.clusterId !== 'string' || !item.clusterId || Buffer.byteLength(item.clusterId, 'utf8') > 128 || item.clusterId.trim() !== item.clusterId || typeof item.clusterName !== 'string' || Buffer.byteLength(item.clusterName, 'utf8') > 200 || typeof item.regionId !== 'string' || Buffer.byteLength(item.regionId, 'utf8') > 100 || typeof item.plan !== 'string' || Buffer.byteLength(item.plan, 'utf8') > 100 || typeof item.status !== 'string' || Buffer.byteLength(item.status, 'utf8') > 100) return branch('invalidResponse', 'PROTOCOL');
      clusters.push({ clusterID: item.clusterId, name: item.clusterName, regionID: item.regionId, plan: item.plan, status: item.status });
    }
    return newQueryBranch('found', { total: document.data.count as number, clusters });
  }
}

function classifyStatus(status: number, headers: ProviderResponse['headers']): QueryAttempt<ListClustersOutput> {
  const value = headers['retry-after']; const delay = parseRetryAfter(Array.isArray(value) ? value[0] ?? '' : value ?? '');
  if (status === 429) return retry('RATE_LIMIT', delay);
  if (status === 408 || status >= 500) return retry('AVAILABILITY', delay);
  const kind: Failure['kind'] = status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION';
  return branch('providerRejected', kind);
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function isValidAPIKey(value: string): boolean { return value.length > 0 && value.length <= 1024 && /^[!-~]+$/.test(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'zilliz', operation: 'listClusters', kind, message: 'Zilliz cluster listing failed' }; }
function branch(id: string, kind: Failure['kind']): QueryAttempt<ListClustersOutput> { return newQueryBranch(id, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delayMs = 0): QueryAttempt<ListClustersOutput> { return newQueryRetry(failure(kind), delayMs); }
