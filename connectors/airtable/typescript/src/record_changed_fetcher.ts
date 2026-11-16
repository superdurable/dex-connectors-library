// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { TriggerBindingRef, TriggerContext, TriggerEvent } from '@superdurable/connectors-sdk';
import { markCursorInvalid, markRateLimited, newTriggerCredentialCall, waitForPollRequest, type ChangeFetcher, type ChangePage, type Cursor } from '@superdurable/connectors-sdk/pollingtrigger';
import type { AirtableClient } from './client.js';
import type { Record as AirtableRecord } from './generated/models.js';
import { decodeRecord } from './operations.js';
import { RecordChangedCursorError, RecordChangedPosition, decodeRecordChangedCursor, encodeRecordChangedCursor } from './record_changed_cursor.js';
import type { RecordChangedEvent, RecordChangedTriggerConfiguration } from './record_changed_models.js';
import {
  NanosecondsPerMillisecond, NanosecondsPerSecond, formatEventIDTime, formatMilliseconds, formatRFC3339Nanoseconds,
  nanosecondsToDate, parseRFC3339Nanoseconds, truncateToMillisecond,
} from './record_changed_time.js';

/** Pages one fetch follows past already processed records; the open scan resumes on the next fetch. */
const maximumRecordChangedScanPages = 20;
const lastModifiedTimeFieldType = 'lastModifiedTime';
const maximumOffsetBytes = 1024;

/** A failed recordChanged poll; the kit backs off and retries without moving the cursor. */
export class RecordChangedPollError extends Error {
  constructor(message: string) { super(message); this.name = 'RecordChangedPollError'; }
}

/** Airtable answered 422 LIST_RECORDS_ITERATOR_NOT_AVAILABLE for a page offset. */
class ListOffsetExpiredError extends RecordChangedPollError {}

interface ChangedRecord { record: AirtableRecord; modifiedAt: bigint }

/** Return Go's event ID: the record ID and its modification time, a pure function of the change. */
export function recordChangedEventId(recordId: string, lastModifiedTime: bigint): string {
  return `${recordId}-${formatEventIDTime(lastModifiedTime)}`;
}

/** The ChangeFetcher of one recordChanged binding: one table's records by modification time, as the Go fetcher reads them. */
export class RecordChangedFetcher implements ChangeFetcher<TriggerEvent<RecordChangedEvent>> {
  /** The watched field's current name, resolved from the base schema; blank until resolved. */
  private fieldName = '';

  /** Bind the client and the binding's table; it sends nothing. */
  constructor(private readonly client: AirtableClient, private readonly binding: TriggerBindingRef, private readonly configuration: RecordChangedTriggerConfiguration) {}

  /** Position the cursor at the table's most recent change, so existing records are not delivered. */
  async seedCursor(context: TriggerContext): Promise<Cursor> {
    await this.resolveFieldName(context);
    const { records, nextOffset } = await this.listRecords(context, undefined, 'desc', '');
    const position = new RecordChangedPosition(this.configuration.fieldId);
    if (nextOffset && records.length > 0) {
      // Older existing records than this page, including its oldest time's unlisted ties, stay hidden.
      const oldest = records.reduce((minimum, changed) => (changed.modifiedAt < minimum ? changed.modifiedAt : minimum), records[0]!.modifiedAt);
      position.floor = truncateToMillisecond(oldest) + NanosecondsPerMillisecond;
    }
    for (const changed of records) position.deliver(changed.record.id, changed.modifiedAt);
    return encodeRecordChangedCursor(position);
  }

