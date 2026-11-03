// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Connection as RawConnection, FieldPacket } from 'mysql2';
import type { Socket } from 'node:net';
import { newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt, type ReceiptDraft } from '@superdurable/connectors-sdk';
import type { Connection } from 'mysql2/promise';
import { ExecuteStatementDefinition, QueryRowsDefinition } from './generated/definitions.js';
import { ExecuteStatementInputSchema, QueryRowsInputSchema, type Column, type ExecuteStatementInput, type QueryRowsInput, type RowSet, type StatementExecution } from './generated/models.js';
import { classifyDriverError, type ClassifiedFailure, type SessionPhase } from './failures.js';
import { MySQLSessionSetupError, type MySQLClient } from './client.js';

const emptyRows = (): RowSet => ({ columns: [], rows: [] });
const emptyExecution = (): StatementExecution => ({ rowsAffected: 0, lastInsertId: '0', warningCount: 0, columns: [], rows: [] });
const readKeywords = new Set(['SELECT', 'WITH', 'TABLE', 'VALUES', 'SHOW', 'EXPLAIN', 'DESCRIBE', 'DESC']);
const writeKeywords = new Set(['INSERT', 'UPDATE', 'DELETE', 'REPLACE', 'WITH']);
type DriverConnection = RawConnection & { stream?: Socket };

class SessionDeadline {
  private readonly pendingFailure: Promise<never>;
  private readonly timer: NodeJS.Timeout;
  private rejectFailure!: (error: unknown) => void;
  private connection?: Connection;
  private expired = false;

  constructor(timeoutMs: number) {
    this.pendingFailure = new Promise((_, reject) => { this.rejectFailure = reject; });
    this.timer = setTimeout(() => {
      this.expired = true;
      this.destroySocket();
      this.rejectFailure(transportError('the MySQL call deadline expired'));
    }, Math.min(timeoutMs, 2147483647));
  }

  bind(connection: Connection): void {
    this.connection = connection;
    const rawConnection = (connection as Connection & { connection?: DriverConnection }).connection;
    // mysql2 emits connection errors for streaming commands without an onResult callback.
    rawConnection?.on?.('error', error => this.fail(error));
    if (this.expired) {
      this.destroySocket();
      throw transportError('the MySQL call deadline expired');
    }
  }

  wait<T>(operation: Promise<T>): Promise<T> { return Promise.race([operation, this.pendingFailure]); }

  close(): void {
    clearTimeout(this.timer);
    this.connection?.destroy();
  }

  private fail(error: unknown): void {
    if (this.expired) return;
    this.expired = true;
    clearTimeout(this.timer);
    this.rejectFailure(error);
  }

  private destroySocket(): void {
    const rawConnection = (this.connection as Connection & { connection?: DriverConnection } | undefined)?.connection;
    if (rawConnection?.stream?.destroy) rawConnection.stream.destroy();
    else this.connection?.destroy();
  }
}

function transportError(message: string, code = 'ETIMEDOUT'): Error & { code: string } {
  return Object.assign(new Error(message), { code });
}

