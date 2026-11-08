// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, newQueryRetry, type Call, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { ListDatasetsDefinition } from './generated/definitions.js';
import type { DatasetsPage, ListDatasetsInput } from './generated/models.js';
import type { BigQueryClient, BigQueryExchange } from './client.js';

const emptyPage = (): DatasetsPage => ({ datasets: [], nextPageToken: '' });

/** Read one bounded dataset page in a Google Cloud project. */
export class ListDatasetsOperation implements Query<ListDatasetsInput, DatasetsPage> {
  /** Bind the provider client. */ constructor(private readonly client: BigQueryClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return ListDatasetsDefinition; }
  /** Project dataset IDs, project IDs, display names, and the next page token. */
  async invoke(call: Call, input: ListDatasetsInput): Promise<QueryAttempt<DatasetsPage>> {
    if (!input || !validProjectID(input.projectId) ||
      (input.maxResults !== undefined && (!Number.isSafeInteger(input.maxResults) || input.maxResults < 0 || input.maxResults > 100)) ||
      (input.pageToken !== undefined && !validPageToken(input.pageToken)))
      return branch('defect', 'VALIDATION', 'project ID, page size, or page token is invalid');
    const maxResults = input.maxResults || 100;
    const query = new URLSearchParams({ maxResults: String(maxResults) });
    if (input.pageToken) query.set('pageToken', input.pageToken);
    const path = `/bigquery/v2/projects/${encodeURIComponent(input.projectId)}/datasets?${query.toString()}`;
    const exchange = await this.client.get(call, path);
    const failed = classifyRead(exchange);
    if (failed) return failed;
    const document = parseDocument(exchange.body);
    const entries = document?.datasets ?? [];
    if (!document || !Array.isArray(entries) || entries.length > maxResults ||
      (document.nextPageToken !== undefined && document.nextPageToken !== null && !validPageToken(document.nextPageToken)))
      return branch('invalidResponse', 'PROTOCOL', 'BigQuery returned invalid dataset JSON');
    const datasets: DatasetsPage['datasets'] = [];
    for (const item of entries) {
      if (!isObject(item) || !isObject(item.datasetReference) || !validDatasetID(item.datasetReference.datasetId) ||
        item.datasetReference.projectId !== input.projectId ||
        (item.friendlyName !== undefined && item.friendlyName !== null && typeof item.friendlyName !== 'string'))
        return branch('invalidResponse', 'PROTOCOL', 'BigQuery returned invalid dataset JSON');
      datasets.push({ id: item.datasetReference.datasetId, projectId: input.projectId, friendlyName: item.friendlyName ?? '' });
    }
    return newQueryBranch('listed', { datasets, nextPageToken: typeof document.nextPageToken === 'string' ? document.nextPageToken : '' });
  }
}

function classifyRead(exchange: BigQueryExchange): QueryAttempt<DatasetsPage> | undefined {
  if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION', 'connection credentials are unavailable or invalid');
  if (exchange.error === 'refresh' || exchange.error === 'request') return retry('TRANSPORT', 'BigQuery request failed');
  if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE', 'BigQuery response exceeded the configured limit');
  if (exchange.status === 429) return retry('RATE_LIMIT', 'BigQuery rate limited the request', exchange.retryAfterMs);
  if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'BigQuery is temporarily unavailable', exchange.retryAfterMs);
  if (exchange.status !== 200) return branch('providerRejected', 'PROVIDER_REJECTION', `BigQuery rejected HTTP ${exchange.status}`);
  return undefined;
}
function validProjectID(value: unknown): value is string { return typeof value === 'string' && /^[A-Za-z0-9][A-Za-z0-9:._~-]{0,255}$/.test(value); }
function validDatasetID(value: unknown): value is string { return typeof value === 'string' && value.length <= 1024 && /^[A-Za-z0-9_]+$/.test(value); }
function validPageToken(value: unknown): value is string { return typeof value === 'string' && Buffer.byteLength(value) <= 4096 && !/[\r\n]/.test(value); }
function parseDocument(body: Uint8Array): Record<string, unknown> | undefined {
  try { const value: unknown = JSON.parse(Buffer.from(body).toString('utf8')); return isObject(value) ? value : undefined; }
  catch { return undefined; }
}
function isObject(value: unknown): value is Record<string, unknown> { return !!value && typeof value === 'object' && !Array.isArray(value); }
function branch(branchId: string, kind: Failure['kind'], message: string): QueryAttempt<DatasetsPage> {
  return newQueryBranch(branchId, emptyPage(), {}, failure(kind, message));
}
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<DatasetsPage> {
  return newQueryRetry(failure(kind, message), delay);
}
function failure(kind: Failure['kind'], message: string): Failure {
  return { kind, provider: 'google-bigquery', operation: 'listDatasets', message };
}
