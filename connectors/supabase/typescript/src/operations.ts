// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newMutationBranch, newQueryBranch, zeroReceipt, type Call, type Mutation, type MutationAttempt, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import { failure, mutationExchange, queryExchange, type SupabaseClient } from './client.js';
import { SelectRowsDefinition, UpsertRowsDefinition } from './generated/definitions.js';
import type { SelectRowsInput, SelectRowsOutput, UpsertRowsInput, UpsertRowsOutput } from './generated/models.js';

const identifier = /^[A-Za-z0-9_-]+$/;
const columnList = /^[A-Za-z_][A-Za-z0-9_]*(,[A-Za-z_][A-Za-z0-9_]*)*$/;
const emptyOutput = { data: null };

/** Read one bounded PostgREST page. */
export class SelectRowsOperation implements Query<SelectRowsInput, SelectRowsOutput> {
  /** Bind one Supabase client. */ constructor(private readonly client: SupabaseClient) {}
  /** Return the manifest operation contract. */ definition() { return SelectRowsDefinition; }
  /** Validate, request, and classify one query attempt. */
  async invoke(call: Call, input: SelectRowsInput): Promise<QueryAttempt<SelectRowsOutput>> {
    if (!identifier.test(input.table) || !Number.isInteger(input.limit ?? 0) || (input.limit ?? 0) < 0 || (input.limit ?? 0) > 1000 || !Number.isInteger(input.offset ?? 0) || (input.offset ?? 0) < 0) {
      return newQueryBranch('defect', emptyOutput, zeroReceipt(), failure('selectRows', 'VALIDATION', 'invalid table, limit, or offset'));
    }
    if (input.columns && !columnList.test(input.columns)) return newQueryBranch('defect', emptyOutput, zeroReceipt(), failure('selectRows', 'VALIDATION', 'columns must be comma-separated identifiers'));
    const query = new URLSearchParams();
    query.set('select', input.columns || '*'); query.set('limit', String(input.limit || 100));
    if (input.offset) query.set('offset', String(input.offset));
    return queryExchange(await this.client.exchange(call, 'selectRows', 'GET', `/${input.table}`, query));
  }
}

/** Repeat-safe merge by database-enforced conflict columns. */
export class UpsertRowsOperation implements Mutation<UpsertRowsInput, UpsertRowsOutput> {
  /** Bind one Supabase client. */ constructor(private readonly client: SupabaseClient) {}
  /** Return the manifest operation contract. */ definition() { return UpsertRowsDefinition; }
  /** Reuse the Dex Call ID; the conflict key makes repeated writes safe. */ idempotencyKey(callId: string): string { return callId; }
  /** Validate, request, and classify one mutation attempt. */
  async invoke(call: Call, input: UpsertRowsInput): Promise<MutationAttempt<UpsertRowsOutput>> {
    if (!identifier.test(input.table) || !columnList.test(input.onConflict) || !Array.isArray(input.rows) || input.rows.length < 1 || input.rows.length > 100) {
      return newMutationBranch('defect', emptyOutput, zeroReceipt(), failure('upsertRows', 'VALIDATION', 'table, unique onConflict, and 1 through 100 rows are required'));
    }
    const conflictColumns = input.onConflict.split(',');
    const seenKeys = new Set<string>();
    for (const row of input.rows) {
      if (!row || typeof row !== 'object' || Array.isArray(row) || Object.keys(row).length === 0) return newMutationBranch('defect', emptyOutput, zeroReceipt(), failure('upsertRows', 'VALIDATION', 'rows must not be empty'));
      const keyValues = conflictColumns.map(column => row[column]);
      if (keyValues.some(value => value === null || value === undefined)) return newMutationBranch('defect', emptyOutput, zeroReceipt(), failure('upsertRows', 'VALIDATION', 'every row must contain non-null onConflict columns'));
      let key: string;
      try { key = JSON.stringify(keyValues); if (key === undefined) throw new TypeError(); }
      catch { return newMutationBranch('defect', emptyOutput, zeroReceipt(), failure('upsertRows', 'VALIDATION', 'onConflict values must be JSON serializable')); }
      if (seenKeys.has(key)) return newMutationBranch('defect', emptyOutput, zeroReceipt(), failure('upsertRows', 'VALIDATION', 'rows contain duplicate onConflict values'));
      seenKeys.add(key);
    }
    let body: Uint8Array;
    try { body = Buffer.from(JSON.stringify(input.rows), 'utf8'); }
    catch { return newMutationBranch('defect', emptyOutput, zeroReceipt(), failure('upsertRows', 'VALIDATION', 'onConflict values must be JSON serializable')); }
    const query = new URLSearchParams({ on_conflict: input.onConflict });
    return mutationExchange(await this.client.exchange(call, 'upsertRows', 'POST', `/${input.table}`, query, body));
  }
}
