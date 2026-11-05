// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newMutationBranch, newMutationRetry, newQueryBranch, newQueryRetry, type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import type { SnowflakeClient, SnowflakeFailure } from './client.js';
import { decodeStatus, isStatementHandle } from './client.js';
import { SubmitStatementDefinition, GetStatementResultDefinition, CancelStatementDefinition } from './generated/definitions.js';
import type { SubmitStatementInput, StatementSubmission, GetStatementResultInput, StatementResult, CancelStatementInput, StatementCancellation, Column, StatementStats } from './generated/models.js';

const path = '/api/v2/statements';
const jsonWithRaw = JSON as unknown as {
  parse(text: string, reviver: (key: string, value: unknown, context: { source?: string }) => unknown): unknown;
  rawJSON(text: string): unknown;
};
const emptySubmission: StatementSubmission = { statementHandle: '' };
const emptyCancellation: StatementCancellation = { statementHandle: '' };
const emptyResult = (input: GetStatementResultInput): StatementResult => ({ statementHandle: input.statementHandle, columns: [], rows: [], partition: input.partition ?? 0 });

/** Submit one parameterized SQL statement asynchronously. */
export class SubmitStatementOperation implements Mutation<SubmitStatementInput, StatementSubmission> {
  /** Bind the validated client. */ constructor(private readonly client: SnowflakeClient) {}
  /** Return the manifest definition. */ definition() { return SubmitStatementDefinition; }
  /** Use the stable Dex Call ID as Snowflake requestId. */ idempotencyKey(callId: string) { return callId; }
  /** Send one repeat-safe asynchronous attempt. */
  async invoke(call: Call, input: SubmitStatementInput): Promise<MutationAttempt<StatementSubmission>> {
    let bindings: Record<string, { type: string; value: string | null }> | undefined;
    try { bindings = bindStatement(input.statement, input.parameters ?? []); }
    catch (error) { return mutationFailure(this.client, call, 'submitStatement', 'defect', 'VALIDATION', error instanceof Error ? error.message : 'invalid statement', emptySubmission); }
    if (!isStatementHandle(call.idempotencyKey ?? '')) return mutationFailure(this.client, call, 'submitStatement', 'defect', 'LOCAL_DEFECT', "the Step's idempotency key is not a UUID, so it cannot be Snowflake's requestId", emptySubmission);
    const body = { statement: input.statement, timeout: this.client.statementTimeoutSeconds,
      ...(this.client.context.database ? { database: this.client.context.database } : {}),
      ...(this.client.context.schema ? { schema: this.client.context.schema } : {}),
      ...(this.client.context.warehouse ? { warehouse: this.client.context.warehouse } : {}),
      ...(this.client.context.role ? { role: this.client.context.role } : {}),
      ...(bindings ? { bindings } : {}), parameters: { MULTI_STATEMENT_COUNT: '1' } };
    const target = `${path}?${new URLSearchParams({ requestId: call.idempotencyKey!, retry: 'true', async: 'true' }).toString()}`;
    const exchange = await this.client.exchange(call, 'submitStatement', 'POST', target, Buffer.from(JSON.stringify(body)));
    if ('outcome' in exchange) return mutationOutcome(this.client, call, 'submitStatement', exchange.outcome, emptySubmission);
    let status;
    try { status = decodeSuccessStatus(exchange.response.body); }
    catch { return newMutationRetry({ kind: 'PROTOCOL', provider: 'snowflake', operation: 'submitStatement', message: 'Snowflake accepted the request but its response held no readable statement handle' }, 0); }
    if (!status.statementHandle) return newMutationRetry({ kind: 'PROTOCOL', provider: 'snowflake', operation: 'submitStatement', message: 'Snowflake accepted the request but its response held no readable statement handle' }, 0);
    return newMutationBranch('submitted', { statementHandle: status.statementHandle, ...(status.createdOn && status.createdOn > 0 ? { submittedAt: formatGoTime(status.createdOn) } : {}) }, this.client.receipt(call, status.statementHandle, status));
  }
}

