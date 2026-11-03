// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import sql from 'mssql';
import { newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, type Call, type Mutation, type MutationAttempt, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import { ExecuteStatementDefinition, QueryRowsDefinition } from './generated/definitions.js';
import type { Column, ExecuteStatementInput, QueryRowsInput, RowSet, StatementExecution } from './generated/models.js';
import { classifyDriverError, type ClassifiedFailure, type SQLServerClient } from './client.js';

const emptyRows = (): RowSet => ({ columns: [], rows: [] });
const emptyExecution = (): StatementExecution => ({ rowsAffected: 0, columns: [], rows: [] });

function validateStatement(statement: string, parameters: unknown[], isWrite: boolean): void {
  if (!statement?.trim() || Buffer.byteLength(statement, 'utf8') > 65536 || statement.includes('\0')) throw new TypeError('one nonempty SQL statement is required');
  const tokens = statement.replace(/--[^\n]*|\/\*[\s\S]*?\*\/|'(?:''|[^'])*'|\[(?:]]|[^\]])*\]|"(?:""|[^"])*"/g, ' ').trim();
  if (!new RegExp(isWrite ? '^(?:INSERT|UPDATE|DELETE|MERGE|WITH)\\b' : '^(?:SELECT|WITH)\\b', 'i').test(tokens)) throw new TypeError('statement starts with the wrong SQL operation');
  if (/\b(?:COMMIT|ROLLBACK|BEGIN|EXEC|EXECUTE|USE|DECLARE|GRANT|REVOKE|OPENROWSET|OPENDATASOURCE)\b/i.test(tokens)) throw new TypeError('transaction control, dynamic SQL, and context switching are not allowed');
  if (/;[\s\S]*\S/.test(tokens.replace(/;\s*$/, ''))) throw new TypeError('more than one SQL statement is not allowed');
  const found = [...tokens.matchAll(/@p(\d+)\b/gi)].map(match => Number(match[1]));
  if (/@(?!p\d+\b)[a-z_]/i.test(tokens) || found.some(index => index < 1 || index > parameters.length) || parameters.some((_, index) => !found.includes(index + 1))) throw new TypeError('statement parameters must use each bound @pN placeholder');
  if (parameters.some(value => value !== null && typeof value !== 'string' && typeof value !== 'number' && typeof value !== 'boolean' && !(value instanceof Date) && !Buffer.isBuffer(value))) throw new TypeError('unsupported SQL parameter type');
}

function bindParameters(request: sql.Request, values: unknown[]): void {
  values.forEach((value, index) => request.input(`p${index + 1}`, value));
}

function decodeRows(result: sql.IResult<Record<string, unknown>>, maxRows: number, maxResponseBytes: number): { columns: Column[]; rows: Array<Record<string, unknown>>; truncated: boolean } {
  const recordset = result.recordset;
  if (!recordset) return { columns: [], rows: [], truncated: false };
  const columns = Object.entries(recordset.columns).map(([name, metadata]) => ({ name, typeName: String((metadata.type as { declaration?: string; name?: string }).declaration ?? (metadata.type as { name?: string }).name ?? '').toUpperCase() }));
  if (new Set(columns.map(column => column.name)).size !== columns.length || columns.some(column => !column.name || !column.typeName)) throw new TypeError('SQL Server returned invalid column metadata');
  const rows: Array<Record<string, unknown>> = [];
  let bytes = 2;
  for (const row of recordset) {
    if (rows.length >= maxRows) return { columns, rows, truncated: true };
    const decoded: Record<string, unknown> = {};
    for (const column of columns) {
      const value = row[column.name];
      decoded[column.name] = value instanceof Date ? value.toISOString().replace(/Z$/, '') : Buffer.isBuffer(value) ? value.toString('base64') : typeof value === 'bigint' ? value.toString() : value;
    }
    const rowBytes = Buffer.byteLength(JSON.stringify(decoded), 'utf8') + (rows.length ? 1 : 0);
    if (bytes + rowBytes > maxResponseBytes) return { columns, rows, truncated: true };
    bytes += rowBytes;
    rows.push(decoded);
  }
  return { columns, rows, truncated: false };
}

