// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { TriggerDeliveryAbortedError, type ConnectionRef, type TriggerContext, type TriggerEvent, type TriggerRef } from '@superdurable/connectors-sdk';
import { MaxChangesPerPage, markCursorInvalid, markRateLimited, newTriggerCredentialCall, waitForPollRequest, type ChangeFetcher, type ChangePage, type Cursor } from '@superdurable/connectors-sdk/pollingtrigger';
import { encodeGoJSONValue } from '@superdurable/connectors-sdk/projectconfig';
import type { BambooHRClient } from './client.js';
import { selectGoFields, parseFirstOrderedJSONValue, parseOrderedJSON, type OrderedJSONValue } from './go_json.js';
import { addGoTimeSeconds, compareGoTimes, formatGoTime, goTimeFromDate, goTimeInUTC, goTimeToDate, isZeroGoTime, parseGoTime, parseHTTPDate, type GoTime } from './go_time.js';
import type { ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';

/** One employee BambooHR lists as changed; the payload of an employeeChanged Trigger event. */
export interface EmployeeChangedEvent {
  /** BambooHR's internal employee ID, a digit string such as 140. */ employeeId: string;
  /** BambooHR's change action: Inserted, Updated, or Deleted. A termination is Updated. */ action: string;
  /** When BambooHR last changed the employee, in UTC with second precision, as Go's time.Time JSON. */ lastChanged: string;
}

/** The employeeChanged binding configuration that the changeTypePicker unit saves in Dex Web. */
export interface EmployeeChangedTriggerConfiguration {
  /** Keeps one BambooHR change type: inserted, updated, or deleted. Blank or omitted delivers every change. */ changeType?: string | undefined;
}

/** The change types a binding may keep; blank delivers every change. */
export const EmployeeChangeTypes = ['inserted', 'updated', 'deleted'] as const;
/** BambooHR's action for an added employee. */ export const EmployeeChangeActionInserted = 'Inserted';
/** BambooHR's action for a changed employee, including a termination. */ export const EmployeeChangeActionUpdated = 'Updated';
/** BambooHR's action for a deleted employee record. */ export const EmployeeChangeActionDeleted = 'Deleted';
/**
 * How far behind BambooHR's Date header a change must be before it is delivered, in milliseconds. It covers
 * changes that become visible after a later second was already listed and any clock difference.
 */
export const EmployeeChangeSettleWindowMs = 30_000;

const triggerName = 'employeeChanged';
const cursorVersion = 1;
const employeeIdPattern = /^[1-9][0-9]{0,18}$/;
const actionPattern = /^[A-Za-z]{1,32}$/;
const undecodableCursor = 'BambooHR employeeChanged cursor is not decodable';

/** Throw unless the configuration keeps inserted, updated, deleted, or every change. */
export function validateEmployeeChangedTriggerConfiguration(configuration: EmployeeChangedTriggerConfiguration): void {
  const changeType = configuration.changeType ?? '';
  if (changeType && !(EmployeeChangeTypes as readonly string[]).includes(changeType)) {
    throw new TypeError(`bamboohr employeeChanged configuration: BambooHR employeeChanged changeType must be inserted, updated, deleted, or blank, not ${JSON.stringify(changeType)}`);
  }
}

/** The Trigger event ID employee-<id>-<action>-<lastChanged Unix seconds>, stable across refetches. */
export function employeeChangedEventId(employeeId: string, action: string, lastChangedUnixSeconds: number): string {
  return `employee-${employeeId}-${action.toLowerCase()}-${lastChangedUnixSeconds}`;
}

interface Change { employeeId: string; action: string; lastChanged: GoTime }
interface Position { since: GoTime; afterEmployeeId: string; changeType: string }

/** Reads one binding's changed-employees list for the poll Trigger kit, at parity with the Go connector. */
export class EmployeeChangeFetcher implements ChangeFetcher<TriggerEvent<EmployeeChangedEvent>> {
  private readonly changeType: string;

  /** Bind the client, connection, Trigger identity, and validated configuration. */
  constructor(private readonly client: BambooHRClient, private readonly connection: ConnectionRef, private readonly trigger: TriggerRef, configuration: EmployeeChangedTriggerConfiguration) {
    this.changeType = configuration.changeType ?? '';
  }

  /** Start at BambooHR's clock from the Date header, or the local clock; the request also proves the key. */
  async seedCursor(context: TriggerContext): Promise<Cursor> {
    const response = await this.listChangedEmployees(context, goTimeFromDate(this.client.currentTime()));
    decodeOrThrow(response.body);
    return { token: encodeCursor({ since: this.bambooHRTime(response), afterEmployeeId: '', changeType: this.changeType }) };
  }

  /** Return the oldest MaxChangesPerPage settled changes after cursor, by lastChanged then employee ID. */
  async fetchChanges(context: TriggerContext, cursor: Cursor): Promise<ChangePage<TriggerEvent<EmployeeChangedEvent>>> {
    const position = decodeCursor(cursor.token);
    if (!position) throw markCursorInvalid(new Error(undecodableCursor));
    if (position.changeType !== this.changeType) throw markCursorInvalid(new Error('BambooHR employeeChanged cursor was read with another changeType'));
    const response = await this.listChangedEmployees(context, position.since);
    const settledThrough = addGoTimeSeconds(this.bambooHRTime(response), -EmployeeChangeSettleWindowMs / 1000);
    const after = decodeOrThrow(response.body)
      .filter(change => compareGoTimes(change.lastChanged, settledThrough) <= 0 && isAfterPosition(change, position))
      .sort((left, right) => compareGoTimes(left.lastChanged, right.lastChanged) || compareEmployeeIds(left.employeeId, right.employeeId));
    const page = after.slice(0, MaxChangesPerPage);
    const last = page.at(-1);
    const next: Position = last ? { since: last.lastChanged, afterEmployeeId: last.employeeId, changeType: this.changeType } : { ...position, changeType: this.changeType };
    return { changes: page.map(changeTriggerEvent), next: { token: encodeCursor(next) }, hasMorePages: after.length > MaxChangesPerPage };
  }

  /** Send GET /employees/changed one second before since, as listEmployeeChanges does. */
  private async listChangedEmployees(context: TriggerContext, since: GoTime): Promise<ProviderResponse> {
    await waitForPollRequest(context);
    const call = newTriggerCredentialCall(this.connection, this.trigger, context.signal);
    const credential = await this.client.resolveKey(call);
    if (credential.kind !== 'resolved') throw new Error('BambooHR employeeChanged poll: connection credentials are unavailable');
    const query = new URLSearchParams({ since: formatGoTime(addGoTimeSeconds({ ...goTimeInUTC(since), nanoseconds: 0 }, -1)) });
    if (this.changeType) query.set('type', this.changeType);
    const exchange = await this.client.exchange(call, triggerName, credential.key, 'GET', '/employees/changed', query, undefined, context.signal);
    if (context.signal.aborted) throw new TriggerDeliveryAbortedError();
    if (exchange.kind === 'success') return exchange.response;
    const message = exchange.failure.message;
    // BambooHR throttles per company and API key, so a 429 or 503 pauses every binding.
    if (exchange.kind === 'rateLimited' || exchange.status === 503) throw markRateLimited(new Error(message), exchange.retryAfterMs);
    if (exchange.status === 400) throw markCursorInvalid(new Error(message));
    if (exchange.failure.kind === 'RESPONSE_TOO_LARGE') throw new Error('BambooHR changed-employees list exceeds maxResponseBytes; raise maxResponseBytes on the connection');
    throw new Error(`BambooHR employeeChanged poll: ${message}`);
  }

  /** BambooHR's clock from the Date header, or the local clock, in whole UTC seconds. */
  private bambooHRTime(response: ProviderResponse): GoTime {
    const header = response.headers.date;
    const serverTime = parseHTTPDate(Array.isArray(header) ? header[0] ?? '' : header ?? '');
    return serverTime && !isZeroGoTime(serverTime) ? serverTime : goTimeFromDate(this.client.currentTime());
  }
}

function changeTriggerEvent(change: Change): TriggerEvent<EmployeeChangedEvent> {
  return {
    id: employeeChangedEventId(change.employeeId, change.action, change.lastChanged.unixSeconds),
    occurredAt: goTimeToDate(change.lastChanged),
    payload: { employeeId: change.employeeId, action: change.action, lastChanged: formatGoTime(change.lastChanged) },
  };
}

function isAfterPosition(change: Change, position: Position): boolean {
  const comparison = compareGoTimes(change.lastChanged, position.since);
  if (comparison) return comparison > 0;
  return !position.afterEmployeeId || compareEmployeeIds(change.employeeId, position.afterEmployeeId) > 0;
}

function compareEmployeeIds(left: string, right: string): number {
  return left.length - right.length || (left < right ? -1 : left > right ? 1 : 0);
}

function decodeOrThrow(body: Uint8Array): Change[] {
  try { return decodeEmployeeChanges(body); }
  catch (error) { throw new Error(`BambooHR returned an invalid change list: ${error instanceof Error ? error.message : String(error)}`); }
}

/** Decode BambooHR's changed-employees map as the Go connector does. */
function decodeEmployeeChanges(body: Uint8Array): Change[] {
  let document: OrderedJSONValue;
  // Keep a byte order mark so it fails as in Go; TextDecoder strips it by default.
  try { document = parseOrderedJSON(new TextDecoder('utf-8', { ignoreBOM: true }).decode(body)); } catch { throw new Error('the response is not a change list'); }
  if (document.kind !== 'object' && document.kind !== 'null') throw new Error('the response is not a change list');
  const employees = document.kind === 'object' ? selectGoFields(document.members, ['employees']).get('employees') : undefined;
  if (!employees) throw new Error('the response is not a change list');
  // A PHP encoder writes an empty map as [], which is how a period without changes may arrive.
  if (employees.kind === 'array' && employees.items.length === 0) return [];
  if (employees.kind !== 'object') throw new Error('employees is not a map of changes');
  const entries = new Map<string, Map<string, OrderedJSONValue>>();
  for (const [key, entry] of employees.members) {
    if (entry.kind !== 'object' && entry.kind !== 'null') throw new Error('employees is not a map of changes');
    const fields = selectGoFields(entry.kind === 'object' ? entry.members : [], ['id', 'action', 'lastChanged']);
    for (const name of ['action', 'lastChanged']) {
      const value = fields.get(name);
      if (value && value.kind !== 'string' && value.kind !== 'null') throw new Error('employees is not a map of changes');
    }
    entries.set(key, fields);
  }
  return [...entries].map(([employeeId, fields]) => {
    const id = fields.get('id');
    if (id && decodeEmployeeIdValue(id) !== employeeId) throw new Error('a change names an invalid employee ID');
    if (!employeeIdPattern.test(employeeId)) throw new Error('a change names an invalid employee ID');
    const action = stringField(fields.get('action'));
    if (!actionPattern.test(action)) throw new Error(`employee ${employeeId} has an invalid action`);
    let lastChanged: GoTime;
    try { lastChanged = goTimeInUTC(parseGoTime(stringField(fields.get('lastChanged')))); } catch { throw new Error(`employee ${employeeId} has an invalid lastChanged`); }
    return { employeeId, action, lastChanged };
  });
}

function stringField(value: OrderedJSONValue | undefined): string {
  return value?.kind === 'string' ? value.value : '';
}

function decodeEmployeeIdValue(value: OrderedJSONValue): string {
  const text = value.kind === 'string' ? value.value : value.kind === 'number' ? value.text : value.kind === 'null' ? '' : undefined;
  if (text === undefined || !employeeIdPattern.test(text)) throw new Error('a change names an invalid employee ID');
  return text;
}

/** Encode the position as Go's json.Marshal does: struct field order, omitempty, HTML-safe strings. */
function encodeCursor(position: Position): string {
  let since: string;
  try { since = formatGoTime(position.since); }
  catch (error) { throw new Error(`encode BambooHR employeeChanged cursor: json: error calling MarshalJSON for type time.Time: ${error instanceof Error ? error.message : String(error)}`); }
  const members = [`"v":${cursorVersion}`, `"since":${encodeGoJSONValue(since)}`];
  if (position.afterEmployeeId) members.push(`"afterEmployeeId":${encodeGoJSONValue(position.afterEmployeeId)}`);
  if (position.changeType) members.push(`"changeType":${encodeGoJSONValue(position.changeType)}`);
  return `{${members.join(',')}}`;
}

/** Decode the token as Go's json.Decoder with DisallowUnknownFields does, or undefined when Go would fail. */
function decodeCursor(token: string): Position | undefined {
  let document: OrderedJSONValue;
  try { document = parseFirstOrderedJSONValue(token).value; } catch { return undefined; }
  if (document.kind !== 'object') return undefined;
  const known = new Map([['v', 'v'], ['since', 'since'], ['afteremployeeid', 'afterEmployeeId'], ['changetype', 'changeType']]);
  let version = 0, since: GoTime | undefined, afterEmployeeId = '', changeType = '';
  for (const [key, value] of document.members) {
    const name = known.get(key.toLowerCase());
    if (!name) return undefined;
    if (value.kind === 'null') continue;
    if (name === 'v') {
      if (value.kind !== 'number' || !/^-?\d+$/.test(value.text)) return undefined;
      version = Number(value.text);
      continue;
    }
    if (value.kind !== 'string') return undefined;
    if (name === 'since') { try { since = parseGoTime(value.value); } catch { return undefined; } }
    else if (name === 'afterEmployeeId') afterEmployeeId = value.value;
    else changeType = value.value;
  }
  if (version !== cursorVersion || !since || isZeroGoTime(since)) return undefined;
  if (afterEmployeeId && !employeeIdPattern.test(afterEmployeeId)) return undefined;
  if (changeType && !(EmployeeChangeTypes as readonly string[]).includes(changeType)) return undefined;
  return { since, afterEmployeeId, changeType };
}