/** Read one statement status or result partition. */
export class GetStatementResultOperation implements Query<GetStatementResultInput, StatementResult> {
  /** Bind the validated client. */ constructor(private readonly client: SnowflakeClient) {}
  /** Return the manifest definition. */ definition() { return GetStatementResultDefinition; }
  /** Read one partition without waiting for the statement to finish. */
  async invoke(call: Call, input: GetStatementResultInput): Promise<QueryAttempt<StatementResult>> {
    const zero = emptyResult(input);
    if (!isStatementHandle(input.statementHandle)) return queryFailure(this.client, call, 'defect', 'VALIDATION', 'statementHandle must be the UUID-shaped handle submitStatement returned', emptyResult({ statementHandle: '' }), false);
    const partition = input.partition ?? 0;
    if (!Number.isSafeInteger(partition) || partition < 0) return queryFailure(this.client, call, 'defect', 'VALIDATION', 'partition cannot be negative', zero, false);
    if (partition > 0 && (!input.columns?.length || hasDuplicateColumns(input.columns))) return queryFailure(this.client, call, 'defect', 'VALIDATION', !input.columns?.length ? 'columns from the partition-0 Result are required to read a later partition' : 'the statement returns a column more than once; give each result column a unique alias', zero, false);
    const exchange = await this.client.exchange(call, 'getStatementResult', 'GET', `${path}/${input.statementHandle}${partition > 0 ? `?partition=${partition}` : ''}`);
    if ('outcome' in exchange) return queryOutcome(this.client, call, input, exchange.outcome);
    if (exchange.response.status === 202) {
      const status = decodeStatus(exchange.response.body);
      return newQueryBranch('running', { ...zero, ...(status.createdOn && status.createdOn > 0 ? { submittedAt: formatGoTime(status.createdOn) } : {}) }, this.client.receipt(call, input.statementHandle, status));
    }
    const receipt = this.client.receipt(call, input.statementHandle, decodeStatus(exchange.response.body));
    let result: StatementResult;
    try { result = decodeResult(exchange.response.body, input, this.client.maxRows, this.client.maxResponseBytes); }
    catch (error) {
      const message = error instanceof Error ? error.message : 'Snowflake returned a response the connector cannot decode';
      return queryFailure(this.client, call, message.includes('unique alias') ? 'defect' : 'invalidResponse', message.includes('unique alias') ? 'VALIDATION' : message.startsWith('the Snowflake response exceeds') ? 'RESPONSE_TOO_LARGE' : 'PROTOCOL', message, zero, true);
    }
    if (result.truncated) return newQueryBranch('truncated', result, receipt, { kind: 'RESPONSE_TOO_LARGE', provider: 'snowflake', operation: 'getStatementResult', message: 'the partition held more rows or bytes than maxRows or maxResponseBytes allow' });
    return newQueryBranch('completed', result, receipt);
  }
}

/** Cancel a queued or running statement. */
export class CancelStatementOperation implements Mutation<CancelStatementInput, StatementCancellation> {
  /** Bind the validated client. */ constructor(private readonly client: SnowflakeClient) {}
  /** Return the manifest definition. */ definition() { return CancelStatementDefinition; }
  /** Use the stable Dex Call ID. */ idempotencyKey(callId: string) { return callId; }
  /** Send one repeat-safe cancellation attempt. */
  async invoke(call: Call, input: CancelStatementInput): Promise<MutationAttempt<StatementCancellation>> {
    if (!isStatementHandle(input.statementHandle)) return mutationFailure(this.client, call, 'cancelStatement', 'defect', 'VALIDATION', 'statementHandle must be the UUID-shaped handle submitStatement returned', emptyCancellation);
    const exchange = await this.client.exchange(call, 'cancelStatement', 'POST', `${path}/${input.statementHandle}/cancel?${new URLSearchParams({ requestId: call.idempotencyKey ?? '' }).toString()}`, Buffer.from('{}'));
    if ('outcome' in exchange) return mutationOutcome(this.client, call, 'cancelStatement', exchange.outcome, emptyCancellation, input.statementHandle);
    return newMutationBranch('canceled', { statementHandle: input.statementHandle }, this.client.receipt(call, input.statementHandle, decodeStatus(exchange.response.body)));
  }
}

