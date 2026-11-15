// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Buffer } from 'node:buffer';
import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';

/** The REST path the default connector configuration uses. */ export const DataPathPrefix = '/services/data/v62.0';
/** The instance URL the fake answers. */ export const FakeInstanceURL = 'https://fake-org.my.salesforce.com';

const seedQuery = /^SELECT Id, SystemModstamp FROM (\w+) WHERE SystemModstamp <= (\S+) ORDER BY SystemModstamp DESC, Id DESC LIMIT 1$/;
const changesQuery = /^SELECT (.+) FROM (\w+) WHERE SystemModstamp <= (\S+) AND (?:SystemModstamp > (\S+)|\(SystemModstamp > (\S+) OR \(SystemModstamp = (\S+) AND Id > '(\w+)'\)\)) ORDER BY SystemModstamp ASC, Id ASC LIMIT ([0-9]+)$/;

interface FakeRecord { sObjectType: string; id: string; createdMs: number; modifiedMs: number; modstampMs: number; fields: Record<string, unknown> }

/** One recorded REST request. */
export interface FakeRequest { path: string; soql: string; status: number }

/**
 * A stateful single-org Salesforce REST query and record read stand-in, as the Go connector's
 * internal/salesforcepollfake: it answers only the SOQL the Trigger sends and stamps every change with a
 * whole-second SystemModstamp that only moves forward. Pass send as the client's provider sender.
 */
export class SalesforceOrgFake {
  /** Every request served so far. */ readonly requests: FakeRequest[] = [];
  /** Objects whose queries answer 400 with the error code, such as INVALID_TYPE. */ readonly rejectedObjects = new Map<string, string>();
  /** Record IDs deleted right after a change query lists them, as a deletion before the read-back. */ readonly deleteAfterListing = new Set<string>();
  /** How far the org's clock runs ahead of clock(); negative puts Salesforce behind the client host. */ clockSkewMs = 0;
  private readonly records = new Map<string, FakeRecord>();
  private nextID = 1;
  private lastStampMs: number;
  private rateLimited = 0;
  private retryAfter = '';

  /** Start stamping one minute before clock(), so the settle delay never hides changes; clock defaults to now. */
  constructor(private readonly accessToken: string, private readonly clock: () => Date = () => new Date()) {
    this.lastStampMs = Math.floor((clock().getTime() - 60_000) / 1000) * 1000;
  }

  /** Answer one request as Salesforce would. */
  readonly send = async (request: ProviderRequest): Promise<ProviderResponse> => {
    const url = new URL(request.url);
    const soql = url.searchParams.get('q') ?? '';
    const [status, body] = this.answer(request, url, soql);
    this.requests.push({ path: url.pathname, soql, status });
    const headers: Record<string, string> = {
      'content-type': 'application/json', 'sforce-limit-info': 'api-usage=25/15000', date: new Date(this.serverNow()).toUTCString(),
    };
    if (status === 403 && this.retryAfter) headers['retry-after'] = this.retryAfter;
    return { status, headers, body: Buffer.from(body) };
  };

  /** Create a record and return its 18-character ID. */ insert(sObjectType: string, fields: Record<string, unknown>): string { return this.store(sObjectType, this.advanceStamp(), fields); }
  /** Create count records sharing one SystemModstamp, as a bulk load does. */
  insertBatch(sObjectType: string, count: number, fields: (index: number) => Record<string, unknown> = () => ({})): string[] {
    const stamp = this.advanceStamp();
    return Array.from({ length: count }, (_, index) => this.store(sObjectType, stamp, fields(index)));
  }
  /** Create a record stamped at the org's current second, as a change committed just now. */
  insertRecent(sObjectType: string, fields: Record<string, unknown>): string {
    let stamp = Math.floor(this.serverNow() / 1000) * 1000;
    if (stamp <= this.lastStampMs) stamp = this.lastStampMs + 1000;
    this.lastStampMs = stamp;
    return this.store(sObjectType, stamp, fields);
  }
  /** Merge fields and move LastModifiedDate and SystemModstamp forward. */
  update(id: string, fields: Record<string, unknown>): void {
    const record = this.records.get(id)!;
    const stamp = this.advanceStamp();
    record.modifiedMs = stamp; record.modstampMs = stamp;
    Object.assign(record.fields, fields);
  }
  /** Set a stored field without moving any stamp. */ setField(id: string, name: string, value: unknown): void { this.records.get(id)!.fields[name] = value; }
  /** Answer the next count requests with 403 REQUEST_LIMIT_EXCEEDED and retryAfter, if set. */
  rateLimitNext(count: number, retryAfter: string): void { this.rateLimited = count; this.retryAfter = retryAfter; }
  /** How many query requests were served. */ get queryCount(): number { return this.requests.filter(request => request.soql).length; }