  /**
   * List records modified since the cursor's lower bound, oldest first, and return those not yet delivered.
   * An open scan resumes from its stored Airtable offset.
   */
  async fetchChanges(context: TriggerContext, cursor: Cursor): Promise<ChangePage<TriggerEvent<RecordChangedEvent>>> {
    let position: RecordChangedPosition;
    try { position = decodeRecordChangedCursor(cursor.token); }
    catch (error) {
      if (error instanceof RecordChangedCursorError) throw markCursorInvalid(error);
      throw error;
    }
    if (position.fieldId !== this.configuration.fieldId) throw markCursorInvalid(new RecordChangedPollError('the binding now watches a different Last modified time field'));
    await this.resolveFieldName(context);
    let since = position.filterSince(), offset = '';
    if (position.scan) { since = parseRFC3339Nanoseconds(position.scan.since) ?? 0n; offset = position.scan.offset; }
    let hasRestartedScan = false;
    const changes: Array<TriggerEvent<RecordChangedEvent>> = [];
    for (let scannedPages = 0; scannedPages < maximumRecordChangedScanPages; scannedPages++) {
      let page: { records: ChangedRecord[]; nextOffset: string };
      try { page = await this.listRecords(context, since, 'asc', offset); }
      catch (error) {
        if (!(error instanceof ListOffsetExpiredError) || !offset || hasRestartedScan) throw error;
        // Airtable iterators time out; the scan starts again, and remembered records stay skipped.
        hasRestartedScan = true;
        position.scan = undefined;
        since = position.filterSince();
        offset = '';
        continue;
      }
      for (const changed of page.records) {
        if (position.isDelivered(changed.record.id, changed.modifiedAt)) continue;
        changes.push(this.newEvent(changed));
        position.deliver(changed.record.id, changed.modifiedAt);
      }
      position.scan = undefined;
      if (!page.nextOffset) position.closeBurst();
      else position.scan = { since: formatRFC3339Nanoseconds(since), offset: page.nextOffset };
      if (changes.length > 0 || !page.nextOffset) return { changes, next: encodeRecordChangedCursor(position), hasMorePages: page.nextOffset !== '' };
      offset = page.nextOffset;
    }
    // A long run of delivered records: commit the open scan so the next fetch continues it.
    return { changes, next: encodeRecordChangedCursor(position), hasMorePages: true };
  }

  private newEvent(changed: ChangedRecord): TriggerEvent<RecordChangedEvent> {
    const createdAt = parseRFC3339Nanoseconds(changed.record.createdTime) ?? 0n;
    const distance = changed.modifiedAt - createdAt;
    return {
      id: recordChangedEventId(changed.record.id, changed.modifiedAt),
      occurredAt: nanosecondsToDate(changed.modifiedAt),
      payload: {
        baseId: this.configuration.baseId, tableId: this.configuration.tableId,
        lastModifiedTime: formatRFC3339Nanoseconds(changed.modifiedAt),
        isCreatedRecord: (distance < 0n ? -distance : distance) <= NanosecondsPerSecond,
        record: changed.record,
      },
    };
  }

  /** Read one page sorted by the watched field, of records whose field value is after since. */
  private async listRecords(context: TriggerContext, since: bigint | undefined, direction: 'asc' | 'desc', offset: string): Promise<{ records: ChangedRecord[]; nextOffset: string }> {
    const fieldId = this.configuration.fieldId;
    const body = {
      pageSize: this.client.pollPageSize,
      ...(offset ? { offset } : {}),
      filterByFormula: `IS_AFTER({${fieldId}}, DATETIME_PARSE("${formatMilliseconds(since ?? 0n)}"))`,
      sort: [{ field: fieldId, direction }],
    };
    const path = `/v0/${encodeURIComponent(this.configuration.baseId)}/${encodeURIComponent(this.configuration.tableId)}/listRecords`;
    const response = await this.exchange(context, 'POST', path, body);
    let records: AirtableRecord[], nextOffset: string;
    try {
      const document = decodeJSONObject(response);
      if (document.records === undefined || document.records === null) throw new RecordChangedPollError('Airtable returned a list response without records');
      if (!Array.isArray(document.records)) throw new RecordChangedPollError('Airtable returned a malformed response');
      if (document.records.length > this.client.pollPageSize) throw new RecordChangedPollError('Airtable returned more records than the requested page size');
      nextOffset = optionalMember(document, 'offset', 'string') ?? '';
      if (Buffer.byteLength(nextOffset, 'utf8') > maximumOffsetBytes) throw new RecordChangedPollError('Airtable returned an oversized offset');
      records = document.records.map(decodeRecord);
    } catch (error) {
      if (error instanceof RecordChangedPollError) throw error;
      throw new RecordChangedPollError('Airtable returned a malformed response');
    }
    return { records: records.map(record => ({ record, modifiedAt: this.readLastModifiedTime(record) })), nextOffset };
  }

  /** Parse the watched field of a record the filter matched, so the field has a value. */
  private readLastModifiedTime(record: AirtableRecord): bigint {
    const fields = record.fields ?? {};
    if (!Object.hasOwn(fields, this.fieldName)) {
      // The field was renamed since the schema was read; the next poll reads the schema again.
      this.fieldName = '';
      throw new RecordChangedPollError('Airtable returned a changed record without the Last modified time field; the field name is read again');
    }
    const value = fields[this.fieldName];
    const modifiedAt = typeof value === 'string' ? parseRFC3339Nanoseconds(value) : undefined;
    if (modifiedAt === undefined) throw new RecordChangedPollError('Airtable returned a Last modified time value without a time of day');
    return modifiedAt;
  }