function mutationOutcome<T>(client: SnowflakeClient, call: Call, _operation: string, outcome: SnowflakeFailure, zero: T, handle = ''): MutationAttempt<T> {
  if (outcome.disposition === 'retry' || outcome.disposition === 'invalidResponse') return newMutationRetry(outcome.failure, outcome.retryAfterMs);
  return newMutationBranch(outcome.disposition === 'defect' ? 'defect' : 'providerRejected', zero, client.receipt(call, handle || outcome.status.statementHandle, outcome.status), outcome.failure);
}

function queryOutcome(client: SnowflakeClient, call: Call, input: GetStatementResultInput, outcome: SnowflakeFailure): QueryAttempt<StatementResult> {
  if (outcome.disposition === 'retry') return newQueryRetry(outcome.failure, outcome.retryAfterMs);
  return newQueryBranch(outcome.disposition === 'rejected' ? 'providerRejected' : outcome.disposition, emptyResult(input), client.receipt(call, input.statementHandle, outcome.status), outcome.failure);
}

function mutationFailure<T>(_client: SnowflakeClient, _call: Call, operation: string, branch: string, kind: Failure['kind'], message: string, zero: T): MutationAttempt<T> {
  return newMutationBranch(branch, zero, {}, { kind, provider: 'snowflake', operation, message });
}

function queryFailure(client: SnowflakeClient, call: Call, branch: string, kind: Failure['kind'], message: string, zero: StatementResult, withReceipt: boolean): QueryAttempt<StatementResult> {
  return newQueryBranch(branch, zero, withReceipt ? client.receipt(call, zero.statementHandle) : {}, { kind, provider: 'snowflake', operation: 'getStatementResult', message });
}

function decodeSuccessStatus(body: Uint8Array) {
  const value = parseJSON(body);
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('invalid response');
  const object = value as Record<string, unknown>;
  if (object.statementHandle !== undefined && typeof object.statementHandle !== 'string') throw new Error('invalid response');
  if (object.createdOn !== undefined && object.createdOn !== null && !Number.isSafeInteger(object.createdOn)) throw new Error('invalid response');
  return decodeStatus(body);
}

