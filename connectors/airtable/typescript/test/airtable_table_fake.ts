// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

/**
 * One in-memory Airtable table served through the client's injectable sender for the recordChanged tests. It
 * mirrors the Go connector's internal/airtablefake: the base schema, and POST listRecords with the Last
 * modified time filter, sort, page size, and offset the Trigger sends, and GET of one record by ID for the
 * example Flow's read-back. Offsets name the next record and work only with their own query, as Airtable's
 * do. It never echoes the personal access token.
 */

import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';

export const BaseID = 'appIntakeBase0001';
export const TableID = 'tblIntakeLeads001';
export const FieldID = 'fldLastModified01';
export const AccessToken = 'patFAKE.0123456789abcdef';
export const Endpoint = 'https://airtable.fake.test';
const filterPattern = /^IS_AFTER\(\{(fld[0-9A-Za-z]{14})\}, DATETIME_PARSE\("([^"]+)"\)\)$/;

/** One fake record; an undefined modifiedAt leaves the Last modified time field empty. */
export interface FakeRecord { id: string; createdAt: Date; modifiedAt: Date | undefined; fields: Record<string, unknown> }
/** Replaces the next request's response. */
export interface ScriptedResponse { status: number; retryAfter?: string; errorType?: string }
/** One listRecords request body as the fake received it. */
export interface ListRequest { pageSize: number; offset?: string; filterByFormula: string; sort: Array<{ field: string; direction?: string }> }

/** Return a record created and last modified at modifiedAt, with an Email field. */
export function fakeRecord(id: string, modifiedAt: Date): FakeRecord {
  return { id, createdAt: modifiedAt, modifiedAt, fields: { Email: `${id}@example.test` } };
}

/** The fake provider. */
export class AirtableTableFake {
  readonly listRequests: ListRequest[] = [];
  readonly statuses: number[] = [];
  schemaRequests = 0;
  completedRequests = 0;
  private fieldName = 'Last Modified';
  private fieldType = 'lastModifiedTime';
  private readonly records = new Map<string, FakeRecord>();
  private readonly scripted: ScriptedResponse[] = [];
  private readonly iterators = new Map<string, string>();

  constructor(...records: FakeRecord[]) { this.put(...records); }

  /** The client's sender, bound to this fake. */
  readonly send = async (request: ProviderRequest): Promise<ProviderResponse> => {
    const response = this.answer(request);
    this.completedRequests++;
    this.statuses.push(response.status);
    return response;
  };

  /** Create or replace records, as an edit in Airtable would. */
  put(...records: FakeRecord[]): void { for (const record of records) this.records.set(record.id, record); }
  /** Rename the Last modified time field; its ID stays the same. */
  renameField(name: string): void { this.fieldName = name; }
  /** Change the watched field's type in the schema. */
  setFieldType(fieldType: string): void { this.fieldType = fieldType; }
  /** Forget every offset handed out, as an Airtable iterator timeout does. */
  expireIterators(): void { this.iterators.clear(); }
  /** Queue responses that replace the next requests' responses, in order. */
  script(...responses: ScriptedResponse[]): void { this.scripted.push(...responses); }

