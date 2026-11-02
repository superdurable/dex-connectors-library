// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, newQueryRetry, type Call, type Failure, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import type { DatadogClient, DatadogExchange } from './client.js';
import { ListMonitorsDefinition, QueryMetricsDefinition } from './generated/definitions.js';
import type { ListMonitorsInput, MetricSeries, MetricsQuery, Monitor, MonitorsPage, QueryMetricsInput } from './generated/models.js';

const emptyMetricsQuery: MetricsQuery = { status: '', series: [] };
const emptyMonitorsPage: MonitorsPage = { monitors: [] };

/** Query Datadog timeseries points over a Unix-second window. */
export class QueryMetricsOperation implements Query<QueryMetricsInput, MetricsQuery> {
  /** Bind one authenticated client. */ constructor(private readonly client: DatadogClient) {}
  /** Return the generated manifest definition. */ definition() { return QueryMetricsDefinition; }
  /** Validate input, send one request, and classify the provider result. */
  async invoke(call: Call, input: QueryMetricsInput): Promise<QueryAttempt<MetricsQuery>> {
    if (!Number.isSafeInteger(input.from) || input.from <= 0 || !Number.isSafeInteger(input.to) || input.to <= input.from || !input.query.trim()) {
      return validationFailure('queryMetrics', emptyMetricsQuery, 'from, to, and query are required; to must follow from');
    }
    const exchange = await this.client.exchange(call, 'queryMetrics', '/api/v1/query', new URLSearchParams({ from: String(input.from), to: String(input.to), query: input.query }));
    if (!('response' in exchange)) return classify(exchange, emptyMetricsQuery);
    try {
      const parsed = decodeMetricsQuery(exchange.response.body);
      return newQueryBranch('queried', parsed, {});
    } catch {
      return invalidResponse('queryMetrics', emptyMetricsQuery);
    }
  }
}

/** List a zero-based page of Datadog monitors. */
export class ListMonitorsOperation implements Query<ListMonitorsInput, MonitorsPage> {
  /** Bind one authenticated client. */ constructor(private readonly client: DatadogClient) {}
  /** Return the generated manifest definition. */ definition() { return ListMonitorsDefinition; }
  /** Validate page settings, send one request, and classify the result. */
  async invoke(call: Call, input: ListMonitorsInput): Promise<QueryAttempt<MonitorsPage>> {
    if (!Number.isSafeInteger(input.page) || input.page < 0 || !Number.isSafeInteger(input.pageSize) || input.pageSize < 0 || input.pageSize > 1000) {
      return validationFailure('listMonitors', emptyMonitorsPage, 'page must be nonnegative and pageSize at most 1000');
    }
    const exchange = await this.client.exchange(call, 'listMonitors', '/api/v1/monitor', new URLSearchParams({ page: String(input.page), page_size: String(input.pageSize || 100) }));
    if (!('response' in exchange)) return classify(exchange, emptyMonitorsPage);
    try {
      const parsed = decodeMonitorsPage(exchange.response.body);
      return newQueryBranch('listed', parsed, {});
    } catch {
      return invalidResponse('listMonitors', emptyMonitorsPage);
    }
  }
}

function classify<Output>(exchange: Exclude<DatadogExchange, { response: unknown }>, empty: Output): QueryAttempt<Output> {
  if ('retry' in exchange) return newQueryRetry(exchange.failure, exchange.retryAfterMs);
  return newQueryBranch(exchange.branch, empty, {}, exchange.failure);
}

function validationFailure<Output>(operation: string, empty: Output, message: string): QueryAttempt<Output> {
  return newQueryBranch('defect', empty, {}, failure(operation, 'VALIDATION', message));
}

function invalidResponse<Output>(operation: string, empty: Output): QueryAttempt<Output> {
  return newQueryBranch('invalidResponse', empty, {}, failure(operation, 'PROTOCOL', 'response was malformed'));
}

function failure(operation: string, kind: Failure['kind'], message: string): Failure {
  return { kind, provider: 'datadog', operation, message };
}

function decodeMetricsQuery(body: Uint8Array): MetricsQuery {
  const document = goObject(JSON.parse(Buffer.from(body).toString('utf8')));
  const status = goString(document.status);
  if (status !== 'ok' || !Array.isArray(document.series)) throw new TypeError('missing successful metric series');
  const series: MetricSeries[] = document.series.map(value => {
    const item = goObject(value);
    return {
      metric: goString(item.metric),
      pointlist: goArray(item.pointlist).map(point => goArray(point).map(number => goNullableNumber(number))),
    };
  });
  return { status, series };
}

function decodeMonitorsPage(body: Uint8Array): MonitorsPage {
  const document: unknown = JSON.parse(Buffer.from(body).toString('utf8'));
  if (!Array.isArray(document)) throw new TypeError('missing monitors');
  const monitors: Monitor[] = document.map(value => {
    const item = goObject(value);
    return {
      id: goNumber(item.id), name: goString(item.name), type: goString(item.type), overall_state: goString(item.overall_state),
    };
  });
  return { monitors };
}

function goObject(value: unknown): Record<string, unknown> {
  if (value === null) return {};
  if (typeof value !== 'object' || Array.isArray(value)) throw new TypeError('expected JSON object');
  return value as Record<string, unknown>;
}

function goArray(value: unknown): unknown[] {
  if (value === null || value === undefined) return [];
  if (!Array.isArray(value)) throw new TypeError('expected JSON array');
  return value;
}

function goString(value: unknown): string {
  if (value === null || value === undefined) return '';
  if (typeof value !== 'string') throw new TypeError('expected JSON string');
  return value;
}

function goNumber(value: unknown): number {
  if (value === null || value === undefined) return 0;
  if (typeof value !== 'number' || !Number.isSafeInteger(value)) throw new TypeError('expected JSON integer');
  return value;
}

function goNullableNumber(value: unknown): number | null {
  if (value === null) return null;
  if (typeof value !== 'number' || !Number.isFinite(value)) throw new TypeError('expected JSON number');
  return value;
}