function decodeResult(body: Uint8Array, input: GetStatementResultInput, maxRows: number, maxBytes: number): StatementResult {
  const value = parseJSON(body);
  const partition = input.partition ?? 0;
  if (!Array.isArray(value) && (!value || typeof value !== 'object')) throw new Error('Snowflake returned a response the connector cannot decode');
  const object = Array.isArray(value) ? {} as Record<string, unknown> : value as Record<string, unknown>;
  const metadata = object.resultSetMetaData;
  let columns = input.columns ?? [];
  let partitionCount = 0;
  let totalRowCount: number | undefined;
  if (partition === 0) {
    if (!metadata || typeof metadata !== 'object' || Array.isArray(metadata)) throw new Error('Snowflake returned partition 0 without jsonv2 column metadata');
    const meta = metadata as Record<string, unknown>;
    if (meta.format !== undefined && meta.format !== '' && meta.format !== 'jsonv2') throw new Error('Snowflake returned partition 0 without jsonv2 column metadata');
    if (!Array.isArray(meta.rowType)) throw new Error('Snowflake returned invalid column metadata');
    columns = meta.rowType.map(decodeColumn);
    if (hasDuplicateColumns(columns)) throw new Error('the statement returns a column more than once; give each result column a unique alias');
    if (meta.partitionInfo !== undefined && !Array.isArray(meta.partitionInfo)) throw new Error('Snowflake returned invalid column metadata');
    partitionCount = Array.isArray(meta.partitionInfo) ? meta.partitionInfo.length : 0;
    if (meta.numRows !== undefined && meta.numRows !== null) {
      if (!Number.isSafeInteger(meta.numRows)) throw new Error('Snowflake returned invalid column metadata');
      totalRowCount = meta.numRows as number;
    }
  }
  const rawRows = Array.isArray(value) ? value : object.data ?? [];
  if (!Array.isArray(rawRows)) throw new Error('Snowflake returned a response the connector cannot decode');
  const rows: Record<string, unknown>[] = [];
  let bytes = 2;
  let truncated = false;
  for (const rawRow of rawRows) {
    if (rows.length >= maxRows) { truncated = true; break; }
    if (!Array.isArray(rawRow) || rawRow.length !== columns.length) throw new Error('Snowflake returned a row whose width differs from its columns');
    const row: Record<string, unknown> = {};
    for (let index = 0; index < columns.length; index++) {
      const column = columns[index];
      const cell: unknown = rawRow[index];
      if (!column || (cell !== null && typeof cell !== 'string')) throw new Error('Snowflake returned a response the connector cannot decode');
      row[column.name] = decodeColumnValue(column, cell as string | null);
    }
    bytes += Buffer.byteLength(JSON.stringify(row)) + 1;
    if (bytes > maxBytes) { truncated = true; break; }
    rows.push(row);
  }
  const status = Array.isArray(value) ? {} : decodeStatus(body);
  const result: StatementResult = { statementHandle: input.statementHandle, columns, rows, partition,
    ...(partition === 0 && partitionCount ? { partitionCount } : {}),
    ...(partition === 0 && totalRowCount !== undefined ? { totalRowCount } : {}),
    ...(truncated ? { truncated: true } : {}),
    ...(status.createdOn && status.createdOn > 0 ? { submittedAt: formatGoTime(status.createdOn) } : {}) };
  if (object.stats !== undefined && object.stats !== null) result.stats = decodeStats(object.stats);
  return result;
}

function decodeColumn(value: unknown): Column {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('Snowflake returned invalid column metadata');
  const column = value as Record<string, unknown>;
  if (typeof column.name !== 'string' || !column.name || typeof column.type !== 'string' || !column.type) throw new Error('Snowflake returned invalid column metadata');
  const result: Column = { name: column.name, type: column.type.toLowerCase(), nullable: false };
  for (const field of ['precision', 'scale', 'length'] as const) {
    if (column[field] !== undefined && column[field] !== null) {
      if (!Number.isSafeInteger(column[field])) throw new Error('Snowflake returned invalid column metadata');
      result[field] = column[field] as number;
    }
  }
  if (column.nullable !== undefined && column.nullable !== null) {
    if (typeof column.nullable !== 'boolean') throw new Error('Snowflake returned invalid column metadata');
    result.nullable = column.nullable;
  }
  return result;
}

function decodeStats(value: unknown): StatementStats {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('Snowflake returned a response the connector cannot decode');
  const stats = value as Record<string, unknown>;
  const field = (name: string) => { const candidate = stats[name]; if (candidate === undefined || candidate === null) return 0; if (!Number.isSafeInteger(candidate)) throw new Error('Snowflake returned a response the connector cannot decode'); return candidate as number; };
  return { rowsInserted: field('numRowsInserted'), rowsUpdated: field('numRowsUpdated'), rowsDeleted: field('numRowsDeleted'), duplicateRowsUpdated: field('numDuplicateRowsUpdated') };
}