/** Read a single parameterized statement in a read-only transaction. */
export class QueryRowsOperation implements Query<QueryRowsInput, RowSet> {
  /** Bind one client. */ constructor(private readonly client: MySQLClient) {}
  /** Stable query definition. */ definition() { return QueryRowsDefinition; }
  /** Run and classify one read operation. */
  async invoke(call: Call, input: QueryRowsInput): Promise<QueryAttempt<RowSet>> {
    const parameters = input.parameters ?? [];
    if (!QueryRowsInputSchema.safeParse(input).success || !validStatement(input.statement, readKeywords) || !validParameters(parameters) || countPlaceholders(input.statement) !== parameters.length) return queryDefect('query input or placeholder count is invalid');
    const password = await this.client.resolvePassword(call);
    if (!password) return queryDefect('MySQL connection credentials are unavailable', 'AUTHENTICATION');
    let connection: Connection | undefined;
    let connectionId = '', serverVersion = '';
    let phase: SessionPhase = 'connect';
    const deadline = new SessionDeadline(this.client.config.connectTimeoutMs + this.client.config.statementTimeoutMs + 5000);
    try {
      const session = await deadline.wait(this.client.openSession(password, connected => { phase = 'begin'; deadline.bind(connected); }));
      ({ connection, connectionId, serverVersion } = session);
      phase = 'begin';
      await deadline.wait(connection.query('START TRANSACTION READ ONLY'));
      phase = 'statement';
      const rowSet = await deadline.wait(readPreparedRows(connection, input.statement, parameters, this.client.config.maxRows, this.client.config.maxResponseBytes));
      const receipt = this.client.receipt(call, connectionId, serverVersion);
      if (rowSet.truncated) return newQueryBranch('truncated', rowSet, receipt, failure('query', 'RESPONSE_TOO_LARGE', 'the statement returned more rows or bytes than maxRows or maxResponseBytes allow'));
      return newQueryBranch('completed', rowSet, receipt);
    } catch (error) {
      if (error instanceof DuplicateColumnsError) return queryDefect(error.message);
      if (error instanceof InvalidRowsError) return newQueryBranch('invalidResponse', emptyRows(), this.client.receipt(call, connectionId, serverVersion), failure('query', 'PROTOCOL', error.message));
      if (error instanceof TypeError) return queryDefect(error.message);
      const classified = classifyDriverError(error instanceof MySQLSessionSetupError ? error.driverError : error, error instanceof MySQLSessionSetupError ? 'begin' : phase);
      const receipt = this.client.receipt(call, connectionId, serverVersion, classified.errorNumber, classified.sqlState);
      if (classified.disposition === 'retry' || classified.disposition === 'uncertain') return newQueryRetry(failure('query', classified.kind, classified.message));
      return newQueryBranch(classified.disposition, emptyRows(), receipt, failure('query', classified.kind, classified.message));
    } finally { deadline.close(); }
  }
}

