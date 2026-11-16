// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Buffer } from 'node:buffer';
import type { ConnectionRef, TriggerContext, TriggerEvent } from '@superdurable/connectors-sdk';
import { markCursorInvalid, markRateLimited, newTriggerCredentialCall, waitForPollRequest, type ChangeFetcher, type ChangePage, type Cursor } from '@superdurable/connectors-sdk/pollingtrigger';
import { goQuery, isRecordID, type SalesforceClient } from './client.js';
import type { QueryRecordsOutput, Record as SalesforceRecord, SOQLValue } from './generated/models.js';
import { decodeQueryPage } from './operations.js';
import { renderSOQL } from './soql.js';
import { RecordModifiedPolledTriggerDefinition, selectedFields, type RecordModifiedEvent, type RecordModifiedTriggerConfiguration } from './record_modified.js';
import {
  compareInstants, decodeRecordPollCursor, encodeRecordPollCursor, formatGoTime, formatRFC3339Seconds, isSameRecordID, parseSalesforceDateTime,
  recordChangeKind, recordModifiedEventID, stringField, type Instant, type RecordPollCursor,
} from './record_poll_cursor.js';

/** Leaves recent SystemModstamp values unread, so a transaction committing after its stamp is still read. */
const modstampSettleDelayMs = 15_000;
/** Absorbs the HTTP Date header's whole-second truncation. */
const serverClockToleranceMs = 1_000;
const recordModifiedOperation = 'recordModifiedPolled';
const maxEncodedQueryBytes = 15_000;
const minimumQueryBatchSize = 200;

/** One unsuccessful recordModifiedPolled poll; its message carries no record values or credentials. */
export class RecordPollError extends Error {
  /** Describe the failed poll. */
  constructor(message: string, options?: ErrorOptions) {
    super(message, options);
    this.name = 'RecordPollError';
  }
}

/** Reads one object's records in (SystemModstamp, Id) order for the poll Trigger kit. */
export class RecordPollFetcher implements ChangeFetcher<TriggerEvent<RecordModifiedEvent>> {
  /** How far Salesforce's clock was behind this host's at the last response, or zero. */
  private serverClockOffsetMs = 0;

  /** Bind the client, the connection whose credentials each poll resolves, and the validated configuration. */
  constructor(private readonly client: SalesforceClient, private readonly connection: ConnectionRef, private readonly configuration: RecordModifiedTriggerConfiguration) {}

  /**
   * Position the cursor at the newest record stamped before the settle horizon, or at the horizon for an empty
   * object, so existing records become the baseline and are never delivered.
   */
  async seedCursor(context: TriggerContext): Promise<Cursor> {
    const horizon = this.settleHorizon();
    const cursor = await this.seedCursorAt(context, horizon);
    // A Salesforce clock behind this host's must not seed past seconds Salesforce has not stamped yet.
    const serverHorizon = this.settleHorizon();
    return serverHorizon < horizon ? this.seedCursorAt(context, serverHorizon) : cursor;
  }

