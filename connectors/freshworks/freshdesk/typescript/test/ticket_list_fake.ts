// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';

const maxListPage = 300;

interface StoredTicket { id: number; subject: string; status: number; createdAtMs: number; updatedAtMs: number }
interface InjectedFailure { status: number; retryAfter: string; body: string }

const nowSecond = (): number => Math.floor(Date.now() / 1000) * 1000;
const formatTime = (milliseconds: number): string => `${new Date(milliseconds).toISOString().slice(0, 19)}Z`;

/**
 * A stateful Freshdesk ticket store for ticketChanged tests, served through FreshdeskClientOptions.send as the Go
 * connector's freshdeskticketfake serves it: List All Tickets, View a Ticket, and List All Conversations. It
 * stamps created_at and updated_at from the real clock at one-second precision unless a test passes a time.
 */
export class FreshdeskTicketFake {
  /** The base URL to pass as apiBaseURL. */ readonly baseURL = 'https://acme.freshdesk.com/api/v2';
  readonly #authorization: string;
  readonly #tickets = new Map<number, StoredTicket>();
  readonly #failures: InjectedFailure[] = [];
  readonly #listQueries: URLSearchParams[] = [];
  #clockSkewMs = 0;

  /** Start an empty store that accepts apiKey over HTTP Basic. */
  constructor(apiKey: string) { this.#authorization = `Basic ${Buffer.from(`${apiKey}:X`).toString('base64')}`; }

  /** Serve one request; pass as FreshdeskClientOptions.send. */
  readonly send = async (request: ProviderRequest): Promise<ProviderResponse> => this.#serve(request);

  /** Add an Open ticket created and updated now. */
  create(ticketId: number, subject: string): void { const now = nowSecond(); this.createAt(ticketId, subject, now, now); }

  /** Add an Open ticket with explicit created_at and updated_at times in milliseconds. */
  createAt(ticketId: number, subject: string, createdAtMs: number, updatedAtMs: number): void {
    this.#tickets.set(ticketId, { id: ticketId, subject, status: 2, createdAtMs, updatedAtMs });
  }

  /** Change a ticket's subject and status and stamp updated_at at least one second later. */
  update(ticketId: number, subject: string, status: number): void {
    const ticket = this.#tickets.get(ticketId)!;
    ticket.subject = subject; ticket.status = status;
    ticket.updatedAtMs = Math.max(nowSecond(), ticket.updatedAtMs + 1000);
  }

  /** Move the time the Date header reports, as minutes passing on Freshdesk's clock. */
  advanceClock(milliseconds: number): void { this.#clockSkewMs += milliseconds; }

  /** Answer the next List All Tickets request with status, Retry-After, and an error body. */
  failNextList(status: number, retryAfter: string, body: string): void { this.#failures.push({ status, retryAfter, body }); }

  /** Return how many List All Tickets requests arrived. */
  listRequestCount(): number { return this.#listQueries.length; }

  /** Return the List All Tickets queries in arrival order. */
  listQueries(): URLSearchParams[] { return [...this.#listQueries]; }

  #serve(request: ProviderRequest): ProviderResponse {
    const headers: Record<string, string> = { date: new Date(Date.now() + this.#clockSkewMs).toUTCString(), 'content-type': 'application/json' };
    const url = new URL(request.url);
    if (request.headers?.Authorization !== this.#authorization) return json(401, { code: 'invalid_credentials' }, headers);
    if (request.method !== 'GET') return json(405, { code: 'method_not_allowed' }, headers);
    const path = url.pathname.replace(/^\/api\/v2/, '');
    if (path === '/tickets') { this.#listQueries.push(url.searchParams); return this.#listTickets(url.searchParams, headers); }
    const parts = path.replace(/^\/tickets\//, '').split('/');
    const ticket = /^\d+$/.test(parts[0]!) ? this.#tickets.get(Number(parts[0])) : undefined;
    if (!ticket || parts.length > 2 || (parts.length === 2 && parts[1] !== 'conversations')) return json(404, { code: 'access_denied' }, headers);
    if (parts.length === 2) return json(200, [], headers);
    return json(200, { ...ticketWire(ticket), description_text: `Customer message for ${ticket.subject}`, requester: { id: 6007738334, name: 'Jane Smith', email: 'jane@acme.example.com' } }, headers);
  }

  #listTickets(query: URLSearchParams, headers: Record<string, string>): ProviderResponse {
    const failure = this.#failures.shift();
    if (failure) {
      if (failure.retryAfter) headers['retry-after'] = failure.retryAfter;
      return { status: failure.status, headers, body: Buffer.from(failure.body) };
    }
    const since = Date.parse(query.get('updated_since') ?? '');
    const perPage = Number(query.get('per_page'));
    const page = Number(query.get('page'));
    if (!Number.isFinite(since) || !Number.isInteger(perPage) || perPage < 1 || perPage > 100 || !Number.isInteger(page) || page < 1
      || query.get('order_by') !== 'updated_at' || query.get('order_type') !== 'asc') {
      return json(400, { description: 'Validation failed', errors: [{ field: 'updated_since', code: 'invalid_value', message: 'bad' }] }, headers);
    }
    const matched = [...this.#tickets.values()].filter(ticket => ticket.updatedAtMs >= since)
      .sort((left, right) => left.updatedAtMs - right.updatedAtMs || left.id - right.id);
    const start = Math.min((page - 1) * perPage, matched.length);
    const end = Math.min(start + perPage, matched.length);
    // Freshdesk returns at most 300 pages (30,000 tickets) for one list query.
    if (end < matched.length && page < maxListPage) {
      const next = new URLSearchParams(query);
      next.set('page', String(page + 1));
      headers.link = `<${this.baseURL}/tickets?${next.toString()}>; rel="next"`;
    }
    return json(200, matched.slice(start, end).map(ticketWire), headers);
  }
}

function ticketWire(ticket: StoredTicket): Record<string, unknown> {
  return { id: ticket.id, subject: ticket.subject, status: ticket.status, priority: 1, source: 2, requester_id: 6007738334, tags: [], created_at: formatTime(ticket.createdAtMs), updated_at: formatTime(ticket.updatedAtMs) };
}

function json(status: number, value: unknown, headers: Record<string, string>): ProviderResponse {
  return { status, headers, body: Buffer.from(JSON.stringify(value)) };
}