/** Run a single transactional write and classify the COMMIT boundary. */
export class ExecuteStatementOperation implements Mutation<ExecuteStatementInput, StatementExecution> {
  /** Bind one client. */ constructor(private readonly client: MySQLClient) {}
  /** Stable mutation definition. */ definition() { return ExecuteStatementDefinition; }
  /** Use Dex's stable call ID as the write key. */ idempotencyKey(callId: string): string { return callId; }
  /** Run and classify one write operation. */
  async invoke(call: Call, input: ExecuteStatementInput): Promise<MutationAttempt<StatementExecution>> {
    const parameters = [...(input.parameters ?? [])];
    if (!ExecuteStatementInputSchema.safeParse(input).success || !validStatement(input.statement, writeKeywords) || !validParameters(parameters) || (input.maxRowsAffected !== undefined && (!Number.isSafeInteger(input.maxRowsAffected) || input.maxRowsAffected < 0)) || (input.idempotencyKeyPlaceholder !== undefined && input.idempotencyKeyPlaceholder !== 0 && input.idempotencyKeyPlaceholder !== parameters.length + 1) || countPlaceholders(input.statement) !== parameters.length + (input.idempotencyKeyPlaceholder ? 1 : 0)) return executeDefect('execute input or placeholder count is invalid');
    if (input.idempotencyKeyPlaceholder) parameters.push(call.idempotencyKey ?? call.id);
    const password = await this.client.resolvePassword(call);
    if (!password) return executeDefect('MySQL connection credentials are unavailable', 'AUTHENTICATION');
    let connection: Connection | undefined;
    let connectionId = '', serverVersion = '';
    let phase: SessionPhase = 'connect';
    const deadline = new SessionDeadline(this.client.config.connectTimeoutMs + this.client.config.statementTimeoutMs + 5000);
    try {
      const session = await deadline.wait(this.client.openSession(password, connected => { phase = 'begin'; deadline.bind(connected); }));
      ({ connection, connectionId, serverVersion } = session);
      phase = 'begin';
      await deadline.wait(connection.query('START TRANSACTION'));
      phase = 'statement';
      const rows = await deadline.wait(readPreparedRows(connection, input.statement, parameters, this.client.config.maxRows, this.client.config.maxResponseBytes));
      if (rows.truncated) return newMutationBranch('limitExceeded', emptyExecution(), this.client.receipt(call, connectionId, serverVersion), failure('execute', 'RESPONSE_TOO_LARGE', 'RETURNING rows exceeded maxRows or maxResponseBytes, so the transaction was rolled back'));
      const [outcome] = await deadline.wait(connection.query('SELECT ROW_COUNT(), LAST_INSERT_ID(), @@warning_count'));
      const outcomeRow = (outcome as unknown[][])[0];
      if (!outcomeRow || outcomeRow.length !== 3) throw new InvalidRowsError('the server returned an unreadable ROW_COUNT(), LAST_INSERT_ID(), or @@warning_count');
      const affected = Number(outcomeRow[0]) < 0 ? (rows.rows?.length ?? 0) : Number(outcomeRow[0]);
      const lastInsertId = String(outcomeRow[1]);
      const warningCount = Number(outcomeRow[2]);
      if (!Number.isSafeInteger(affected) || !/^\d+$/.test(lastInsertId) || !Number.isSafeInteger(warningCount)) throw new InvalidRowsError('the server returned an unreadable ROW_COUNT(), LAST_INSERT_ID(), or @@warning_count');
      if (input.maxRowsAffected !== undefined && affected > input.maxRowsAffected) return newMutationBranch('limitExceeded', emptyExecution(), this.client.receipt(call, connectionId, serverVersion), failure('execute', 'CONFLICT', `the statement affected ${affected} rows but maxRowsAffected is ${input.maxRowsAffected}, so the transaction was rolled back`));
      const rawConnection = (connection as Connection & { connection?: DriverConnection }).connection;
      if (rawConnection && rawConnection.state !== 'authenticated') return newMutationRetry(failure('execute', 'TRANSPORT', 'the connection closed before COMMIT could be sent'));
      phase = 'commit';
      await deadline.wait(connection.query('COMMIT'));
      return newMutationBranch('completed', { rowsAffected: affected, lastInsertId, warningCount, columns: rows.columns, rows: rows.rows }, this.client.receipt(call, connectionId, serverVersion));
    } catch (error) {
      if (error instanceof DuplicateColumnsError) return executeDefect(error.message);
      if (error instanceof InvalidRowsError) return newMutationBranch('invalidResponse', emptyExecution(), this.client.receipt(call, connectionId, serverVersion), failure('execute', 'PROTOCOL', error.message));
      if (error instanceof TypeError) return executeDefect(error.message);
      const classified = classifyDriverError(error instanceof MySQLSessionSetupError ? error.driverError : error, error instanceof MySQLSessionSetupError ? 'begin' : phase);
      const receipt = this.client.receipt(call, connectionId, serverVersion, classified.errorNumber, classified.sqlState);
      return mutationFailure(classified, receipt);
    } finally { deadline.close(); }
  }
}

function mutationFailure(classified: ClassifiedFailure, receipt: ReceiptDraft): MutationAttempt<StatementExecution> {
  const detailed = failure('execute', classified.kind, classified.message);
  if (classified.disposition === 'retry') return newMutationRetry(detailed);
  if (classified.disposition === 'uncertain') return newMutationUncertain(emptyExecution(), detailed, receipt);
  return newMutationBranch(classified.disposition, emptyExecution(), receipt, detailed);
}
function queryDefect(message: string, kind: Failure['kind'] = 'VALIDATION'): QueryAttempt<RowSet> { return newQueryBranch('defect', emptyRows(), {}, failure('query', kind, message)); }
function executeDefect(message: string, kind: Failure['kind'] = 'VALIDATION'): MutationAttempt<StatementExecution> { return newMutationBranch('defect', emptyExecution(), {}, failure('execute', kind, message)); }
function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { provider: 'mysql', operation, kind, message }; }

