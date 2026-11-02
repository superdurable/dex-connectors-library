// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, newQueryRetry, type Call, type Failure, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import type { CodaClient, CodaExchangeFailure, CodaExchangeResult } from './client.js';
import { GetRowDefinition, ListRowsDefinition } from './generated/definitions.js';
import type { GetRowInput, GetRowOutput, ListRowsInput, ListRowsOutput } from './generated/models.js';

const identifier = /^[A-Za-z0-9_-]+$/;
const emptyOutput = { data: null };

/** List one page of rows from a Coda table. */
export class ListRowsOperation implements Query<ListRowsInput, ListRowsOutput> {
  /** Bind a validated Coda client. */ constructor(private readonly client: CodaClient) {}
  /** Return the manifest operation definition. */ definition() { return ListRowsDefinition; }
  /** Validate and query one provider page. */
  async invoke(call: Call, input: ListRowsInput): Promise<QueryAttempt<ListRowsOutput>> {
    if (!identifier.test(input.docId) || !identifier.test(input.tableId) || !Number.isInteger(input.limit ?? 0) || (input.limit ?? 0) < 0 || (input.limit ?? 0) > 500) {
      return failedBranch('defect', 'listRows', 'VALIDATION', 'invalid doc, table, or limit');
    }
    const query = new URLSearchParams();
    if (input.limit) query.set('limit', String(input.limit));
    if (input.pageToken) query.set('pageToken', input.pageToken);
    const result = await this.client.exchange(call, 'listRows', `/docs/${input.docId}/tables/${input.tableId}/rows`, query, false);
    if ('outcome' in result) return classifyOutcome(result.outcome);
    const decoded = decodeObject(result);
    if (!decoded || !Array.isArray(decoded.items)) return failedBranch('invalidResponse', 'listRows', 'PROTOCOL', 'provider returned an incomplete response');
    return newQueryBranch('listed', { data: decoded });
  }
}

/** Get one stable row from a Coda table. */
export class GetRowOperation implements Query<GetRowInput, GetRowOutput> {
  /** Bind a validated Coda client. */ constructor(private readonly client: CodaClient) {}
  /** Return the manifest operation definition. */ definition() { return GetRowDefinition; }
  /** Validate and query one provider row. */
  async invoke(call: Call, input: GetRowInput): Promise<QueryAttempt<GetRowOutput>> {
    if (!identifier.test(input.docId) || !identifier.test(input.tableId) || !identifier.test(input.rowId)) {
      return failedBranch('defect', 'getRow', 'VALIDATION', 'doc, table, and row IDs are required');
    }
    const result = await this.client.exchange(call, 'getRow', `/docs/${input.docId}/tables/${input.tableId}/rows/${input.rowId}`, new URLSearchParams(), true);
    if ('outcome' in result) return classifyOutcome(result.outcome);
    const decoded = decodeObject(result);
    if (!decoded || typeof decoded.id !== 'string' || !decoded.id) return failedBranch('invalidResponse', 'getRow', 'PROTOCOL', 'provider returned an incomplete response');
    return newQueryBranch('found', { data: decoded });
  }
}

function decodeObject(result: CodaExchangeResult): Record<string, unknown> | null {
  if (!('body' in result)) return null;
  try {
    // Supported Node versions expose source-aware parsing before TypeScript declares it.
    const jsonWithRawNumbers = JSON as unknown as {
      parse(text: string, reviver: (key: string, value: unknown, context: { source: string }) => unknown): unknown;
      rawJSON(text: string): unknown;
    };
    const decoded = jsonWithRawNumbers.parse(Buffer.from(result.body).toString('utf8'), (_key, value, context) =>
      typeof value === 'number' ? jsonWithRawNumbers.rawJSON(context.source) : value);
    return decoded && typeof decoded === 'object' && !Array.isArray(decoded) ? decoded as Record<string, unknown> : null;
  } catch { return null; }
}

function classifyOutcome(outcome: CodaExchangeFailure): QueryAttempt<{ data: null }> {
  if (outcome.branch === 'retry') return newQueryRetry(outcome.failure, outcome.retryAfterMs);
  return newQueryBranch(outcome.branch, emptyOutput, {}, outcome.failure);
}

function failedBranch(branch: string, operation: string, kind: Failure['kind'], message: string): QueryAttempt<{ data: null }> {
  return newQueryBranch(branch, emptyOutput, {}, { kind, provider: 'coda', operation, message });
}