  private answer(request: ProviderRequest): ProviderResponse {
    if (header(request, 'authorization') !== `Bearer ${AccessToken}`) return errorResponse(401, 'AUTHENTICATION_REQUIRED');
    const scripted = this.scripted.shift();
    if (scripted) return errorResponse(scripted.status, scripted.errorType ?? '', scripted.retryAfter ? { 'retry-after': scripted.retryAfter } : {});
    const path = new URL(request.url).pathname;
    if (request.method === 'GET' && path === `/v0/meta/bases/${BaseID}/tables`) {
      this.schemaRequests++;
      const options = { isValid: true, referencedFieldIds: null, result: { type: 'dateTime' } };
      return jsonResponse(200, { tables: [{ id: TableID, name: 'Leads', primaryFieldId: 'fldLeadEmail00001', fields: [
        { id: 'fldLeadEmail00001', name: 'Email', type: 'email' },
        { id: FieldID, name: this.fieldName, type: this.fieldType, options },
      ] }] });
    }
    if (request.method === 'POST' && path === `/v0/${BaseID}/${TableID}/listRecords`) {
      return this.list(JSON.parse(Buffer.from(request.body ?? []).toString('utf8')) as ListRequest);
    }
    const recordPrefix = `/v0/${BaseID}/${TableID}/`;
    if (request.method === 'GET' && path.startsWith(`${recordPrefix}rec`)) {
      const record = this.records.get(path.slice(recordPrefix.length));
      return record ? jsonResponse(200, this.providerRecord(record)) : errorResponse(404, 'NOT_FOUND');
    }
    return errorResponse(404, 'NOT_FOUND');
  }

  private list(body: ListRequest): ProviderResponse {
    if (!Number.isInteger(body.pageSize) || body.pageSize < 1 || body.pageSize > 100) return errorResponse(422, 'INVALID_REQUEST_UNKNOWN');
    this.listRequests.push(body);
    const match = filterPattern.exec(body.filterByFormula ?? '');
    if (!match || match[1] !== FieldID || body.sort?.length !== 1 || body.sort[0]!.field !== FieldID) return errorResponse(422, 'INVALID_FILTER_BY_FORMULA');
    const since = Date.parse(match[2]!);
    const direction = body.sort[0]!.direction ?? 'asc';
    const matched = [...this.records.values()].filter(record => record.modifiedAt !== undefined && record.modifiedAt.getTime() > since);
    // Airtable leaves ties unordered; record ID descending exposes that.
    matched.sort((left, right) => {
      const compared = left.modifiedAt!.getTime() - right.modifiedAt!.getTime();
      if (compared !== 0) return direction === 'desc' ? -compared : compared;
      return left.id < right.id ? 1 : left.id > right.id ? -1 : 0;
    });
    const query = `${body.filterByFormula}|${direction}`;
    let start = 0;
    if (body.offset) {
      const separator = body.offset.indexOf('/');
      const position = matched.findIndex(record => record.id === body.offset!.slice(separator + 1));
      if (separator < 0 || this.iterators.get(body.offset) !== query || position < 0) return errorResponse(422, 'LIST_RECORDS_ITERATOR_NOT_AVAILABLE');
      start = position;
    }
    const end = Math.min(start + body.pageSize, matched.length);
    const records = matched.slice(start, end).map(record => this.providerRecord(record));
    const document: Record<string, unknown> = { records };
    if (end < matched.length) {
      const offset = `itr${this.iterators.size}/${matched[end]!.id}`;
      this.iterators.set(offset, query);
      document.offset = offset;
    }
    return jsonResponse(200, document);
  }

  private providerRecord(record: FakeRecord): Record<string, unknown> {
    const fields = { ...record.fields, ...(record.modifiedAt ? { [this.fieldName]: record.modifiedAt.toISOString() } : {}) };
    return { id: record.id, createdTime: record.createdAt.toISOString(), fields };
  }
}

function header(request: ProviderRequest, name: string): string | undefined {
  const entry = Object.entries(request.headers ?? {}).find(([key]) => key.toLowerCase() === name);
  const value = entry?.[1];
  return Array.isArray(value) ? value[0] : value;
}

function jsonResponse(status: number, document: unknown, headers: Record<string, string> = {}): ProviderResponse {
  return { status, headers: { 'content-type': 'application/json', ...headers }, body: Buffer.from(JSON.stringify(document), 'utf8') };
}

function errorResponse(status: number, errorType: string, headers: Record<string, string> = {}): ProviderResponse {
  return jsonResponse(status, { error: { type: errorType, message: 'SENTINEL provider message text' } }, headers);
}