function decodeColumnValue(column: Column, cell: string | null): unknown {
  if (cell === null) return null;
  const invalid = () => { throw new Error(`column ${JSON.stringify(column.name)} of type ${column.type} returned text the connector cannot decode`); };
  switch (column.type) {
    case 'fixed':
      if (!/^-?[0-9]+(?:\.[0-9]+)?$/.test(cell)) return invalid();
      return (column.scale ?? 0) === 0 && (column.precision ?? 0) > 0 && (column.precision ?? 0) <= 15 ? Number(cell) : cell;
    case 'real': {
      if (/^nan$/i.test(cell)) return 'NaN';
      if (/^(inf|infinity)$/i.test(cell)) return 'Infinity';
      if (/^-(inf|infinity)$/i.test(cell)) return '-Infinity';
      const number = Number(cell); return /^[+-]?(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][+-]?\d+)?$/.test(cell) && Number.isFinite(number) ? number : invalid();
    }
    case 'decfloat': return /^-?[0-9]+(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$/.test(cell) ? cell : invalid();
    case 'boolean': return ['true', 'TRUE', 'True', '1', 't', 'T'].includes(cell) ? true : ['false', 'FALSE', 'False', '0', 'f', 'F'].includes(cell) ? false : invalid();
    case 'binary': return /^(?:[0-9a-fA-F]{2})*$/.test(cell) ? Buffer.from(cell, 'hex').toString('base64') : invalid();
    case 'date': {
      if (!/^-?[0-9]{1,9}$/.test(cell)) return invalid();
      const date = new Date(Number(cell) * 86400000);
      return Number.isNaN(date.getTime()) || date.getUTCFullYear() < 1 || date.getUTCFullYear() > 9999 ? cell : date.toISOString().slice(0, 10);
    }
    case 'time': case 'timestamp_ntz': case 'timestamp_ltz': case 'timestamp_tz': return decodeTemporal(column.type, cell, invalid);
    case 'variant': case 'object': case 'array': case 'map':
      try {
        return jsonWithRaw.parse(cell, (_key, value, context) =>
          typeof value === 'number' ? jsonWithRaw.rawJSON(context.source!) : value);
      } catch { return invalid(); }
    default: return cell;
  }
}

function decodeTemporal(type: string, cell: string, invalid: () => never): string {
  const match = /^(-?)([0-9]{1,12})(?:\.([0-9]{1,9}))?(?: ([0-9]{1,4}))?$/.exec(cell);
  if (!match) return invalid();
  if (type === 'timestamp_tz' ? !match[4] : !!match[4]) return invalid();
  const seconds = Number(match[2]);
  let nanoseconds = Number((match[3] ?? '').padEnd(9, '0'));
  let epochSeconds = match[1] === '-' ? -seconds : seconds;
  if (match[1] === '-' && nanoseconds > 0) { epochSeconds--; nanoseconds = 1_000_000_000 - nanoseconds; }
  const offset = type === 'timestamp_tz' ? Number(match[4]) - 1440 : 0;
  if (offset < -1440 || offset > 1440) return invalid();
  const date = new Date(epochSeconds * 1000 + offset * 60000);
  if (Number.isNaN(date.getTime())) return invalid();
  if (type !== 'time' && (date.getUTCFullYear() < 1 || date.getUTCFullYear() > 9999)) return cell;
  const fraction = String(nanoseconds).padStart(9, '0').replace(/0+$/, '');
  if (type === 'time') return `${date.toISOString().slice(11, 19)}${fraction ? `.${fraction}` : ''}`;
  const timestamp = `${date.toISOString().slice(0, 19)}${fraction ? `.${fraction}` : ''}`;
  if (type === 'timestamp_ntz') return timestamp;
  if (type === 'timestamp_tz') {
    if (offset === 0) return `${timestamp}Z`;
    const sign = offset < 0 ? '-' : '+';
    return `${timestamp}${sign}${String(Math.floor(Math.abs(offset) / 60)).padStart(2, '0')}:${String(Math.abs(offset) % 60).padStart(2, '0')}`;
  }
  return `${timestamp}Z`;
}