  private store(sObjectType: string, stamp: number, fields: Record<string, unknown>): string {
    const id = `006RM${String(this.nextID++).padStart(10, '0')}AAA`;
    this.records.set(id, { sObjectType, id, createdMs: stamp, modifiedMs: stamp, modstampMs: stamp, fields: { ...fields } });
    return id;
  }

  private advanceStamp(): number {
    const floor = Math.floor((this.serverNow() - 60_000) / 1000) * 1000;
    this.lastStampMs = Math.max(this.lastStampMs + 1000, floor);
    return this.lastStampMs;
  }

  private serverNow(): number { return this.clock().getTime() + this.clockSkewMs; }

  private answer(request: ProviderRequest, url: URL, soql: string): [number, string] {
    if (request.headers?.Authorization !== 'Bearer ' + this.accessToken) return [401, errorBody('INVALID_SESSION_ID')];
    if (this.rateLimited > 0) { this.rateLimited--; return [403, errorBody('REQUEST_LIMIT_EXCEEDED')]; }
    if (url.pathname === DataPathPrefix + '/query') {
      for (const [sObjectType, errorCode] of this.rejectedObjects) if (soql.includes(` FROM ${sObjectType} `)) return [400, errorBody(errorCode)];
      return this.answerQuery(soql);
    }
    if (url.pathname.startsWith(DataPathPrefix + '/sobjects/')) {
      const [sObjectType, id] = url.pathname.slice((DataPathPrefix + '/sobjects/').length).split('/');
      const record = this.records.get(id ?? '');
      if (!record || record.sObjectType !== sObjectType) return [404, errorBody('NOT_FOUND')];
      return [200, JSON.stringify(recordObject(record, (url.searchParams.get('fields') ?? '').split(',')))];
    }
    return [404, errorBody('NOT_FOUND')];
  }

  private answerQuery(soql: string): [number, string] {
    const seed = seedQuery.exec(soql);
    if (seed) {
      const horizon = Date.parse(seed[2]!);
      return [200, queryBody(this.sorted(seed[1]!, record => record.modstampMs <= horizon).slice(-1), ['Id', 'SystemModstamp'])];
    }
    const changes = changesQuery.exec(soql);
    if (!changes) return [400, errorBody('MALFORMED_QUERY')];
    let afterText = changes[4];
    let afterID = '';
    if (afterText === undefined) {
      if (changes[6] !== changes[5]) return [400, errorBody('MALFORMED_QUERY')];
      afterText = changes[5]!; afterID = changes[7]!;
    }
    const horizon = Date.parse(changes[3]!);
    const after = Date.parse(afterText);
    const matching = this.sorted(changes[2]!, record =>
      (record.modstampMs > after || (afterID !== '' && record.modstampMs === after && record.id > afterID)) && record.modstampMs <= horizon,
    ).slice(0, Number(changes[8]));
    const body = queryBody(matching, changes[1]!.split(', '));
    for (const record of matching) if (this.deleteAfterListing.delete(record.id)) this.records.delete(record.id);
    return [200, body];
  }

  private sorted(sObjectType: string, isMatching: (record: FakeRecord) => boolean): FakeRecord[] {
    return [...this.records.values()].filter(record => record.sObjectType === sObjectType && isMatching(record))
      .sort((left, right) => left.modstampMs - right.modstampMs || (left.id < right.id ? -1 : left.id > right.id ? 1 : 0));
  }
}

function salesforceTime(milliseconds: number): string { return new Date(milliseconds).toISOString().slice(0, 19) + '.000+0000'; }

function queryBody(records: FakeRecord[], fields: string[]): string {
  const objects = records.map(record => recordObject(record, fields));
  return JSON.stringify({ totalSize: objects.length, done: true, records: objects });
}

/** Render the requested fields; a field the record never set is null, as Salesforce returns it. */
function recordObject(record: FakeRecord, fields: string[]): Record<string, unknown> {
  const rendered: Record<string, unknown> = { attributes: { type: record.sObjectType, url: `${DataPathPrefix}/sobjects/${record.sObjectType}/${record.id}` } };
  for (const name of fields) {
    if (name === 'Id') rendered[name] = record.id;
    else if (name === 'CreatedDate') rendered[name] = salesforceTime(record.createdMs);
    else if (name === 'LastModifiedDate') rendered[name] = salesforceTime(record.modifiedMs);
    else if (name === 'SystemModstamp') rendered[name] = salesforceTime(record.modstampMs);
    else rendered[name] = record.fields[name] ?? null;
  }
  return rendered;
}

function errorBody(errorCode: string): string {
  return JSON.stringify([{ message: 'fake Salesforce error', errorCode, fields: [] }]);
}
