// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createServer, type IncomingMessage, type Server, type ServerResponse } from 'node:http';
import type { AddressInfo } from 'node:net';
import { SecretString, StaticCredentialProvider, type ConnectionRef } from '@superdurable/connectors-sdk';
import type { Credentials } from '../../src/generated/models.js';

/** The only bearer token the fake accepts. */ export const FakeAccessToken = '1000.zohoDeskTriggerAccess0123456789';
/** The only orgId header the fake accepts. */ export const FakeOrganizationID = '2389290';

/** A static US data center credential holding the fake's access token for reference. */
export function fakeCredentialSource(reference: ConnectionRef, accessToken = FakeAccessToken): StaticCredentialProvider<Credentials> {
  const credentials = { authMethodId: 'zoho-us-oauth', oauthClientId: 'fake-client', oauthClientSecret: new SecretString('fake-secret'), accessToken: new SecretString(accessToken), refreshToken: new SecretString('fake-refresh') };
  return new StaticCredentialProvider<Credentials>([[reference, credentials]]);
}

const maximumSearchLimit = 100;
const maximumSearchFrom = 999;

interface FakeTicket { id: string; subject: string; status: string; departmentId: string; createdAt: number; modifiedAt: number }

/** One received search request. */
export interface FakeSearch { modifiedTimeRange: string; from: number; limit: number; departmentId: string }

/**
 * A local Zoho Desk stand-in for the ticket search and ticket read endpoints the ticketChanged Trigger and its
 * example use, mirroring the Go connector's internal/desksearchfake: modifiedTimeRange filtering, ascending
 * modifiedTime order, from and limit paging within Zoho Desk's documented bounds, a Date header from the fake's
 * clock, an indexing delay, a ticket listed twice, and injected 429 and 422 responses. Its clock is the wall clock
 * truncated to milliseconds.
 */
export class DeskSearchFake {
  /** Every search received, in order. */ readonly searches: FakeSearch[] = [];
  private nextId = 1892000000200000n;
  private readonly tickets = new Map<string, FakeTicket>();
  private readonly hidden = new Set<string>();
  private readonly duplicates = new Set<string>();
  private rateLimits = 0;
  private retryAfter = '';
  private rejectedRanges = 0;

  private constructor(private readonly server: Server, /** The API base URL, ending in /api/v1. */ readonly baseURL: string) {}

  /** Start the fake on a loopback port. */
  static async start(): Promise<DeskSearchFake> {
    const handlers: { fake?: DeskSearchFake } = {};
    const server = createServer((request, response) => handlers.fake!.serve(request, response));
    await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
    handlers.fake = new DeskSearchFake(server, `http://127.0.0.1:${(server.address() as AddressInfo).port}/api/v1`);
    return handlers.fake;
  }

  /** Stop accepting requests. */ async close(): Promise<void> { await new Promise<void>(resolve => this.server.close(() => resolve())); }

  /** Add a ticket created and modified now and return its ID. */
  create(subject: string, departmentId: string): string {
    this.nextId += 1n;
    const id = String(this.nextId);
    const now = Date.now();
    this.tickets.set(id, { id, subject, status: 'Open', departmentId, createdAt: now, modifiedAt: now });
    return id;
  }

  /** Set a ticket's status and move its modified time to now. */
  update(id: string, status: string): void {
    const ticket = this.tickets.get(id)!;
    ticket.status = status;
    ticket.modifiedAt = Date.now();
  }

  /** Model the indexing delay: the ticket is readable but absent from search until shown. */
  hideFromSearch(id: string, isHidden: boolean): void { if (isHidden) this.hidden.add(id); else this.hidden.delete(id); }
  /** Model a search index that lists the ticket a second time on the page holding it. */ listTwice(id: string): void { this.duplicates.add(id); }
  /** Answer the next count searches with 429 and retryAfter, which may be empty. */ rateLimitNext(count: number, retryAfter: string): void { this.rateLimits = count; this.retryAfter = retryAfter; }
  /** Answer the next count searches with 422 naming modifiedTimeRange. */ rejectNextRanges(count: number): void { this.rejectedRanges = count; }

