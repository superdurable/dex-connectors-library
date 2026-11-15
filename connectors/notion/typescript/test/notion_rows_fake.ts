// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

/**
 * One Notion data source's rows behind a NotionClient `send` option, a port of the Go notionrowsfake package. It
 * answers the data source query with a last_edited_time on_or_after filter and timestamp sort, and page reads.
 * Start cursors are "rows-<offset>". Tests set every row's created and last edited time, so minute rounding and
 * shared timestamps are scripted, not guessed.
 */

import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';

/** The only API token the fake accepts; it is test data, not a credential. */
export const Token = 'ntn_fake_rows_token';
/** The fake's API base URL. */
export const Endpoint = 'http://127.0.0.1:9';
const notionVersion = '2026-03-11';

/** One request the fake answered. */
export interface RecordedRequest { method: string; path: string; body?: QueryBody; status: number }

/** The subset of a data source query the fake reads. */
export interface QueryBody {
  filter?: { timestamp?: string; last_edited_time?: { on_or_after?: string } };
  sorts?: { timestamp?: string; direction?: string }[];
  start_cursor?: string;
  page_size?: number;
}

interface StoredRow { id: string; title: string; created: Date; lastEdited: Date; isInTrash: boolean }

/** A stateful single-data-source fake. */
export class NotionRowsFake {
  readonly requests: RecordedRequest[] = [];
  private readonly rows = new Map<string, StoredRow>();
  private readonly hiddenRows = new Set<string>();
  private rateLimited = 0;
  private retryAfter = '';
  private isStartCursorStale = false;

  constructor(readonly dataSourceId: string, readonly databaseId: string) {}

  /** The NotionClient `send` option answered by this fake. */
  readonly send = async (request: ProviderRequest): Promise<ProviderResponse> => {
    const path = new URL(request.url).pathname;
    const recorded: RecordedRequest = { method: request.method, path, status: 0 };
    const [status, body] = this.answer(request, path, recorded);
    recorded.status = status;
    this.requests.push(recorded);
    return { status, headers: { 'content-type': 'application/json', ...(status === 429 ? { 'retry-after': this.retryAfter } : {}) }, body: Buffer.from(body) };
  };

  /** Add a row created and last edited at createdAt. */
  create(rowId: string, title: string, createdAt: Date): void { this.rows.set(rowId, { id: rowId, title, created: createdAt, lastEdited: createdAt, isInTrash: false }); }
  /** Retitle a row and set its last edited time. */
  edit(rowId: string, title: string, editedAt: Date): void { const row = this.mustRow(rowId); row.title = title; row.lastEdited = editedAt; }
  /** Move a row to the trash, which the data source query no longer returns. */
  trash(rowId: string, editedAt: Date): void { const row = this.mustRow(rowId); row.isInTrash = true; row.lastEdited = editedAt; }
  /** Simulate Notion indexing a row late: queries omit it until revealToQueries. */
  hideFromQueries(rowId: string): void { this.hiddenRows.add(rowId); }
  /** Make a hidden row visible to queries again. */
  revealToQueries(rowId: string): void { this.hiddenRows.delete(rowId); }
  /** Answer the next count requests 429 rate_limited with Retry-After seconds. */
  rateLimitNextRequests(count: number, retryAfter: string): void { this.rateLimited = count; this.retryAfter = retryAfter; }
  /** Answer the next query that sends a start cursor 400 validation_error. */
  expireStartCursors(): void { this.isStartCursorStale = true; }

  private answer(request: ProviderRequest, path: string, recorded: RecordedRequest): [number, string] {
    if (request.headers?.Authorization !== `Bearer ${Token}` || request.headers?.['Notion-Version'] !== notionVersion) return [401, notionError(401, 'unauthorized')];
    if (this.rateLimited > 0) { this.rateLimited--; return [429, notionError(429, 'rate_limited')]; }
    if (request.method === 'POST' && path === `/v1/data_sources/${this.dataSourceId}/query`) {
      let body: QueryBody;
      try { body = JSON.parse(Buffer.from(request.body ?? new Uint8Array()).toString('utf8')) as QueryBody; }
      catch { return [400, notionError(400, 'invalid_json')]; }
      recorded.body = body;
      return this.answerQuery(body);
    }
    if (request.method === 'POST' && path.endsWith('/query')) return [404, notionError(404, 'object_not_found')];
    if (request.method === 'GET' && path.startsWith('/v1/pages/')) {
      const row = this.rows.get(path.slice('/v1/pages/'.length));
      return row ? [200, this.pageResource(row)] : [404, notionError(404, 'object_not_found')];
    }
    return [404, notionError(404, 'object_not_found')];
  }

  private answerQuery(body: QueryBody): [number, string] {
    const sorts = body.sorts ?? [];
    const pageSize = body.page_size ?? 0;
    if (sorts.length !== 1 || sorts[0]!.timestamp !== 'last_edited_time' || pageSize < 1 || pageSize > 100) return [400, notionError(400, 'validation_error')];
    let offset = 0;
    if (body.start_cursor) {
      if (this.isStartCursorStale || !body.start_cursor.startsWith('rows-')) { this.isStartCursorStale = false; return [400, notionError(400, 'validation_error')]; }
      offset = Number(body.start_cursor.slice('rows-'.length));
    }
    const lowerBound = body.filter ? Date.parse(body.filter.last_edited_time?.on_or_after ?? '') : -Infinity;
    const isDescending = sorts[0]!.direction === 'descending';
    const matched = [...this.rows.values()]
      .filter(row => !row.isInTrash && !this.hiddenRows.has(row.id) && row.lastEdited.getTime() >= lowerBound)
      .sort((left, right) => left.lastEdited.getTime() !== right.lastEdited.getTime()
        ? (left.lastEdited.getTime() - right.lastEdited.getTime()) * (isDescending ? -1 : 1)
        : left.id < right.id ? -1 : 1);
    const end = Math.min(offset + pageSize, matched.length);
    const results = matched.slice(Math.min(offset, end), end).map(row => this.pageResource(row)).join(',');
    const hasMore = end < matched.length;
    return [200, `{"object":"list","results":[${results}],"next_cursor":${hasMore ? JSON.stringify(`rows-${end}`) : 'null'},"has_more":${hasMore},"type":"page_or_data_source"}`];
  }

  private pageResource(row: StoredRow): string {
    return JSON.stringify({
      object: 'page', id: row.id, created_time: row.created.toISOString(), last_edited_time: row.lastEdited.toISOString(), in_trash: row.isInTrash, is_archived: false,
      url: `https://www.notion.so/${row.id.replaceAll('-', '')}`,
      parent: { type: 'data_source_id', data_source_id: this.dataSourceId, database_id: this.databaseId },
      properties: { Name: { id: 'title', type: 'title', title: [{ type: 'text', text: { content: row.title }, plain_text: row.title }] } },
    });
  }

  private mustRow(rowId: string): StoredRow {
    const row = this.rows.get(rowId);
    if (!row) throw new Error(`notion rows fake: unknown row ${rowId}`);
    return row;
  }
}

function notionError(status: number, code: string): string { return JSON.stringify({ object: 'error', status, code, request_id: 'fake-request' }); }
