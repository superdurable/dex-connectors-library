// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { pathEscape } from '../src/index.js';

/**
 * One Google Calendar's events.list full and incremental sync, a port of the Go connector's internal
 * calendarsyncfake. Sync tokens are "sync-<sequence>", and every insert, update, or cancel advances the
 * sequence, as Google's documented sync protocol behaves.
 */
const forbiddenWithSyncToken = ['iCalUID', 'orderBy', 'privateExtendedProperty', 'q', 'sharedExtendedProperty', 'timeMin', 'timeMax', 'updatedMin'];
const baseTimeMs = Date.UTC(2026, 2, 2, 9, 0, 0);

/** One recorded request with the status the fake answered. */
export interface RecordedRequest { path: string; query: string; status: number }

interface StoredEvent { id: string; summary: string; status: string; createdAt: number; sequence: number }

function rfc3339(minutes: number): string { return new Date(baseTimeMs + minutes * 60_000).toISOString().replace('.000Z', 'Z'); }
function googleError(code: number, reason: string): unknown { return { error: { code, errors: [{ reason }] } }; }

function resource(event: StoredEvent): Record<string, unknown> {
  if (event.status === 'cancelled') return { id: event.id, status: 'cancelled', etag: `"${event.sequence}"`, updated: rfc3339(event.sequence) };
  return {
    id: event.id, status: 'confirmed', summary: event.summary, etag: `"${event.sequence}"`, created: rfc3339(event.createdAt), updated: rfc3339(event.sequence),
    start: { dateTime: '2026-03-10T10:00:00Z' }, end: { dateTime: '2026-03-10T11:00:00Z' },
  };
}

/** A stateful single-calendar fake behind a CalendarClient send function. */
export class CalendarSyncFake {
  private sequence = 0;
  private readonly events = new Map<string, StoredEvent>();
  private oldestValidToken = 0;
  private throttled = 0;
  private throttleStatus = 0;
  private throttleReason = '';
  private retryAfter = '';
  private readonly recorded: RecordedRequest[] = [];
  /** Answer one provider request; pass it as CalendarClientOptions.send. */
  readonly send = async (request: ProviderRequest): Promise<ProviderResponse> => this.handle(request);

  constructor(readonly calendarId: string) {}

  /** Create a confirmed event. */ insert(eventId: string, summary: string): void { this.sequence++; this.events.set(eventId, { id: eventId, summary, status: 'confirmed', createdAt: this.sequence, sequence: this.sequence }); }
  /** Change an existing event's summary. */ update(eventId: string, summary: string): void { this.sequence++; Object.assign(this.events.get(eventId)!, { summary, sequence: this.sequence }); }
  /** Delete an existing event; incremental sync then reports it with status cancelled. */ cancel(eventId: string): void { this.sequence++; Object.assign(this.events.get(eventId)!, { status: 'cancelled', sequence: this.sequence }); }
  /** Make every sync token issued so far answer 410 Gone. */ expireSyncTokens(): void { this.sequence++; this.oldestValidToken = this.sequence; }
  /** Answer the next count requests with 429 and the given Retry-After header value. */ rateLimitNextRequests(count: number, retryAfter: string): void { this.throttleNextRequests(count, 429, 'rateLimitExceeded', retryAfter); }
  /** Answer the next count requests with status, a Google error reason, and Retry-After. */
  throttleNextRequests(count: number, status: number, reason: string, retryAfter: string): void { Object.assign(this, { throttled: count, throttleStatus: status, throttleReason: reason, retryAfter }); }
  /** The recorded requests in arrival order. */ requests(): RecordedRequest[] { return [...this.recorded]; }
  /** The token a full sync would issue now. */ syncToken(): string { return `sync-${this.sequence}`; }

  private handle(request: ProviderRequest): ProviderResponse {
    const url = new URL(request.url);
    const [status, body] = this.answer(request, url);
    this.recorded.push({ path: url.pathname, query: url.search.slice(1), status });
    const headers: Record<string, string> = { 'content-type': 'application/json' };
    if (status === this.throttleStatus && this.retryAfter) headers['retry-after'] = this.retryAfter;
    return { status, headers, body: Buffer.from(JSON.stringify(body)) };
  }

  private answer(request: ProviderRequest, url: URL): [number, unknown] {
    const params = url.searchParams;
    const eventsPath = `/calendars/${pathEscape(this.calendarId)}/events`;
    const path = url.pathname;
    if (request.method !== 'GET' || (!path.endsWith(eventsPath) && !path.includes(`${eventsPath}/`))) return [404, googleError(404, 'notFound')];
    if (!request.headers?.Authorization) return [401, googleError(401, 'authError')];
    if (this.throttled > 0) { this.throttled--; return [this.throttleStatus, googleError(this.throttleStatus, this.throttleReason)]; }
    if (path.includes(`${eventsPath}/`)) {
      const event = this.events.get(decodeURIComponent(path.slice(path.lastIndexOf('/') + 1)));
      return event ? [200, resource(event)] : [404, googleError(404, 'notFound')];
    }
    let after = -1;
    const token = params.get('syncToken') ?? '';
    if (token) {
      if (forbiddenWithSyncToken.some(name => params.has(name)) || params.get('showDeleted') === 'false' || !/^sync-\d+$/.test(token)) return [400, googleError(400, 'invalid')];
      if (Number(token.slice(5)) < this.oldestValidToken) return [410, googleError(410, 'fullSyncRequired')];
      after = Number(token.slice(5));
    }
    const pageSize = Number(params.get('maxResults') || '250');
    let offset = 0, snapshot = this.sequence;
    const pageToken = params.get('pageToken');
    if (pageToken) { const [, offsetText, snapshotText] = pageToken.split('-'); offset = Number(offsetText); snapshot = Number(snapshotText); }
    const items = [...this.events.values()]
      .filter(event => event.sequence <= snapshot && !(after < 0 && event.status === 'cancelled') && !(after >= 0 && event.sequence <= after))
      .sort((left, right) => left.sequence - right.sequence);
    const end = Math.min(offset + pageSize, items.length);
    const body: Record<string, unknown> = { kind: 'calendar#events', items: items.slice(offset, end).map(resource) };
    if (end < items.length) body.nextPageToken = `page-${end}-${snapshot}`;
    else body.nextSyncToken = `sync-${snapshot}`;
    return [200, body];
  }
}