  private serve(request: IncomingMessage, response: ServerResponse): void {
    // Zoho Desk, like any HTTP server, dates each response with its own clock.
    response.setHeader('Date', new Date().toUTCString());
    if (request.headers.authorization !== `Zoho-oauthtoken ${FakeAccessToken}`) return json(response, 401, { errorCode: 'INVALID_OAUTH' });
    if (request.headers.orgid !== FakeOrganizationID) return json(response, 403, { errorCode: 'OAUTH_ORG_MISMATCH' });
    const url = new URL(request.url ?? '/', 'http://fake');
    const path = url.pathname.replace(/^\/api\/v1/, '');
    if (request.method === 'GET' && path === '/tickets/search') return this.search(url.searchParams, response);
    if (request.method === 'GET' && path.startsWith('/tickets/')) return this.read(path.slice('/tickets/'.length).split('/'), response);
    json(response, 404, { errorCode: 'URL_NOT_FOUND' });
  }

  private search(query: URLSearchParams, response: ServerResponse): void {
    const from = /^[0-9]+$/.test(query.get('from') ?? '') ? Number(query.get('from')) : -1;
    const limit = /^[0-9]+$/.test(query.get('limit') ?? '') ? Number(query.get('limit')) : -1;
    const departmentId = query.get('departmentId') ?? '';
    const range = query.get('modifiedTimeRange') ?? '';
    this.searches.push({ modifiedTimeRange: range, from, limit, departmentId });
    if (this.rateLimits > 0) {
      this.rateLimits--;
      if (this.retryAfter) response.setHeader('Retry-After', this.retryAfter);
      return json(response, 429, { errorCode: 'THRESHOLD_EXCEEDED' });
    }
    const [lower, upper] = range.split(',').map(value => (/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$/.test(value) ? Date.parse(value) : NaN));
    if (this.rejectedRanges > 0 || !Number.isFinite(lower) || !Number.isFinite(upper)) {
      this.rejectedRanges = Math.max(0, this.rejectedRanges - 1);
      return json(response, 422, invalid('/modifiedTimeRange'));
    }
    if (from < 0 || from > maximumSearchFrom || limit < 1 || limit > maximumSearchLimit || query.get('sortBy') !== 'modifiedTime') return json(response, 422, invalid('/from'));
    const matches = [...this.tickets.values()]
      .filter(ticket => !this.hidden.has(ticket.id) && ticket.modifiedAt >= lower! && ticket.modifiedAt <= upper! && (!departmentId || ticket.departmentId === departmentId))
      .sort((left, right) => left.modifiedAt - right.modifiedAt || (left.id < right.id ? -1 : left.id > right.id ? 1 : 0));
    if (from >= matches.length) { response.writeHead(204).end(); return; }
    const data = matches.slice(from, from + limit).flatMap(ticket => (this.duplicates.has(ticket.id) ? [wire(ticket), wire(ticket)] : [wire(ticket)]));
    // A listed-twice ticket takes a page slot, as an index entry would.
    json(response, 200, { data: data.slice(0, limit), count: String(matches.length) });
  }

  private read(segments: string[], response: ServerResponse): void {
    const ticket = this.tickets.get(segments[0] ?? '');
    if (!ticket) return json(response, 404, { errorCode: 'URL_NOT_FOUND' });
    if (segments.length === 1) return json(response, 200, wire(ticket));
    if (segments.length === 2 && (segments[1] === 'threads' || segments[1] === 'comments')) return json(response, 200, { data: [] });
    json(response, 404, { errorCode: 'URL_NOT_FOUND' });
  }
}

function wire(ticket: FakeTicket): Record<string, unknown> {
  return {
    id: ticket.id, ticketNumber: ticket.id.replace(/^18920000002/, ''), subject: ticket.subject, description: `<div>${ticket.subject}</div>`,
    status: ticket.status, statusType: ticket.status === 'Closed' || ticket.status === 'On Hold' ? ticket.status : 'Open', priority: 'Medium', channel: 'Email',
    departmentId: ticket.departmentId, contactId: '1892000000042032', email: 'jane@example.test',
    createdTime: new Date(ticket.createdAt).toISOString(), modifiedTime: new Date(ticket.modifiedAt).toISOString(),
  };
}

function invalid(fieldName: string): Record<string, unknown> {
  return { errorCode: 'UNPROCESSABLE_ENTITY', errors: [{ fieldName, errorType: 'invalid' }] };
}

function json(response: ServerResponse, status: number, value: unknown): void {
  response.writeHead(status, { 'Content-Type': 'application/json' }).end(JSON.stringify(value));
}
