// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { once } from 'node:events';
import { createServer, type IncomingMessage, type Server, type ServerResponse } from 'node:http';

/** The only bearer token the fake accepts. */
export const FakeAccessToken = 'pat-na1-fake-hubspot-token';
const objectsPathPrefix = '/crm/objects/2026-09/';

interface FakeRecord { id: string; properties: Record<string, string>; createdAtMs: number; modifiedAtMs: number; isIndexed: boolean }

/** One scripted error answer to the next search. */
export interface FailedResponse { status: number; headers?: Record<string, string>; body: string }

/**
 * Serves HubSpot CRM search and record reads over a deterministic in-memory record set, as the Go connector's
 * internal/hubspotsearchfake does. Its clock moves one second per change, so modification times are deterministic.
 */
export class HubSpotSearchFake {
  /** The API base URL for Config.endpoint; set by start. */ url = '';
  private readonly server: Server;
  private readonly records = new Map<string, FakeRecord>();
  private readonly failures: FailedResponse[] = [];
  private readonly bodies: Record<string, unknown>[] = [];
  private clockMs: number;
  private nextID = 1000;
  private searches = 0;

  private constructor(private readonly objectType: string, startMs: number) {
    this.clockMs = Math.trunc(startMs);
    this.server = createServer((request, response) => { void this.serve(request, response); });
  }

  /** Start a fake serving objectType on a loopback port. */
  static async start(objectType: string, start: Date): Promise<HubSpotSearchFake> {
    const fake = new HubSpotSearchFake(objectType, start.getTime());
    fake.server.listen(0, '127.0.0.1');
    await once(fake.server, 'listening');
    const address = fake.server.address();
    if (!address || typeof address === 'string') throw new Error('fake HubSpot port unavailable');
    fake.url = `http://127.0.0.1:${address.port}`;
    return fake;
  }

  /** Stop serving. */ close(): Promise<void> { return new Promise(resolve => this.server.close(() => resolve())); }
  /** Store a new indexed record and return its ID. */ create(properties: Record<string, string>): string { this.clockMs += 1000; return this.store(properties, true); }
  /** Store a record that search omits until index, as HubSpot does while it indexes. */ createUnindexed(properties: Record<string, string>): string { this.clockMs += 1000; return this.store(properties, false); }
  /** Store count indexed records that share one modification time and return their IDs. */
  createAtSameTime(count: number, properties: Record<string, string>): string[] {
    this.clockMs += 1000;
    return Array.from({ length: count }, () => this.store(properties, true));
  }
  /** Set properties on a record and move its modification time forward. */
  update(id: string, properties: Record<string, string>): void {
    const record = this.records.get(id)!;
    this.clockMs += 1000;
    Object.assign(record.properties, properties);
    record.modifiedAtMs = this.clockMs;
  }
  /** Make an unindexed record visible to search. */ index(id: string): void { this.records.get(id)!.isIndexed = true; }
  /** Answer the next searches with responses, in order. */ failNextSearches(...responses: FailedResponse[]): void { this.failures.push(...responses); }
  /** The number of search requests received. */ searchRequestCount(): number { return this.searches; }
  /** The decoded search request bodies, in order. */ searchBodies(): Record<string, unknown>[] { return [...this.bodies]; }

  private store(properties: Record<string, string>, isIndexed: boolean): string {
    const id = String(++this.nextID);
    this.records.set(id, { id, properties: { ...properties }, createdAtMs: this.clockMs, modifiedAtMs: this.clockMs, isIndexed });
    return id;
  }

  private modifiedProperty(): string { return this.objectType === 'contacts' ? 'lastmodifieddate' : 'hs_lastmodifieddate'; }

