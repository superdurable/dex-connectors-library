// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, type Call, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import type { SendGridClient } from './client.js';
import { GetStatsDefinition, ListTemplatesDefinition } from './generated/definitions.js';
import type { GetStatsInput, ListTemplatesInput, StatsOutput, TemplatesOutput } from './generated/models.js';

/** Read global email statistics for an inclusive date range. */
export class GetStatsOperation implements Query<GetStatsInput, StatsOutput> {
  /** Bind one provider client. */ constructor(private readonly client: SendGridClient) {}
  /** Return the manifest definition. */ definition() { return GetStatsDefinition; }
  /** Validate dates, send a request, and decode category totals. */
  invoke(call: Call, input: GetStatsInput): Promise<QueryAttempt<StatsOutput>> {
    if (!isDate(input.startDate)) return Promise.resolve(defect('getStats', { days: [] }, 'startDate must be YYYY-MM-DD'));
    const parameters = new URLSearchParams({ start_date: input.startDate });
    if (input.endDate) {
      if (!isDate(input.endDate) || input.endDate < input.startDate) return Promise.resolve(defect('getStats', { days: [] }, 'endDate must be YYYY-MM-DD and not before startDate'));
      parameters.set('end_date', input.endDate);
    }
    return this.client.query(call, 'getStats', 'found', '/v3/stats', parameters, { days: [] }, decodeStats);
  }
}

/** Read one bounded transactional template page. */
export class ListTemplatesOperation implements Query<ListTemplatesInput, TemplatesOutput> {
  /** Bind one provider client. */ constructor(private readonly client: SendGridClient) {}
  /** Return the manifest definition. */ definition() { return ListTemplatesDefinition; }
  /** Validate page selection, send a request, and decode the page. */
  invoke(call: Call, input: ListTemplatesInput): Promise<QueryAttempt<TemplatesOutput>> {
    const empty: TemplatesOutput = { templates: [], count: 0 };
    const generation = input.generation || 'dynamic';
    if (generation !== 'dynamic' && generation !== 'legacy') return Promise.resolve(defect('listTemplates', empty, 'generation must be dynamic or legacy'));
    const pageSize = input.pageSize || 100;
    if (!Number.isSafeInteger(pageSize) || pageSize < 1 || pageSize > 200) return Promise.resolve(defect('listTemplates', empty, 'pageSize must be 1 to 200'));
    const parameters = new URLSearchParams({ generations: generation, page_size: String(pageSize) });
    if (input.pageToken) parameters.set('page_token', input.pageToken);
    return this.client.query(call, 'listTemplates', 'listed', '/v3/templates', parameters, empty, decodeTemplates);
  }
}

function defect<Output>(operation: string, empty: Output, message: string): QueryAttempt<Output> {
  return newQueryBranch('defect', empty, {}, { kind: 'VALIDATION', provider: 'sendgrid', operation, message });
}
function isDate(value: string): boolean {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return false;
  const date = new Date(`${value}T00:00:00Z`);
  return !Number.isNaN(date.getTime()) && date.toISOString().slice(0, 10) === value;
}
function object(value: unknown): Record<string, unknown> {
  if (value === null) return {};
  if (typeof value !== 'object' || Array.isArray(value)) throw new Error('expected object');
  return value as Record<string, unknown>;
}
function integer(value: unknown): number { if (value === undefined || value === null) return 0; if (!Number.isSafeInteger(value)) throw new Error('expected integer'); return value as number; }
function string(value: unknown): string { if (value === undefined || value === null) return ''; if (typeof value !== 'string') throw new Error('expected string'); return value; }
function decodeStats(body: Uint8Array): StatsOutput {
  const document: unknown = JSON.parse(Buffer.from(body).toString('utf8'));
  if (!Array.isArray(document)) throw new Error('statistics array missing');
  return { days: document.map(value => {
    const day = object(value);
    if (!string(day.date) || !Array.isArray(day.stats) || day.stats.length === 0) throw new Error('statistics day incomplete');
    let requests = 0; let delivered = 0;
    for (const category of day.stats) { const metrics = object(object(category).metrics); requests += integer(metrics.requests); delivered += integer(metrics.delivered); }
    return { date: string(day.date), requests, delivered };
  }) };
}
function decodeTemplates(body: Uint8Array): TemplatesOutput {
  const document = object(JSON.parse(Buffer.from(body).toString('utf8')) as unknown);
  if (!Array.isArray(document.result)) throw new Error('template result missing');
  const metadata = object(document._metadata);
  const templates = document.result.map(value => { const item = object(value); return { id: string(item.id), name: string(item.name), generation: string(item.generation) }; });
  const output: TemplatesOutput = { templates, count: integer(metadata.count) };
  const next = string(metadata.next);
  if (next) {
    const parsed = new URL(next);
    if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') throw new Error('template next-page URL invalid');
    const token = parsed.searchParams.get('page_token');
    if (!token) throw new Error('template next-page token missing');
    output.nextPageToken = token;
  }
  return output;
}