function queryFailure(client: SQLServerClient, call: Call, classification: ClassifiedFailure): QueryAttempt<RowSet> {
  const failure = client.failure('query', classification);
  if (classification.disposition === 'retry' || classification.disposition === 'uncertain') return newQueryRetry(failure, 0);
  const branch = classification.disposition === 'defect' ? 'defect' : classification.disposition === 'invalidResponse' ? 'invalidResponse' : 'providerRejected';
  return newQueryBranch(branch, emptyRows(), client.receipt(call), failure);
}
function executeFailure(client: SQLServerClient, call: Call, classification: ClassifiedFailure): MutationAttempt<StatementExecution> {
  const failure = client.failure('execute', classification);
  if (classification.disposition === 'retry') return newMutationRetry(failure, 0);
  if (classification.disposition === 'uncertain') return newMutationUncertain(emptyExecution(), failure, client.receipt(call));
  const branch = classification.disposition === 'defect' ? 'defect' : classification.disposition === 'invalidResponse' ? 'invalidResponse' : 'providerRejected';
  return newMutationBranch(branch, emptyExecution(), client.receipt(call), failure);
}

/** Read one bounded SELECT in a transaction that is always rolled back. */
export class QueryRowsOperation implements Query<QueryRowsInput, RowSet> {
  /** Bind the SQL Server client. */ constructor(private readonly client: SQLServerClient) {}
  /** Return the immutable manifest definition. */ definition() { return QueryRowsDefinition; }
  /** Run one query attempt. */
  async invoke(call: Call, input: QueryRowsInput): Promise<QueryAttempt<RowSet>> {
    try { validateStatement(input.statement, input.parameters ?? [], false); }
    catch { return queryFailure(this.client, call, { disposition: 'defect', kind: 'VALIDATION', message: 'the query statement or parameters are invalid' }); }
    let pool: sql.ConnectionPool;
    try { pool = await this.client.open(call); }
    catch (error) { return queryFailure(this.client, call, error instanceof TypeError ? { disposition: 'defect', kind: 'AUTHENTICATION', message: 'SQL Server connection credentials are unavailable' } : classifyDriverError(error, 'connect')); }
    let transaction: sql.Transaction | undefined;
    try {
      transaction = new sql.Transaction(pool);
      await transaction.begin(sql.ISOLATION_LEVEL.READ_COMMITTED);
      const request = transaction.request();
      bindParameters(request, input.parameters ?? []);
      const result = await request.query<Record<string, unknown>>(input.statement);
      const decoded = decodeRows(result, this.client.config.maxRows, this.client.config.maxResponseBytes);
      if (!decoded.columns.length) return queryFailure(this.client, call, { disposition: 'defect', kind: 'VALIDATION', message: 'the statement returned no result set' });
      const output: RowSet = { columns: decoded.columns, rows: decoded.rows, ...(decoded.truncated ? { truncated: true } : {}) };
      if (decoded.truncated) return newQueryBranch('truncated', output, this.client.receipt(call), this.client.failure('query', { disposition: 'rejected', kind: 'RESPONSE_TOO_LARGE', message: 'the statement returned more rows or bytes than maxRows or maxResponseBytes allow' }));
      return newQueryBranch('completed', output, this.client.receipt(call));
    } catch (error) { return queryFailure(this.client, call, classifyDriverError(error, transaction ? 'statement' : 'begin')); }
    finally {
      if (transaction) { try { await transaction.rollback(); } catch { /* Closing the pool rolls back a failed or completed transaction. */ } }
      await pool.close();
    }
  }
}