  /** Read the base schema once, and again after a rename, to find the watched field's current name, since listed records key their fields by name. */
  private async resolveFieldName(context: TriggerContext): Promise<void> {
    if (this.fieldName) return;
    const response = await this.exchange(context, 'GET', `/v0/meta/bases/${encodeURIComponent(this.configuration.baseId)}/tables`, undefined);
    let tables: SchemaTable[];
    try { tables = readSchemaTables(decodeJSONObject(response)); }
    catch { throw new RecordChangedPollError('Airtable returned a malformed response'); }
    const table = tables.find(candidate => candidate.id === this.configuration.tableId);
    if (!table) throw new RecordChangedPollError('the Airtable base schema has no table with the recordChanged table ID');
    const field = table.fields.find(candidate => candidate.id === this.configuration.fieldId);
    if (!field) throw new RecordChangedPollError('the Airtable table has no field with the recordChanged field ID');
    if (field.type !== lastModifiedTimeFieldType || field.name === '') throw new RecordChangedPollError('the Airtable recordChanged field is not a Last modified time field');
    if (field.resultType !== undefined && field.resultType !== 'dateTime') throw new RecordChangedPollError('the Airtable recordChanged Last modified time field must include a time of day');
    this.fieldName = field.name;
  }

  /** Wait on the runner's request limiter, then send one paced request through the client. */
  private async exchange(context: TriggerContext, method: string, path: string, body: unknown): Promise<Uint8Array> {
    await waitForPollRequest(context);
    const call = newTriggerCredentialCall(this.binding.connection, this.binding.trigger, context.signal);
    const baseId = this.configuration.baseId;
    const result = await this.client.exchange(call, this.binding.trigger.triggerName, baseId, baseId, method, path, body);
    if ('response' in result) return result.response.body;
    const outcome = result.outcome;
    if (outcome.disposition === 'offsetExpired') throw new ListOffsetExpiredError('Airtable no longer recognizes the list offset');
    const error = new RecordChangedPollError(outcome.failure.message);
    // 429 and a full per-base pacing queue pause every binding of the connection.
    if (outcome.disposition === 'retry' && outcome.failure.kind === 'RATE_LIMIT') throw markRateLimited(error, outcome.retryAfterMs);
    throw error;
  }
}

interface SchemaField { id: string; name: string; type: string; resultType: string | undefined }
interface SchemaTable { id: string; fields: SchemaField[] }

/** Read the part of GET /v0/meta/bases/{baseId}/tables the fetcher uses, typed as Go decodes it. */
function readSchemaTables(document: Record<string, unknown>): SchemaTable[] {
  return (optionalMember(document, 'tables', 'array') ?? []).map((table): SchemaTable => ({
    id: optionalMember(table, 'id', 'string') ?? '',
    fields: (optionalMember(table, 'fields', 'array') ?? []).map((field): SchemaField => {
      const result = optionalMember(optionalMember(field, 'options', 'object'), 'result', 'object');
      return {
        id: optionalMember(field, 'id', 'string') ?? '', name: optionalMember(field, 'name', 'string') ?? '',
        type: optionalMember(field, 'type', 'string') ?? '',
        resultType: result === undefined ? undefined : (optionalMember(result, 'type', 'string') ?? ''),
      };
    }),
  }));
}

type MemberKind = { string: string; array: unknown[]; object: Record<string, unknown> };

function optionalMember<Kind extends keyof MemberKind>(document: unknown, name: string, kind: Kind): MemberKind[Kind] | undefined {
  if (document === undefined || document === null) return undefined;
  if (!isJSONObject(document)) throw new TypeError('Airtable returned a malformed response');
  const value = document[name];
  if (value === undefined || value === null) return undefined;
  const matches = kind === 'string' ? typeof value === 'string' : kind === 'array' ? Array.isArray(value) : isJSONObject(value);
  if (!matches) throw new TypeError('Airtable returned a malformed response');
  return value as MemberKind[Kind];
}

function isJSONObject(value: unknown): value is Record<string, unknown> { return value !== null && typeof value === 'object' && !Array.isArray(value); }

function decodeJSONObject(body: Uint8Array): Record<string, unknown> {
  const document: unknown = JSON.parse(Buffer.from(body).toString('utf8'));
  if (!isJSONObject(document)) throw new TypeError('Airtable returned a malformed response');
  return document;
}

