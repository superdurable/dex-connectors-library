// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Client as PgClient, FieldDef, QueryResult } from 'pg';
import Cursor from 'pg-cursor';
import { newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, zeroReceipt, type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import type { Column, ExecuteStatementInput, QueryRowsInput, RowSet, StatementExecution } from './generated/models.js';
import { ExecuteStatementDefinition, QueryRowsDefinition } from './generated/definitions.js';
import type { PostgreSQLClient } from './client.js';
import { classifyFailure, type ClassifiedFailure, type Phase } from './failures.js';

const typeNames: Record<number, string> = { 16: 'bool', 17: 'bytea', 18: 'char', 19: 'name', 20: 'int8', 21: 'int2', 23: 'int4', 25: 'text', 26: 'oid', 114: 'json', 142: 'xml', 650: 'cidr', 700: 'float4', 701: 'float8', 790: 'money', 829: 'macaddr', 869: 'inet', 1042: 'bpchar', 1043: 'varchar', 1082: 'date', 1083: 'time', 1114: 'timestamp', 1184: 'timestamptz', 1186: 'interval', 1266: 'timetz', 1560: 'bit', 1562: 'varbit', 1700: 'numeric', 2950: 'uuid', 3802: 'jsonb', 4072: 'jsonpath' };
const emptyRows = (): RowSet => ({ columns: [], rows: [] });
const emptyExecution = (): StatementExecution => ({ command: '', rowsAffected: 0, columns: [], rows: [] });

class UndecodableValueError extends Error {
  readonly safeMessage: string;
  constructor(column: Column) { super('undecodable PostgreSQL column'); this.name = 'UndecodableValueError'; this.safeMessage = `column ${JSON.stringify(column.name)} with type OID ${column.typeOid} returned text the connector cannot decode`; }
}

class StatementDescriptionError extends Error { constructor() { super('PostgreSQL statement description is invalid'); this.name = 'StatementDescriptionError'; } }

function failure(operation: 'query' | 'execute', classified: ClassifiedFailure): Failure {
  return { kind: classified.kind, provider: 'postgresql', operation, message: classified.message };
}

function validateStatement(statement: string, parameters: Array<unknown | null> | undefined): Array<string | null> {
  if (!statement?.trim() || Buffer.byteLength(statement, 'utf8') > 1 << 20 || statement.includes('\0') || Buffer.from(statement, 'utf8').toString('utf8') !== statement) throw new TypeError('statement must be non-blank valid SQL without NUL and at most 1048576 bytes');
  let remaining = statement.trimStart();
  while (remaining.startsWith('--') || remaining.startsWith('/*')) {
    if (remaining.startsWith('--')) { const end = remaining.indexOf('\n'); remaining = end < 0 ? '' : remaining.slice(end + 1).trimStart(); }
    else {
      let depth = 0; let index = 0;
      for (; index < remaining.length - 1; index++) {
        if (remaining.slice(index, index + 2) === '/*') { depth++; index++; }
        else if (remaining.slice(index, index + 2) === '*/') { depth--; index++; if (depth === 0) break; }
      }
      remaining = depth === 0 ? remaining.slice(index + 1).trimStart() : '';
    }
  }
  const keyword = /^([\p{L}_]+)/u.exec(remaining)?.[1]?.toUpperCase() ?? '';
  if (['ABORT', 'BEGIN', 'COMMIT', 'END', 'PREPARE', 'RELEASE', 'ROLLBACK', 'SAVEPOINT', 'START', 'COPY'].includes(keyword)) throw new TypeError(`statement cannot start with ${keyword}`);
  const values = parameters ?? [];
  if (values.length > 65535) throw new TypeError('statement cannot bind more than 65535 parameters');
  let bytes = 0;
  const encoded = values.map((value, index) => {
    let text: string | null;
    if (value === null) text = null;
    else if (typeof value === 'string') text = value;
    else if (typeof value === 'boolean') text = String(value);
    else if (typeof value === 'number') text = String(value);
    else if (value instanceof Date && Number.isFinite(value.getTime())) text = value.toISOString();
    else if (value instanceof Uint8Array) text = `\\x${Buffer.from(value).toString('hex')}`;
    else throw new TypeError(`parameter $${index + 1} has an unsupported type`);
    if (text?.includes('\0') || (text && Buffer.from(text, 'utf8').toString('utf8') !== text)) throw new TypeError(`parameter $${index + 1} is not valid UTF-8 text`);
    bytes += text === null ? 0 : Buffer.byteLength(text, 'utf8');
    if (bytes > 16 << 20) throw new TypeError('parameters exceed 16777216 bytes combined');
    return text;
  });
  if (placeholderCount(statement) !== encoded.length) throw new TypeError('statement placeholder count differs from bound parameters');
  return encoded;
}

function placeholderCount(statement: string): number {
  let highest = 0;
  for (let index = 0; index < statement.length;) {
    const pair = statement.slice(index, index + 2);
    if (pair === '--') { const end = statement.indexOf('\n', index + 2); index = end < 0 ? statement.length : end + 1; continue; }
    if (pair === '/*') {
      let depth = 1; index += 2;
      while (index < statement.length && depth > 0) {
        if (statement.slice(index, index + 2) === '/*') { depth++; index += 2; }
        else if (statement.slice(index, index + 2) === '*/') { depth--; index += 2; }
        else index++;
      }
      continue;
    }
    if (statement[index] === '\'' || statement[index] === '"') {
      const quote = statement[index]; index++;
      while (index < statement.length) {
        if (statement[index] === quote) { if (statement[index + 1] === quote) index += 2; else { index++; break; } }
        else if (statement[index] === '\\' && quote === '\'' && /[eE]/.test(statement[index - 2] ?? '')) index += 2;
        else index++;
      }
      continue;
    }
    if (statement[index] === '$') {
      const remaining = statement.slice(index);
      const tag = /^\$(?:[A-Za-z_][A-Za-z0-9_]*)?\$/.exec(remaining)?.[0];
      if (tag) { const end = statement.indexOf(tag, index + tag.length); index = end < 0 ? statement.length : end + tag.length; continue; }
      const placeholder = /^\$([1-9][0-9]*)/.exec(remaining);
      if (placeholder) { highest = Math.max(highest, Number(placeholder[1])); index += placeholder[0].length; continue; }
    }
    index++;
  }
  return highest;
}

function describeColumns(fields: readonly FieldDef[]): Column[] {
  const seen = new Set<string>();
  return fields.map(field => {
    if (seen.has(field.name)) throw new StatementDescriptionError();
    seen.add(field.name);
    const typeName = typeNames[field.dataTypeID];
    return { name: field.name, typeOid: field.dataTypeID, ...(typeName ? { typeName } : {}) };
  });
}

function decodeValue(column: Column, value: unknown): unknown | null {
  if (value === null) return null;
  if (typeof value !== 'string') throw new UndecodableValueError(column);
  switch (column.typeOid) {
    case 16: if (value === 't') return true; if (value === 'f') return false; break;
    case 21: case 23: if (/^-?\d+$/.test(value) && Number.isInteger(Number(value)) && Number(value) >= -2147483648 && Number(value) <= 2147483647) return Number(value); break;
    case 20: if (/^-?\d+$/.test(value)) return value; break;
    case 700: case 701: if (['NaN', 'Infinity', '-Infinity'].includes(value)) return value; if (value.trim() && Number.isFinite(Number(value))) return Number(value); break;
    case 1700: if (/^(?:-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?|NaN|-?Infinity)$/.test(value)) return value; break;
    case 17: if (/^\\x(?:[0-9a-fA-F]{2})*$/.test(value)) return Buffer.from(value.slice(2), 'hex').toString('base64'); break;
    case 114: case 3802: try { return JSON.parse(value) as unknown; } catch { break; }
    case 1184: {
      if (value === 'infinity' || value === '-infinity' || value.endsWith(' BC') || /^\d{5,}-/.test(value)) return value;
      const parsed = new Date(value.replace(' ', 'T'));
      if (!Number.isNaN(parsed.getTime())) return parsed.toISOString().replace(/\.000Z$/, 'Z');
      break;
    }
    case 1114: if (value === 'infinity' || value === '-infinity' || value.endsWith(' BC') || /^\d{5,}-/.test(value)) return value; if (/^\d{4}-\d\d-\d\d \d\d:\d\d:\d\d(?:\.\d+)?$/.test(value)) return value.replace(' ', 'T'); break;
    default: return value;
  }
  throw new UndecodableValueError(column);
}

async function begin(connection: PgClient, readOnly: boolean, statementTimeoutMs: number): Promise<void> {
  await connection.query(readOnly ? 'begin transaction read only' : 'begin');
  for (const setting of [`set local statement_timeout = ${Math.ceil(statementTimeoutMs)}`, "set local idle_in_transaction_session_timeout = 30000", "set local timezone = 'UTC'", "set local datestyle = 'ISO, MDY'", "set local intervalstyle = 'iso_8601'", "set local bytea_output = 'hex'", 'set local extra_float_digits = 3']) await connection.query(setting);
}

async function readRows(connection: PgClient, statement: string, values: Array<string | null>, maxRows: number, maxResponseBytes: number): Promise<{ columns: Column[]; rows: Array<Record<string, unknown | null>>; truncated: boolean; command: string; rowCount: number }> {
  const cursor = connection.query(new Cursor<unknown[]>(statement, values, { rowMode: 'array', types: { getTypeParser: () => (text: string) => text } }));
  const readBatch = (count: number): Promise<{ result: QueryResult; rows: unknown[][] }> => new Promise((resolve, reject) => {
    cursor.read(count, (error, rows, result) => { if (error) reject(error); else resolve({ result, rows }); });
  });
  let columns: Column[] | undefined;
  const rows: Array<Record<string, unknown | null>> = [];
  let bytes = 2;
  for (;;) {
    const batch = await readBatch(Math.min(100, maxRows - rows.length + 1));
    columns ??= describeColumns(batch.result.fields);
    if (batch.rows.length === 0) return { columns, rows, truncated: false, command: batch.result.command, rowCount: batch.result.rowCount ?? 0 };
    for (const rawRow of batch.rows) {
      if (rows.length === maxRows) {
        // Closing is best effort: the enclosing connection is discarded after this bounded read.
        await cursor.close().catch(() => undefined);
        return { columns, rows, truncated: true, command: batch.result.command, rowCount: batch.result.rowCount ?? 0 };
      }
      if (!Array.isArray(rawRow) || rawRow.length !== columns.length) throw new UndecodableValueError({ name: '*', typeOid: 0 });
      const row: Record<string, unknown | null> = {};
      columns.forEach((column, index) => { row[column.name] = decodeValue(column, rawRow[index]); });
      bytes += Buffer.byteLength(JSON.stringify(row), 'utf8') + 1;
      if (bytes > maxResponseBytes) {
        // Closing is best effort: the enclosing connection is discarded after this bounded read.
        await cursor.close().catch(() => undefined);
        return { columns, rows, truncated: true, command: batch.result.command, rowCount: batch.result.rowCount ?? 0 };
      }
      rows.push(row);
    }
  }
}

/** One read-only PostgreSQL statement using bound positional parameters. */
export class QueryRowsOperation implements Query<QueryRowsInput, RowSet> {
  /** Bind one configured client. */ constructor(private readonly client: PostgreSQLClient) {}
  /** Return the generated operation definition. */ definition() { return QueryRowsDefinition; }
  /** Run a bounded read in a transaction that never commits writes. */
  async invoke(call: Call, input: QueryRowsInput): Promise<QueryAttempt<RowSet>> {
    let values: Array<string | null>;
    try { values = validateStatement(input.statement, input.parameters); }
    catch { return newQueryBranch('defect', emptyRows(), zeroReceipt(), failure('query', { disposition: 'defect', kind: 'VALIDATION', message: 'PostgreSQL statement or parameters are invalid' })); }
    let connection: PgClient | null = null;
    let phase: Phase = 'connect';
    try {
      connection = await this.client.connect(call);
      if (!connection) return newQueryBranch('defect', emptyRows(), zeroReceipt(), failure('query', { disposition: 'defect', kind: 'AUTHENTICATION', message: 'PostgreSQL connection credentials are unavailable' }));
      phase = 'BEGIN';
      await begin(connection, true, this.client.configuration.statementTimeoutMs);
      phase = 'the statement';
      const read = await readRows(connection, input.statement, values, this.client.configuration.maxRows, this.client.configuration.maxResponseBytes);
      const value: RowSet = { columns: read.columns, rows: read.rows, ...(read.truncated ? { truncated: true } : {}) };
      return read.truncated ? newQueryBranch('truncated', value, this.client.receipt(call, connection), failure('query', { disposition: 'rejected', kind: 'RESPONSE_TOO_LARGE', message: 'the statement returned more rows or bytes than maxRows or maxResponseBytes allow' })) : newQueryBranch('completed', value, this.client.receipt(call, connection));
    } catch (error) {
      const classified = classifyFailure(error, phase);
      if (classified.disposition === 'retry') return newQueryRetry(failure('query', classified));
      const branch = classified.disposition === 'invalidResponse' ? 'invalidResponse' : classified.disposition === 'defect' ? 'defect' : 'providerRejected';
      return newQueryBranch(branch, emptyRows(), this.client.receipt(call, connection ?? undefined, classified), failure('query', classified));
    } finally {
      // Closing is best effort: PostgreSQL discards any uncommitted read-only transaction.
      if (connection) await connection.end().catch(() => undefined);
    }
  }
}

/** One transactional PostgreSQL write with a stable Dex idempotency key. */
export class ExecuteStatementOperation implements Mutation<ExecuteStatementInput, StatementExecution> {
  /** Bind one configured client. */ constructor(private readonly client: PostgreSQLClient) {}
  /** Return the generated operation definition. */ definition() { return ExecuteStatementDefinition; }
  /** Use the stable Dex Call ID as the optional bound deduplication value. */ idempotencyKey(callId: string): string { return callId; }
  /** Commit one bounded write; classify only a possibly received COMMIT as uncertain. */
  async invoke(call: Call, input: ExecuteStatementInput): Promise<MutationAttempt<StatementExecution>> {
    let values: Array<string | null>;
    try {
      const parameters = [...(input.parameters ?? [])];
      const placeholder = input.idempotencyKeyPlaceholder ?? 0;
      if (placeholder !== 0 && placeholder !== parameters.length + 1) throw new TypeError('idempotencyKeyPlaceholder must be the last parameter');
      if (placeholder) parameters.push(call.idempotencyKey ?? call.id);
      values = validateStatement(input.statement, parameters);
      if (input.maxRowsAffected !== undefined && (!Number.isSafeInteger(input.maxRowsAffected) || input.maxRowsAffected < 0)) throw new TypeError('maxRowsAffected cannot be negative');
    } catch { return newMutationBranch('defect', emptyExecution(), zeroReceipt(), failure('execute', { disposition: 'defect', kind: 'VALIDATION', message: 'PostgreSQL statement or parameters are invalid' })); }
    let connection: PgClient | null = null;
    let phase: Phase = 'connect';
    try {
      connection = await this.client.connect(call);
      if (!connection) return newMutationBranch('defect', emptyExecution(), zeroReceipt(), failure('execute', { disposition: 'defect', kind: 'AUTHENTICATION', message: 'PostgreSQL connection credentials are unavailable' }));
      phase = 'BEGIN';
      await begin(connection, false, this.client.configuration.statementTimeoutMs);
      phase = 'the statement';
      const read = await readRows(connection, input.statement, values, this.client.configuration.maxRows, this.client.configuration.maxResponseBytes);
      if (read.truncated) return newMutationBranch('limitExceeded', emptyExecution(), this.client.receipt(call, connection), failure('execute', { disposition: 'rejected', kind: 'RESPONSE_TOO_LARGE', message: 'RETURNING rows exceeded maxRows or maxResponseBytes, so the transaction was rolled back' }));
      if (input.maxRowsAffected !== undefined && read.rowCount > input.maxRowsAffected) return newMutationBranch('limitExceeded', emptyExecution(), this.client.receipt(call, connection), failure('execute', { disposition: 'rejected', kind: 'CONFLICT', message: `the statement affected ${read.rowCount} rows but maxRowsAffected is ${input.maxRowsAffected}, so the transaction was rolled back` }));
      phase = 'COMMIT';
      const driverState = connection as PgClient & { _queryable?: boolean; _ending?: boolean };
      if (driverState._queryable === false || driverState._ending === true) throw Object.assign(new Error('COMMIT not sent'), { name: 'CommitNotSentError' });
      const committed = await connection.query('COMMIT');
      if (committed.command !== 'COMMIT') return newMutationBranch('providerRejected', emptyExecution(), this.client.receipt(call, connection), failure('execute', { disposition: 'rejected', kind: 'PROVIDER_REJECTION', message: 'PostgreSQL rolled back the transaction at COMMIT' }));
      return newMutationBranch('completed', { command: read.command, rowsAffected: read.rowCount, columns: read.columns, rows: read.rows }, this.client.receipt(call, connection));
    } catch (error) {
      const classified = classifyFailure(error, phase);
      if (classified.disposition === 'retry') return newMutationRetry(failure('execute', classified));
      if (classified.disposition === 'uncertain') return newMutationUncertain(emptyExecution(), failure('execute', classified), this.client.receipt(call, connection ?? undefined, classified));
      const branch = classified.disposition === 'invalidResponse' ? 'invalidResponse' : classified.disposition === 'defect' ? 'defect' : 'providerRejected';
      return newMutationBranch(branch, emptyExecution(), this.client.receipt(call, connection ?? undefined, classified), failure('execute', classified));
    } finally {
      // Closing is best effort: PostgreSQL rolls back any transaction that was not committed.
      if (connection) await connection.end().catch(() => undefined);
    }
  }
}