  private async serve(request: IncomingMessage, response: ServerResponse): Promise<void> {
    let text = '';
    for await (const chunk of request) text += String(chunk);
    if (request.headers.authorization !== `Bearer ${FakeAccessToken}`) { writeJSON(response, 401, { category: 'INVALID_AUTHENTICATION' }); return; }
    const url = new URL(request.url ?? '/', 'http://fake');
    const prefix = `${objectsPathPrefix}${this.objectType}/`;
    if (!url.pathname.startsWith(prefix)) { writeJSON(response, 404, { category: 'OBJECT_NOT_FOUND' }); return; }
    const suffix = url.pathname.slice(prefix.length);
    if (suffix === 'search' && request.method === 'POST') { this.serveSearch(text, response); return; }
    if (request.method === 'GET') { this.serveRead(suffix, url.searchParams.get('properties') ?? '', response); return; }
    writeJSON(response, 404, { category: 'OBJECT_NOT_FOUND' });
  }

  private serveSearch(text: string, response: ServerResponse): void {
    let search: { filterGroups?: { filters: { propertyName: string; operator: string; value: string }[] }[]; sorts?: { direction: string }[]; properties?: string[]; limit?: number; after?: string };
    try { search = JSON.parse(text) as typeof search; } catch { writeJSON(response, 400, { category: 'VALIDATION_ERROR' }); return; }
    const limit = search.limit ?? 0;
    if ((search.sorts?.length ?? 0) > 1 || limit < 1 || limit > 200) { writeJSON(response, 400, { category: 'VALIDATION_ERROR' }); return; }
    this.searches++;
    this.bodies.push(search as Record<string, unknown>);
    const failure = this.failures.shift();
    if (failure) { response.writeHead(failure.status, { ...failure.headers, 'Content-Type': 'application/json' }).end(failure.body); return; }
    const offset = Number(search.after || '0');
    if (offset + limit > 10_000) { writeJSON(response, 400, { category: 'VALIDATION_ERROR' }); return; }
    const isDescending = search.sorts?.length === 1 && search.sorts[0]!.direction === 'DESCENDING';
    const matched = [...this.records.values()].filter(record => record.isIndexed && (search.filterGroups ?? []).every(group => group.filters.every(filter =>
      filter.propertyName === this.modifiedProperty() && filter.operator === 'GTE' && /^-?\d+$/.test(filter.value) && record.modifiedAtMs >= Number(filter.value))));
    matched.sort((left, right) => {
      const order = left.modifiedAtMs - right.modifiedAtMs || left.id.length - right.id.length || (left.id < right.id ? -1 : left.id > right.id ? 1 : 0);
      return isDescending ? -order : order;
    });
    const results = matched.slice(offset, offset + limit).map(record => this.recordJSON(record, search.properties ?? []));
    writeJSON(response, 200, { total: matched.length, results, ...(offset + limit < matched.length ? { paging: { next: { after: String(offset + limit) } } } : {}) });
  }

  private serveRead(id: string, properties: string, response: ServerResponse): void {
    const record = this.records.get(id);
    if (!record) { writeJSON(response, 404, { category: 'OBJECT_NOT_FOUND' }); return; }
    writeJSON(response, 200, this.recordJSON(record, properties ? properties.split(',') : []));
  }

  /** Return the requested properties, or every property when none are named. */
  private recordJSON(record: FakeRecord, names: string[]): Record<string, unknown> {
    const all: Record<string, string> = { hs_object_id: record.id, createdate: formatTime(record.createdAtMs), [this.modifiedProperty()]: formatTime(record.modifiedAtMs), ...record.properties };
    const properties = Object.fromEntries(Object.entries(all).filter(([name]) => names.length === 0 || names.includes(name)));
    return { id: record.id, properties, archived: false, createdAt: formatTime(record.createdAtMs), updatedAt: formatTime(record.modifiedAtMs) };
  }
}

function formatTime(milliseconds: number): string { return new Date(milliseconds).toISOString(); }

function writeJSON(response: ServerResponse, status: number, value: unknown): void {
  response.writeHead(status, { 'Content-Type': 'application/json' }).end(JSON.stringify(value));
}