  /**
   * Read up to pollPageSize records ordered after cursor and stamped before the settle horizon. An undecodable
   * cursor, or one for another object, is marked invalid so the kit reseeds.
   */
  async fetchChanges(context: TriggerContext, cursor: Cursor): Promise<ChangePage<TriggerEvent<RecordModifiedEvent>>> {
    let decoded;
    try { decoded = decodeRecordPollCursor(cursor.token); } catch (error) { throw markCursorInvalid(error); }
    const { position } = decoded;
    if (position.sObjectType !== this.configuration.sObjectType) throw markCursorInvalid(new Error('Salesforce poll cursor belongs to another object'));
    let after: Instant = decoded.after;
    const horizon = this.settleHorizon();
    if (compareInstants(after, { milliseconds: horizon, nanoseconds: 0 }) >= 0) return { changes: [], next: cursor, hasMorePages: false };
    let statement: string;
    try { statement = this.changesStatement(position, after, horizon); }
    catch (error) { throw new RecordPollError(`Salesforce poll query could not be built: ${errorMessage(error)}`, { cause: error }); }
    const listed = await this.query(context, statement);
    const records = listed.records ?? [];
    const pageSize = this.client.pollPageSize;
    if (records.length > pageSize) throw new RecordPollError(`Salesforce poll page has ${records.length} records, more than the SOQL LIMIT ${pageSize}`);
    // The response may show Salesforce's clock behind; later records wait for a poll past its horizon.
    const serverHorizon = this.settleHorizon();
    const next: RecordPollCursor = { ...position };
    const changes: TriggerEvent<RecordModifiedEvent>[] = [];
    for (const record of records) {
      const modstamp = polledRecordModstamp(record);
      if (!isAfterCursor(modstamp, record.id ?? '', after, next.recordId))
        throw new RecordPollError('Salesforce poll page is not ordered by SystemModstamp after the cursor');
      if (modstamp > serverHorizon) return { changes, next: encodeRecordPollCursor(next), hasMorePages: false };
      changes.push(this.changeEvent(record, modstamp));
      after = { milliseconds: modstamp, nanoseconds: 0 };
      next.systemModstamp = formatRFC3339Seconds(modstamp);
      next.recordId = record.id ?? '';
    }
    // A full page, or a page Salesforce split, may hide more records; the next page continues from next.
    return { changes, next: encodeRecordPollCursor(next), hasMorePages: records.length === pageSize || !listed.isDone };
  }

  private async seedCursorAt(context: TriggerContext, horizon: number): Promise<Cursor> {
    let statement: string;
    try {
      statement = renderSOQL('SELECT Id, SystemModstamp FROM :object WHERE SystemModstamp <= :horizon ORDER BY SystemModstamp DESC, Id DESC LIMIT 1', {
        object: { kind: 'identifier', text: this.configuration.sObjectType }, horizon: { kind: 'dateTime', text: formatRFC3339Seconds(horizon) },
      });
    } catch (error) { throw new RecordPollError(`Salesforce baseline query could not be built: ${errorMessage(error)}`, { cause: error }); }
    const page = await this.query(context, statement);
    const position: RecordPollCursor = { sObjectType: this.configuration.sObjectType, systemModstamp: formatRFC3339Seconds(horizon), recordId: '' };
    const newest = page.records?.[0];
    if (newest) {
      position.systemModstamp = formatRFC3339Seconds(polledRecordModstamp(newest));
      position.recordId = newest.id ?? '';
    }
    return encodeRecordPollCursor(position);
  }

  private changeEvent(record: SalesforceRecord, modstamp: number): TriggerEvent<RecordModifiedEvent> {
    const sObjectType = this.configuration.sObjectType;
    const recordId = record.id ?? '';
    return {
      id: recordModifiedEventID(sObjectType, recordId, modstamp), occurredAt: new Date(modstamp),
      payload: { sObjectType, recordId, changeKind: recordChangeKind(record), systemModstamp: formatGoTime({ milliseconds: modstamp, nanoseconds: 0 }), record },
    };
  }

  /** Select the records ordered after the cursor; Id breaks ties within one second. */
  private changesStatement(position: RecordPollCursor, after: Instant, horizon: number): string {
    const bindings: Record<string, SOQLValue> = {
      object: { kind: 'identifier', text: this.configuration.sObjectType },
      after: { kind: 'dateTime', text: formatRFC3339Seconds(after.milliseconds) },
      horizon: { kind: 'dateTime', text: formatRFC3339Seconds(horizon) },
    };
    let condition = 'SystemModstamp > :after';
    if (position.recordId) {
      condition = '(SystemModstamp > :after OR (SystemModstamp = :after AND Id > :recordId))';
      bindings.recordId = { kind: 'string', text: position.recordId };
    }
    // Field names are validated API names, so joining them cannot inject an expression.
    return renderSOQL(`SELECT ${selectedFields(this.configuration).join(', ')} FROM :object WHERE SystemModstamp <= :horizon AND ${condition}` +
      ` ORDER BY SystemModstamp ASC, Id ASC LIMIT ${this.client.pollPageSize}`, bindings);
  }