function parseJSON(body: Uint8Array): unknown { try { return JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { throw new Error('Snowflake returned a response the connector cannot decode'); } }
function formatGoTime(milliseconds: number): string { return new Date(milliseconds).toISOString().replace(/\.(\d{3})Z$/, (_match, fraction: string) => {
  const trimmed = fraction.replace(/0+$/, '');
  return trimmed ? `.${trimmed}Z` : 'Z';
}); }
function hasDuplicateColumns(columns: Column[]): boolean { return new Set(columns.map(column => column.name)).size !== columns.length; }

function bindStatement(statement: string, parameters: unknown[]): Record<string, { type: string; value: string | null }> | undefined {
  if (!statement.trim()) throw new Error('statement is required');
  if (Buffer.byteLength(statement) > 1048576) throw new Error('statement exceeds 1048576 bytes');
  if (statement.includes('\0')) throw new Error('statement must be valid UTF-8 without NUL characters');
  if (parameters.length > 10000) throw new Error('statement cannot bind more than 10000 parameters');
  const placeholders = countPlaceholders(statement);
  if (placeholders !== parameters.length) throw new Error(`statement uses ${placeholders} ? placeholders but ${parameters.length} parameters were bound`);
  if (!parameters.length) return undefined;
  const bindings: Record<string, { type: string; value: string | null }> = {};
  let totalBytes = 0;
  parameters.forEach((parameter, index) => {
    let type = 'TEXT'; let value: string | null = null;
    if (parameter === null) value = null;
    else if (typeof parameter === 'string') value = parameter;
    else if (typeof parameter === 'boolean') { type = 'BOOLEAN'; value = String(parameter); }
    else if (typeof parameter === 'number' && Number.isFinite(parameter)) { type = Number.isInteger(parameter) ? 'FIXED' : 'REAL'; value = String(parameter); }
    else if (parameter instanceof Uint8Array) { type = 'BINARY'; value = Buffer.from(parameter).toString('hex'); }
    else if (parameter instanceof Date) { type = 'TIMESTAMP_TZ'; value = `${BigInt(parameter.getTime()) * 1000000n} 1440`; }
    else throw new Error(`parameter ${index + 1}: unsupported value type`);
    if (value?.includes('\0')) throw new Error(`parameter ${index + 1}: text must be valid UTF-8 without NUL characters`);
    totalBytes += value ? Buffer.byteLength(value) : 0;
    if (totalBytes > 16777216) throw new Error('parameters exceed 16777216 bytes combined');
    bindings[String(index + 1)] = { type, value };
  });
  return bindings;
}

function countPlaceholders(statement: string): number {
  let count = 0;
  for (let index = 0; index < statement.length; index++) {
    const char = statement[index];
    if (char === '?') { count++; continue; }
    if (char === "'" || char === '"') {
      const quote = char;
      let ended = false;
      for (index++; index < statement.length; index++) {
        if (quote === "'" && statement[index] === '\\') { index++; continue; }
        if (statement[index] === quote && statement[index + 1] === quote) { index++; continue; }
        if (statement[index] === quote) { ended = true; break; }
      }
      if (!ended) throw new Error(quote === "'" ? 'statement has an unterminated string literal' : 'statement has an unterminated quoted identifier');
      continue;
    }
    if (statement.startsWith('$$', index)) { const end = statement.indexOf('$$', index + 2); if (end < 0) throw new Error('statement has an unterminated $$ string'); index = end + 1; continue; }
    if (statement.startsWith('--', index) || statement.startsWith('//', index)) { const end = statement.indexOf('\n', index); if (end < 0) break; index = end; continue; }
    if (statement.startsWith('/*', index)) { const end = statement.indexOf('*/', index + 2); if (end < 0) throw new Error('statement has an unterminated /* comment'); index = end + 1; }
  }
  return count;
}