function validStatement(statement: string, allowed: Set<string>): boolean {
  if (!statement?.trim() || Buffer.byteLength(statement) > 1048576 || statement.includes('\0')) return false;
  const leading = statement.replace(/^\s*(?:(?:\/\*(?![!M])[\s\S]*?\*\/|#[^\n]*\n|--\s[^\n]*\n|\()+\s*)*/, '');
  const keyword = /^[A-Za-z_]+/.exec(leading)?.[0]?.toUpperCase() ?? '';
  return allowed.has(keyword);
}
function validParameters(parameters: unknown[]): boolean {
  if (!Array.isArray(parameters) || parameters.length > 65535) return false;
  let bytes = 0;
  for (const value of parameters) {
    if (value !== null && typeof value !== 'string' && typeof value !== 'boolean' && typeof value !== 'number' && !Buffer.isBuffer(value) && !(value instanceof Date)) return false;
    if (typeof value === 'number' && (!Number.isFinite(value) || (Number.isInteger(value) && !Number.isSafeInteger(value)))) return false;
    bytes += typeof value === 'string' ? Buffer.byteLength(value) : Buffer.isBuffer(value) ? value.byteLength : 8;
  }
  return bytes <= 16777216;
}

function countPlaceholders(statement: string): number {
  let count = 0;
  let quote = '';
  for (let index = 0; index < statement.length; index++) {
    const character = statement[index]!;
    const next = statement[index + 1];
    if (quote) {
      if (character === '\\') { index++; continue; }
      if (character === quote) { if (next === quote) index++; else quote = ''; }
      continue;
    }
    if (character === "'" || character === '"' || character === '`') { quote = character; continue; }
    if (character === '#') { while (index < statement.length && statement[index] !== '\n') index++; continue; }
    if (character === '-' && next === '-' && /\s/.test(statement[index + 2] ?? ' ')) { while (index < statement.length && statement[index] !== '\n') index++; continue; }
    if (character === '/' && next === '*') { index = statement.indexOf('*/', index + 2); if (index < 0) return count; index++; continue; }
    if (character === '?') count++;
  }
  return count;
}

class InvalidRowsError extends Error {}
class DuplicateColumnsError extends Error {}
async function readPreparedRows(connection: Connection, statement: string, parameters: unknown[], maxRows: number, maxBytes: number): Promise<RowSet> {
  const preparedParameters = parameters as Array<string | number | boolean | Date | Buffer | null>;
  const rawConnection = (connection as Connection & { connection?: DriverConnection }).connection;
  if (!rawConnection) {
    const [rawRows, fields] = await connection.execute(statement, preparedParameters);
    return decodeRows(rawRows, fields, maxRows, maxBytes);
  }
  const command = rawConnection.execute(statement, preparedParameters);
  const stream = command.stream({ highWaterMark: 1 });
  const onConnectionError = (error: Error) => stream.destroy(error);
  const onSocketClose = () => stream.destroy(transportError('the MySQL connection closed before the statement completed', 'PROTOCOL_CONNECTION_LOST'));
  rawConnection.on?.('error', onConnectionError);
  rawConnection.stream?.on?.('close', onSocketClose);
  if (rawConnection.stream?.destroyed) onSocketClose();
  let columns: Column[] = [];
  stream.on('fields', (fields: FieldPacket[] | undefined) => {
    columns = (fields ?? []).map(field => ({ name: field.name, typeName: mysqlTypeName(field) }));
    if (new Set(columns.map(column => column.name)).size !== columns.length) stream.destroy(new DuplicateColumnsError('statement returns a column more than once; give each result column a unique alias'));
  });
  const rows: Array<Record<string, unknown>> = [];
  let encodedBytes = 2;
  try {
    for await (const raw of stream) {
      if (columns.length === 0) continue;
      if (rows.length === maxRows) { stream.destroy(); return { columns, rows, truncated: true }; }
      const row = decodeRow(raw, columns);
      encodedBytes += Buffer.byteLength(JSON.stringify(row)) + 1;
      if (encodedBytes > maxBytes) { stream.destroy(); return { columns, rows, truncated: true }; }
      rows.push(row);
    }
  } finally {
    rawConnection.off?.('error', onConnectionError);
    rawConnection.stream?.off?.('close', onSocketClose);
  }
  return { columns, rows };
}
function decodeRows(rawRows: unknown, fields: FieldPacket[], maxRows: number, maxBytes: number): RowSet {
  if (!Array.isArray(rawRows)) return emptyRows();
  const columns: Column[] = fields.map(field => ({ name: field.name, typeName: mysqlTypeName(field) }));
  if (new Set(columns.map(column => column.name)).size !== columns.length) throw new DuplicateColumnsError('statement returns a column more than once; give each result column a unique alias');
  const rows: Array<Record<string, unknown>> = [];
  let encodedBytes = 2;
  for (const raw of rawRows) {
    if (rows.length === maxRows) return { columns, rows, truncated: true };
    const row = decodeRow(raw, columns);
    encodedBytes += Buffer.byteLength(JSON.stringify(row)) + 1;
    if (encodedBytes > maxBytes) return { columns, rows, truncated: true };
    rows.push(row);
  }
  return { columns, rows };
}
function decodeRow(raw: unknown, columns: Column[]): Record<string, unknown> {
  if (!Array.isArray(raw) || raw.length !== columns.length) throw new InvalidRowsError('the server returned an unreadable row');
  const row: Record<string, unknown> = {};
  for (let index = 0; index < columns.length; index++) row[columns[index]!.name] = decodeValue(raw[index], columns[index]!.typeName, columns[index]!.name);
  return row;
}
function mysqlTypeName(field: FieldPacket): string {
  const names: Record<number, string> = { 0: 'DECIMAL', 1: 'TINYINT', 2: 'SMALLINT', 3: 'INT', 4: 'FLOAT', 5: 'DOUBLE', 6: 'NULL', 7: 'TIMESTAMP', 8: 'BIGINT', 9: 'MEDIUMINT', 10: 'DATE', 11: 'TIME', 12: 'DATETIME', 13: 'YEAR', 15: 'VARCHAR', 16: 'BIT', 245: 'JSON', 246: 'DECIMAL', 247: 'ENUM', 248: 'SET', 249: 'TINYBLOB', 250: 'MEDIUMBLOB', 251: 'LONGBLOB', 252: 'BLOB', 253: 'VARCHAR', 254: 'CHAR' };
  let name = names[field.columnType ?? -1] ?? 'VARCHAR';
  if ([249, 250, 251, 252].includes(field.columnType ?? -1) && field.characterSet !== 63) name = ({ TINYBLOB: 'TINYTEXT', MEDIUMBLOB: 'MEDIUMTEXT', LONGBLOB: 'LONGTEXT', BLOB: 'TEXT' } as Record<string, string>)[name] ?? name;
  if (field.columnType === 254 && field.characterSet === 63) name = 'BINARY';
  if (field.columnType === 253 && field.characterSet === 63) name = 'VARBINARY';
  const flags = typeof field.flags === 'number' ? field.flags : 0;
  if (field.columnType === 254 && (flags & 256)) name = 'ENUM';
  if (field.columnType === 254 && (flags & 2048)) name = 'SET';
  const isUnsigned = (typeof field.flags === 'number' ? (field.flags & 32) !== 0 : field.flags.includes('UNSIGNED')) && ['TINYINT', 'SMALLINT', 'MEDIUMINT', 'INT', 'BIGINT'].includes(name);
  return isUnsigned ? `UNSIGNED ${name}` : name;
}
function decodeValue(value: unknown, typeName: string, columnName: string): unknown {
  if (value === null) return null;
  if ((typeName === 'BIGINT' || typeName === 'UNSIGNED BIGINT' || typeName === 'JSON') && typeof value !== 'string') throw new InvalidRowsError(`column "${columnName}" of type ${typeName} returned an unreadable value`);
  if (Buffer.isBuffer(value)) return typeName.replace(/^UNSIGNED /, '') === 'BIT' ? BigInt(`0x${value.toString('hex') || '0'}`).toString() : value.toString('base64');
  if (typeName === 'JSON' && typeof value === 'string') {
    try {
      const parsed: unknown = JSON.parse(value);
      const tokens = value.match(/"(?:\\.|[^"\\])*"|-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?/g) ?? [];
      if (tokens.some(token => token[0] !== '"' && (!Number.isFinite(Number(token)) || String(Number(token)) !== token))) throw new InvalidRowsError(`column "${columnName}" of type JSON contains a number that cannot be represented exactly`);
      return parsed;
    } catch (error) { if (error instanceof InvalidRowsError) throw error; throw new InvalidRowsError(`column "${columnName}" of type JSON returned an invalid value`); }
  }
  if (typeName === 'TIMESTAMP' && typeof value === 'string' && /^\d{4}-\d\d-\d\d \d\d:\d\d:\d\d/.test(value)) return value.replace(' ', 'T').replace(/(\.\d*?)0+$/, '$1').replace(/\.$/, '') + 'Z';
  if (typeName === 'DATETIME' && typeof value === 'string' && /^\d{4}-\d\d-\d\d \d\d:\d\d:\d\d/.test(value) && !value.startsWith('0000')) return value.replace(' ', 'T').replace(/(\.\d*?)0+$/, '$1').replace(/\.$/, '');
  if ((typeName === 'BIGINT' || typeName === 'UNSIGNED BIGINT') && typeof value === 'string') {
    if (!(typeName === 'BIGINT' ? /^-?\d+$/ : /^\d+$/).test(value)) throw new InvalidRowsError(`column "${columnName}" of type ${typeName} returned an invalid integer`);
    return value;
  }
  if (['TINYINT', 'SMALLINT', 'MEDIUMINT', 'INT', 'YEAR'].includes(typeName.replace(/^UNSIGNED /, '')) && typeof value === 'string') {
    const number = Number(value);
    if (!Number.isSafeInteger(number)) throw new InvalidRowsError(`column "${columnName}" of type ${typeName} returned an invalid integer`);
    return number;
  }
  if ((typeName === 'FLOAT' || typeName === 'DOUBLE') && typeof value === 'number' && Object.is(value, -0)) throw new InvalidRowsError(`column "${columnName}" of type ${typeName} returned negative zero, which cannot be represented by the Dex JSON codec`);
  if (typeName === 'FLOAT' && typeof value === 'number' && Number.isFinite(value) && Math.fround(value) === value) return shortestFloat32(value);
  if (typeName === 'FLOAT') throw new InvalidRowsError(`column "${columnName}" of type FLOAT returned an invalid number`);
  if (typeof value === 'number' && !Number.isFinite(value)) throw new InvalidRowsError(`column "${columnName}" of type ${typeName} returned a non-finite number`);
  return value;
}

function shortestFloat32(value: number): number {
  if (value === 0) return 0;
  const magnitude = Math.abs(value);
  const bits = new DataView(new ArrayBuffer(4));
  bits.setFloat32(0, magnitude);
  const binary = bits.getUint32(0);
  // Go's shortest formatter chooses the upper decimal at this power-of-two midpoint.
  if (binary === 0x39800000) return value < 0 ? -0.00024414063 : 0.00024414063;
  const exponent = (binary >>> 23) & 255;
  const significand = BigInt((binary & 0x7fffff) + (exponent ? 0x800000 : 0));
  const binaryExponent = exponent ? exponent - 150 : -149;
  const numerator = binaryExponent >= 0 ? significand << BigInt(binaryExponent) : significand;
  const denominator = binaryExponent < 0 ? 1n << BigInt(-binaryExponent) : 1n;
  const decimalExponent = Number(magnitude.toExponential().split('e')[1]);
  for (let precision = 1; precision <= 9; precision++) {
    let best: { value: number; distance: bigint; denominator: bigint; digit: bigint } | undefined;
    for (let scale = decimalExponent - precision; scale <= decimalExponent - precision + 2; scale++) {
      const scaledNumerator = scale < 0 ? numerator * 10n ** BigInt(-scale) : numerator;
      const scaledDenominator = scale > 0 ? denominator * 10n ** BigInt(scale) : denominator;
      const nearest = scaledNumerator / scaledDenominator;
      for (let offset = -2n; offset <= 2n; offset++) {
        const digit = nearest + offset;
        if (digit < 10n ** BigInt(precision - 1) || digit >= 10n ** BigInt(precision)) continue;
        const candidate = Number(`${digit}e${scale}`);
        if (Math.fround(candidate) !== magnitude) continue;
        const candidateNumerator = scale >= 0 ? digit * 10n ** BigInt(scale) : digit;
        const candidateDenominator = scale < 0 ? 10n ** BigInt(-scale) : 1n;
        const distance = candidateNumerator * denominator - numerator * candidateDenominator;
        const absoluteDistance = distance < 0n ? -distance : distance;
        if (!best || absoluteDistance * best.denominator < best.distance * candidateDenominator || absoluteDistance * best.denominator === best.distance * candidateDenominator && digit % 2n === 0n && best.digit % 2n !== 0n) best = { value: candidate, distance: absoluteDistance, denominator: candidateDenominator, digit };
      }
    }
    if (best) return value < 0 ? -best.value : best.value;
  }
  throw new Error('a finite float32 has no shortest decimal representation');
}