  /** Send one SOQL request; REQUEST_LIMIT_EXCEEDED or 429 pauses the connection through the kit. */
  private async query(context: TriggerContext, statement: string): Promise<QueryRecordsOutput> {
    await waitForPollRequest(context);
    const call = newTriggerCredentialCall(this.connection, RecordModifiedPolledTriggerDefinition.trigger, context.signal);
    const credentials = await this.client.resolve(call, recordModifiedOperation);
    if ('route' in credentials) {
      // Only a revoked grant is a definite answer; every other resolution failure reads as unavailable.
      if (credentials.route === 'providerRejected') throw new RecordPollError(credentials.failure.message);
      throw new RecordPollError('Salesforce Trigger credentials are unavailable');
    }
    const query = { q: statement };
    if (Buffer.byteLength(goQuery(query)) > maxEncodedQueryBytes) throw new RecordPollError('Salesforce poll query is too long; select fewer fields');
    const response = await this.client.exchange(call, credentials, recordModifiedOperation, {
      method: 'GET', path: this.client.dataPath('query'), query, batchSize: Math.max(minimumQueryBatchSize, this.client.pollPageSize),
    });
    if ('route' in response) {
      if (response.responseDate === undefined) throw new RecordPollError(`Salesforce poll request failed: ${response.failure.message}`);
      this.observeServerClock(response.responseDate);
      const failure = new RecordPollError(`Salesforce poll request: ${response.failure.message}`);
      // The daily API allocation and concurrent request limit both report REQUEST_LIMIT_EXCEEDED.
      if (response.failure.kind === 'RATE_LIMIT') throw markRateLimited(failure, response.retryAfterMs);
      throw failure;
    }
    this.observeServerClock(firstHeader(response.headers.date));
    try { return decodeQueryPage(this.client, response.status, response.body); }
    catch (error) { throw new RecordPollError('Salesforce returned an invalid poll query page', { cause: error }); }
  }

  /** The settle delay before the earlier of this host's and Salesforce's clocks, in whole-second Unix milliseconds. */
  private settleHorizon(): number {
    return Math.floor((this.client.now().getTime() + this.serverClockOffsetMs - modstampSettleDelayMs) / 1000) * 1000;
  }

  /** Record how far Salesforce's clock, from a response Date header, is behind this host's. */
  private observeServerClock(dateHeader: string): void {
    const serverTime = Date.parse(dateHeader);
    if (!dateHeader || Number.isNaN(serverTime)) return;
    const offset = serverTime - this.client.now().getTime();
    this.serverClockOffsetMs = offset > -serverClockToleranceMs ? 0 : offset;
  }
}

/** Require an ID and a whole-second SystemModstamp, which SOQL dateTime literals match; returns Unix milliseconds. */
export function polledRecordModstamp(record: SalesforceRecord): number {
  const text = stringField(record, 'SystemModstamp');
  if (!isRecordID(record.id ?? '') || text === undefined) throw new RecordPollError('Salesforce poll record lacks Id or SystemModstamp');
  const modstamp = parseSalesforceDateTime(text);
  if (!modstamp || modstamp.nanoseconds !== 0 || modstamp.milliseconds % 1000 !== 0)
    throw new RecordPollError('Salesforce poll record SystemModstamp is not a whole-second dateTime');
  return modstamp.milliseconds;
}

/** Check the order SOQL promises without assuming how Salesforce collates Id values, so a repeated record cannot loop. */
function isAfterCursor(modstamp: number, recordID: string, after: Instant, afterRecordID: string): boolean {
  const order = compareInstants({ milliseconds: modstamp, nanoseconds: 0 }, after);
  return order > 0 || (order === 0 && afterRecordID !== '' && !isSameRecordID(recordID, afterRecordID));
}

function firstHeader(value: string | string[] | number | undefined): string {
  return typeof value === 'string' ? value : Array.isArray(value) ? value[0] ?? '' : '';
}

function errorMessage(error: unknown): string { return error instanceof Error ? error.message : String(error); }