/** Write one statement in a transaction with explicit commit. */
export class ExecuteStatementOperation implements Mutation<ExecuteStatementInput, StatementExecution> {
  /** Bind the SQL Server client. */ constructor(private readonly client: SQLServerClient) {}
  /** Return the immutable manifest definition. */ definition() { return ExecuteStatementDefinition; }
  /** Reuse the stable Dex Call ID for an application-owned idempotency placeholder. */ idempotencyKey(callId: string): string { return callId; }
  /** Run one transactional write attempt. */
  async invoke(call: Call, input: ExecuteStatementInput): Promise<MutationAttempt<StatementExecution>> {
    const values = [...input.parameters ?? []];
    if (input.idempotencyKeyPlaceholder && input.idempotencyKeyPlaceholder !== values.length + 1) return executeFailure(this.client, call, { disposition: 'defect', kind: 'VALIDATION', message: 'idempotencyKeyPlaceholder must be the next parameter' });
    if (input.idempotencyKeyPlaceholder) values.push(call.idempotencyKey);
    try {
      if (input.maxRowsAffected !== undefined && input.maxRowsAffected < 0) throw new TypeError('negative maxRowsAffected');
      validateStatement(input.statement, values, true);
    } catch { return executeFailure(this.client, call, { disposition: 'defect', kind: 'VALIDATION', message: 'the execute statement or parameters are invalid' }); }
    let pool: sql.ConnectionPool;
    try { pool = await this.client.open(call); }
    catch (error) { return executeFailure(this.client, call, error instanceof TypeError ? { disposition: 'defect', kind: 'AUTHENTICATION', message: 'SQL Server connection credentials are unavailable' } : classifyDriverError(error, 'connect')); }
    let transaction: sql.Transaction | undefined;
    let committed = false;
    try {
      transaction = new sql.Transaction(pool);
      await transaction.begin(sql.ISOLATION_LEVEL.READ_COMMITTED);
      const request = transaction.request();
      bindParameters(request, values);
      const result = await request.query<Record<string, unknown>>(input.statement);
      const decoded = input.returnsOutputRows ? decodeRows(result, this.client.config.maxRows, this.client.config.maxResponseBytes) : { columns: [], rows: [], truncated: false };
      if (decoded.truncated) return newMutationBranch('limitExceeded', emptyExecution(), this.client.receipt(call), this.client.failure('execute', { disposition: 'rejected', kind: 'RESPONSE_TOO_LARGE', message: 'OUTPUT rows exceeded maxRows or maxResponseBytes, so the transaction was rolled back' }));
      if (input.returnsOutputRows && !decoded.columns.length) return executeFailure(this.client, call, { disposition: 'defect', kind: 'VALIDATION', message: 'returnsOutputRows is set but the statement returned no OUTPUT result set' });
      const rowsAffected = input.returnsOutputRows ? decoded.rows.length : result.rowsAffected.reduce((sum, count) => sum + count, 0);
      if (input.maxRowsAffected !== undefined && rowsAffected > input.maxRowsAffected) return newMutationBranch('limitExceeded', emptyExecution(), this.client.receipt(call), this.client.failure('execute', { disposition: 'rejected', kind: 'CONFLICT', message: `the statement affected ${rowsAffected} rows but maxRowsAffected is ${input.maxRowsAffected}, so the transaction was rolled back` }));
      try { await transaction.commit(); committed = true; }
      catch (error) { return executeFailure(this.client, call, classifyDriverError(error, 'commit')); }
      return newMutationBranch('completed', { rowsAffected, columns: decoded.columns, rows: decoded.rows }, this.client.receipt(call));
    } catch (error) { return executeFailure(this.client, call, classifyDriverError(error, transaction ? 'statement' : 'begin')); }
    finally {
      if (transaction && !committed) { try { await transaction.rollback(); } catch { /* Closing the pool rolls back when the transaction has already ended. */ } }
      await pool.close();
    }
  }
}
