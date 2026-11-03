// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import sql from 'mssql';
import { newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, type Call, type Mutation, type MutationAttempt, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import { ExecuteStatementDefinition, QueryRowsDefinition } from './generated/definitions.js';
import type { Column, ExecuteStatementInput, QueryRowsInput, RowSet, StatementExecution } from './generated/models.js';
import { classifyDriverError, type ClassifiedFailure, type SQLServerClient } from './client.js';

const emptyRows = (): RowSet => ({ columns: [], rows: [] });
const emptyExecution = (): StatementExecution => ({ rowsAffected: 0, columns: [], rows: [] });

const deniedKeywords = new Set(`BEGIN COMMIT ROLLBACK SAVE TRAN TRANSACTION DISTRIBUTED EXEC EXECUTE USE SETUSER REVERT IF WHILE GOTO RETURN BREAK CONTINUE WAITFOR DECLARE DEALLOCATE CREATE ALTER DROP TRUNCATE GRANT DENY REVOKE TRIGGER STATISTICS BACKUP RESTORE DBCC KILL SHUTDOWN RECONFIGURE CHECKPOINT LOAD DUMP BULK OPENROWSET OPENDATASOURCE OPENQUERY ROWCOUNT TEXTSIZE IDENTITY_INSERT PRINT RAISERROR READTEXT WRITETEXT UPDATETEXT`.split(' '));
type Token = { kind: 'word' | 'variable' | 'semicolon' | 'open' | 'other'; text: string };

function tokenizeStatement(statement: string): Token[] {
  const tokens: Token[] = [];
  for (let index = 0; index < statement.length;) {
    const rest = statement.slice(index);
    const character = String.fromCodePoint(statement.codePointAt(index)!);
    if (/^\s$/u.test(character)) { index += character.length; continue; }
    if (rest.startsWith('--')) { const end = rest.search(/[\r\n]/); index += end < 0 ? rest.length : end; continue; }
    if (rest.startsWith('/*')) {
      let depth = 0;
      do {
        if (statement.startsWith('/*', index)) { depth++; index += 2; }
        else if (statement.startsWith('*/', index)) { depth--; index += 2; }
        else index++;
        if (index >= statement.length && depth) throw new TypeError('unterminated SQL block comment');
      } while (depth);
      continue;
    }
    if (character === "'" || character === '[' || character === '"') {
      const closing = character === '[' ? ']' : character;
      index += character.length;
      let found = false;
      while (index < statement.length) {
        if (statement[index] === closing) {
          index++;
          if (statement[index] === closing) { index++; continue; }
          found = true; break;
        }
        index++;
      }
      if (!found) throw new TypeError('unterminated SQL literal or identifier');
      continue;
    }
    if (character === '@' || /[\p{L}_#$\p{N}]/u.test(character)) {
      const match = rest.match(/^[\p{L}\p{N}_#$@]+/u)!;
      tokens.push({ kind: character === '@' ? 'variable' : /[\p{L}_#$]/u.test(character) ? 'word' : 'other', text: match[0] });
      index += match[0].length;
      continue;
    }
    tokens.push({ kind: character === ';' ? 'semicolon' : character === '(' ? 'open' : 'other', text: character });
    index += character.length;
  }
  return tokens;
}

/** Validate one bounded, positional SQL statement and its parameter values. */
export function validateStatement(statement: string, parameters: unknown[], isWrite: boolean): void {
  if (!statement?.trim() || Buffer.byteLength(statement, 'utf8') > 1048576 || statement.includes('\0') || /[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(statement)) throw new TypeError('one valid SQL statement is required');
  if (parameters.length > 2098) throw new TypeError('too many SQL parameters');
  const tokens = tokenizeStatement(statement);
  const leading = tokens.find(token => token.kind !== 'open');
  if (leading?.kind !== 'word' || !(isWrite ? ['INSERT', 'UPDATE', 'DELETE', 'MERGE', 'WITH'] : ['SELECT', 'WITH']).includes(leading.text.toUpperCase())) throw new TypeError('statement starts with the wrong SQL operation');
  const referenced = new Set<number>();
  for (let index = 0; index < tokens.length; index++) {
    const token = tokens[index]!;
    if (token.kind === 'semicolon' && tokens.slice(index + 1).some(next => next.kind !== 'semicolon')) throw new TypeError('more than one SQL statement is not allowed');
    if (token.kind === 'word') {
      const word = token.text.toUpperCase();
      if (deniedKeywords.has(word) && !(word === 'USE' && tokens[index + 1]?.kind === 'word' && ['HINT', 'PLAN'].includes(tokens[index + 1]!.text.toUpperCase()))) throw new TypeError(`SQL keyword ${word} is not allowed`);
    }
    if (token.kind === 'variable' && !token.text.startsWith('@@')) {
      if (!/^@p[1-9][0-9]*$/.test(token.text)) throw new TypeError('statement parameters must use lowercase @pN without leading zeros');
      const position = Number(token.text.slice(2));
      if (!Number.isSafeInteger(position) || position > parameters.length) throw new TypeError('unbound SQL parameter');
      referenced.add(position);
    }
  }
  if (referenced.size !== parameters.length) throw new TypeError('each SQL parameter must be referenced');
  let combinedBytes = 0;
  for (const value of parameters) {
    if (value === null) continue;
    if (typeof value === 'string') combinedBytes += Buffer.byteLength(value, 'utf8');
    else if (typeof value === 'boolean') combinedBytes++;
    else if (typeof value === 'number' && Number.isFinite(value) && (!Number.isInteger(value) || Number.isSafeInteger(value))) combinedBytes += 8;
    else if (value instanceof Date && Number.isFinite(value.getTime()) && value.getUTCFullYear() >= 1 && value.getUTCFullYear() <= 9999) combinedBytes += 10;
    else if (Buffer.isBuffer(value)) combinedBytes += value.length;
    else throw new TypeError('unsupported or out-of-range SQL parameter');
    if (combinedBytes > 16 * 1048576) throw new TypeError('SQL parameters exceed 16 MiB combined');
  }
}

/** Bind validated JavaScript values to explicit SQL Server types. */
export function bindParameters(request: sql.Request, values: unknown[]): void {
  values.forEach((value, index) => {
    const name = `p${index + 1}`;
    if (value === null || typeof value === 'string') request.input(name, sql.NVarChar(sql.MAX), value);
    else if (typeof value === 'boolean') request.input(name, sql.Bit, value);
    else if (typeof value === 'number') request.input(name, Number.isSafeInteger(value) ? sql.BigInt : sql.Float, value);
    else if (value instanceof Date) request.input(name, sql.DateTime2(7), new Date(value.getTime()));
    else if (Buffer.isBuffer(value)) request.input(name, sql.VarBinary(sql.MAX), value);
  });
}

/** An exact SQL value cannot be represented by the native driver. */
export class InvalidResultError extends Error {
  constructor(columnName: string, typeName: string) {
    super(`column ${JSON.stringify(columnName)} of type ${typeName} returned a value the connector cannot decode`);
  }
}

/** A result shape the connector cannot represent safely. */
export class ResultShapeError extends Error {}

function formatTemporal(value: Date, typeName: string): string {
  if (!Number.isFinite(value.getTime())) throw new TypeError('invalid temporal value');
  const iso = value.toISOString();
  if (typeName === 'DATE') return iso.slice(0, 10);
  const nanosecondsDelta = (value as Date & { nanosecondsDelta?: number }).nanosecondsDelta ?? 0;
  const subMillisecondDigits = Math.round(nanosecondsDelta * 10_000_000);
  if (subMillisecondDigits < 0 || subMillisecondDigits > 9999) throw new TypeError('invalid temporal fraction');
  const fractional = `${iso.slice(20, 23)}${String(subMillisecondDigits).padStart(4, '0')}`.replace(/0+$/, '');
  const time = `${iso.slice(11, 19)}${fractional ? `.${fractional}` : ''}`;
  return typeName === 'TIME' ? time : `${iso.slice(0, 10)}T${time}`;
}

function decodeColumnValue(value: unknown, typeName: string, scale: number | undefined): unknown {
  if (value === null) return null;
  if (typeName === 'DECIMAL') {
    if (typeof value !== 'number' || !Number.isFinite(value) || scale === undefined || scale < 0 || scale > 15) throw new TypeError('invalid decimal');
    return value.toFixed(scale);
  }
  if (typeName === 'REAL') {
    if (typeof value !== 'number' || !Number.isFinite(value)) throw new TypeError('invalid real');
    const float32 = Math.fround(value);
    if (!Number.isFinite(float32)) throw new TypeError('invalid real');
    for (let significantDigits = 1; significantDigits <= 9; significantDigits++) {
      const rounded = Number(float32.toPrecision(significantDigits));
      if (Math.fround(rounded) === float32) return rounded;
    }
    throw new TypeError('invalid real');
  }
  if (['DATE', 'TIME', 'DATETIME', 'SMALLDATETIME', 'DATETIME2'].includes(typeName)) {
    if (!(value instanceof Date)) throw new TypeError('invalid temporal value');
    return formatTemporal(value, typeName);
  }
  if (typeName === 'UNIQUEIDENTIFIER') {
    if (typeof value !== 'string') throw new TypeError('invalid uniqueidentifier');
    return value.toLowerCase();
  }
  if (typeName === 'BIGINT') {
    if (typeof value !== 'string' && typeof value !== 'bigint') throw new TypeError('invalid bigint');
    return String(value);
  }
  if (['TINYINT', 'SMALLINT', 'INT', 'FLOAT'].includes(typeName)) {
    if (typeof value !== 'number' || !Number.isFinite(value)) throw new TypeError('invalid number');
    return value;
  }
  if (typeName === 'BIT') {
    if (typeof value !== 'boolean') throw new TypeError('invalid bit');
    return value;
  }
  if (['CHAR', 'VARCHAR', 'TEXT', 'NCHAR', 'NVARCHAR', 'NTEXT', 'XML'].includes(typeName)) {
    if (typeof value !== 'string') throw new TypeError('invalid text');
    return value;
  }
  if (['BINARY', 'VARBINARY', 'IMAGE'].includes(typeName)) {
    if (!Buffer.isBuffer(value)) throw new TypeError('invalid binary');
    return value.toString('base64');
  }
  if (Buffer.isBuffer(value) && !['VARIANT', 'SQL_VARIANT', 'UDT'].includes(typeName)) return value.toString('base64');
  throw new TypeError('unsupported SQL type');
}

type ColumnMetadata = { name: string; typeName: string; precision: number | undefined; scale: number | undefined };

function describeColumns(rawColumns: unknown): ColumnMetadata[] {
  const details = Array.isArray(rawColumns) ? rawColumns : Object.values(rawColumns as Record<string, unknown>);
  const seen = new Set<string>();
  return details.map((raw, index) => {
    const column = raw as { name: string; type: { name?: string }; precision?: number; scale?: number; udt?: { name?: string } };
    const name = column.name;
    if (!name) throw new ResultShapeError(`result column ${index + 1} has no name; give every result column an alias with AS`);
    if (seen.has(name)) throw new ResultShapeError(`statement returns column ${JSON.stringify(name)} more than once; give each result column a unique alias`);
    seen.add(name);
    const driverTypeName = (column.udt?.name ?? column.type?.name ?? '').toUpperCase();
    const typeName = driverTypeName === 'NUMERIC' ? 'DECIMAL' : driverTypeName;
    if (!typeName) throw new ResultShapeError(`result column ${index + 1} has no SQL type`);
    if (['MONEY', 'SMALLMONEY', 'DATETIMEOFFSET', 'VARIANT', 'SQL_VARIANT', 'GEOGRAPHY', 'GEOMETRY', 'HIERARCHYID', 'UDT'].includes(typeName) || typeName === 'DECIMAL' && (column.precision === undefined || column.precision < 1 || column.precision > 15 || column.scale === undefined || column.scale < 0 || column.scale > column.precision)) throw new InvalidResultError(name, typeName);
    return { name, typeName, precision: column.precision, scale: column.scale };
  });
}

function decodeRow(row: unknown, metadata: ColumnMetadata[]): Record<string, unknown> {
  const decoded: Record<string, unknown> = {};
  metadata.forEach((column, index) => {
    const value = Array.isArray(row) ? row[index] : (row as Record<string, unknown>)[column.name];
    try { decoded[column.name] = decodeColumnValue(value, column.typeName, column.scale); }
    catch { throw new InvalidResultError(column.name, column.typeName); }
  });
  return decoded;
}

/** Decode an already collected driver result for fixture and compatibility checks. */
export function decodeRows(result: sql.IResult<Record<string, unknown>>, maxRows: number, maxResponseBytes: number): { columns: Column[]; rows: Array<Record<string, unknown>>; truncated: boolean } {
  if (result.recordsets.length > 1) throw new ResultShapeError('the statement returned more than one result set; run one statement per call');
  const recordset = result.recordset;
  if (!recordset) return { columns: [], rows: [], truncated: false };
  const metadata = describeColumns(recordset.columns);
  const columns = metadata.map(({ name, typeName }) => ({ name, typeName }));
  const rows: Array<Record<string, unknown>> = [];
  let bytes = 2;
  for (const row of recordset) {
    if (rows.length >= maxRows) return { columns, rows, truncated: true };
    const decoded = decodeRow(row, metadata);
    const rowBytes = Buffer.byteLength(JSON.stringify(decoded), 'utf8') + 1;
    if (bytes + rowBytes > maxResponseBytes) return { columns, rows, truncated: true };
    bytes += rowBytes;
    rows.push(decoded);
  }
  return { columns, rows, truncated: false };
}

class BoundedResultCollector {
  private metadata: ColumnMetadata[] = [];
  private resultSetCount = 0;
  private bytes = 2;
  private failure: unknown;
  readonly rows: Array<Record<string, unknown>> = [];
  readonly rowsAffected: number[] = [];
  isTruncated = false;

  constructor(private readonly request: sql.Request, private readonly maxRows: number, private readonly maxResponseBytes: number) {}

  async run(statement: string): Promise<{ columns: Column[]; rows: Array<Record<string, unknown>>; truncated: boolean; rowsAffected: number[] }> {
    this.request.stream = true;
    this.request.arrayRowMode = true;
    this.request.on('recordset', columns => this.onRecordset(columns));
    this.request.on('row', row => this.onRow(row));
    this.request.on('rowsaffected', count => this.rowsAffected.push(count));
    this.request.on('error', error => this.onError(error));
    try { await this.request.query(statement); }
    catch (error) { if (!this.isTruncated) throw error; }
    if (this.failure) throw this.failure;
    return { columns: this.metadata.map(({ name, typeName }) => ({ name, typeName })), rows: this.rows, truncated: this.isTruncated, rowsAffected: this.rowsAffected };
  }

  private onRecordset(columns: unknown): void {
    this.resultSetCount++;
    if (this.resultSetCount > 1) { this.stop(new ResultShapeError('the statement returned more than one result set; run one statement per call')); return; }
    try { this.metadata = describeColumns(columns); }
    catch (error) { this.stop(error); }
  }

  private onRow(row: unknown): void {
    if (this.failure || this.isTruncated) return;
    if (this.rows.length >= this.maxRows) { this.isTruncated = true; this.cancel(); return; }
    try {
      const decoded = decodeRow(row, this.metadata);
      const rowBytes = Buffer.byteLength(JSON.stringify(decoded), 'utf8') + 1;
      if (this.bytes + rowBytes > this.maxResponseBytes) { this.isTruncated = true; this.cancel(); return; }
      this.bytes += rowBytes;
      this.rows.push(decoded);
    } catch (error) { this.stop(error); }
  }

  private onError(error: unknown): void {
    if (this.isTruncated && (error as { code?: string }).code === 'ECANCEL') return;
    if (!this.failure) this.failure = error;
  }

  private stop(error: unknown): void {
    this.failure = error;
    this.cancel();
  }

  private cancel(): void {
    try { this.request.cancel(); }
    catch (error) { if (!this.failure) this.failure = error; }
  }
}

function queryFailure(client: SQLServerClient, call: Call, classification: ClassifiedFailure, pool?: sql.ConnectionPool): QueryAttempt<RowSet> {
  const failure = client.failure('query', classification);
  if (classification.disposition === 'retry' || classification.disposition === 'uncertain') return newQueryRetry(failure, 0);
  const branch = classification.disposition === 'defect' ? 'defect' : classification.disposition === 'invalidResponse' ? 'invalidResponse' : 'providerRejected';
  return newQueryBranch(branch, emptyRows(), client.receipt(call, pool, classification), failure);
}
function executeFailure(client: SQLServerClient, call: Call, classification: ClassifiedFailure, pool?: sql.ConnectionPool): MutationAttempt<StatementExecution> {
  const failure = client.failure('execute', classification);
  if (classification.disposition === 'retry') return newMutationRetry(failure, 0);
  if (classification.disposition === 'uncertain') return newMutationUncertain(emptyExecution(), failure, client.receipt(call, pool, classification));
  const branch = classification.disposition === 'defect' ? 'defect' : classification.disposition === 'invalidResponse' ? 'invalidResponse' : 'providerRejected';
  return newMutationBranch(branch, emptyExecution(), client.receipt(call, pool, classification), failure);
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
    let phase: 'begin' | 'statement' = 'begin';
    try {
      transaction = new sql.Transaction(pool);
      await transaction.begin(sql.ISOLATION_LEVEL.READ_COMMITTED);
      await this.client.pinSession(transaction, pool);
      phase = 'statement';
      const request = transaction.request();
      bindParameters(request, input.parameters ?? []);
      const decoded = await new BoundedResultCollector(request, this.client.config.maxRows, this.client.config.maxResponseBytes).run(input.statement);
      if (!decoded.columns.length) return queryFailure(this.client, call, { disposition: 'defect', kind: 'VALIDATION', message: 'the statement returned no result set' }, pool);
      const output: RowSet = { columns: decoded.columns, rows: decoded.rows, ...(decoded.truncated ? { truncated: true } : {}) };
      if (decoded.truncated) return newQueryBranch('truncated', output, this.client.receipt(call, pool), this.client.failure('query', { disposition: 'rejected', kind: 'RESPONSE_TOO_LARGE', message: 'the statement returned more rows or bytes than maxRows or maxResponseBytes allow' }));
      return newQueryBranch('completed', output, this.client.receipt(call, pool));
    } catch (error) { return queryFailure(this.client, call, error instanceof InvalidResultError ? { disposition: 'invalidResponse', kind: 'PROTOCOL', message: error.message } : error instanceof ResultShapeError ? { disposition: 'defect', kind: 'VALIDATION', message: error.message } : classifyDriverError(error, phase), pool); }
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
      if (input.maxRowsAffected !== undefined && (!Number.isSafeInteger(input.maxRowsAffected) || input.maxRowsAffected < 0)) throw new TypeError('invalid maxRowsAffected');
      validateStatement(input.statement, values, true);
    } catch { return executeFailure(this.client, call, { disposition: 'defect', kind: 'VALIDATION', message: 'the execute statement or parameters are invalid' }); }
    let pool: sql.ConnectionPool;
    try { pool = await this.client.open(call); }
    catch (error) { return executeFailure(this.client, call, error instanceof TypeError ? { disposition: 'defect', kind: 'AUTHENTICATION', message: 'SQL Server connection credentials are unavailable' } : classifyDriverError(error, 'connect')); }
    let transaction: sql.Transaction | undefined;
    let phase: 'begin' | 'statement' = 'begin';
    let committed = false;
    try {
      transaction = new sql.Transaction(pool);
      await transaction.begin(sql.ISOLATION_LEVEL.READ_COMMITTED);
      await this.client.pinSession(transaction, pool);
      phase = 'statement';
      const request = transaction.request();
      bindParameters(request, values);
      const decoded = await new BoundedResultCollector(request, this.client.config.maxRows, this.client.config.maxResponseBytes).run(input.statement);
      if (decoded.truncated) return newMutationBranch('limitExceeded', emptyExecution(), this.client.receipt(call, pool), this.client.failure('execute', { disposition: 'rejected', kind: 'RESPONSE_TOO_LARGE', message: 'OUTPUT rows exceeded maxRows or maxResponseBytes, so the transaction was rolled back' }));
      if (input.returnsOutputRows && !decoded.columns.length) return executeFailure(this.client, call, { disposition: 'defect', kind: 'VALIDATION', message: 'returnsOutputRows is set but the statement returned no OUTPUT result set' }, pool);
      if (!input.returnsOutputRows && decoded.columns.length) return executeFailure(this.client, call, { disposition: 'defect', kind: 'VALIDATION', message: 'the statement returned output rows but returnsOutputRows is not set' }, pool);
      const rowsAffected = input.returnsOutputRows ? decoded.rows.length : decoded.rowsAffected.reduce((sum, count) => sum + count, 0);
      if (input.maxRowsAffected !== undefined && rowsAffected > input.maxRowsAffected) return newMutationBranch('limitExceeded', emptyExecution(), this.client.receipt(call, pool), this.client.failure('execute', { disposition: 'rejected', kind: 'CONFLICT', message: `the statement affected ${rowsAffected} rows but maxRowsAffected is ${input.maxRowsAffected}, so the transaction was rolled back` }));
      if (!pool.connected || call.context.cancellationSignal.aborted) return executeFailure(this.client, call, { disposition: 'retry', kind: 'TRANSPORT', message: 'COMMIT was not sent because the connection closed' }, pool);
      try { await transaction.commit(); committed = true; }
      catch (error) { return executeFailure(this.client, call, classifyDriverError(error, 'commit'), pool); }
      return newMutationBranch('completed', { rowsAffected, columns: decoded.columns, rows: decoded.rows }, this.client.receipt(call, pool));
    } catch (error) { return executeFailure(this.client, call, error instanceof InvalidResultError ? { disposition: 'invalidResponse', kind: 'PROTOCOL', message: error.message } : error instanceof ResultShapeError ? { disposition: 'defect', kind: 'VALIDATION', message: error.message } : classifyDriverError(error, phase), pool); }
    finally {
      if (transaction && !committed) { try { await transaction.rollback(); } catch { /* Closing the pool rolls back when the transaction has already ended. */ } }
      await pool.close();
    }
  }
}
