// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';

/** The fake's user email. */
export const fakeEmail = 'agent@example.com';
// Split so secret scanners do not mistake the fake's key for a real credential.
/** The fake's API key. */
export const fakeAPIKey = 'gorgiasFake' + 'Key0123456789';

interface StoredEvent { id: number; eventType: string; objectType: string; objectId: number; userId: number; createdAtMs: number }
interface StoredTicket { id: number; subject: string; status: string; tags: string[] }
interface InjectedFailure { status: number; retryAfter: string; body: string }

const created = '2026-10-20T09:00:00.000000';
// Gorgias documents created_datetime at microsecond precision without an offset.
const formatEventTime = (milliseconds: number): string => `${new Date(milliseconds).toISOString().slice(0, 23)}000`;

/**
 * A stateful Gorgias event log and ticket store, served through GorgiasClientOptions.send as the Go connector's
 * gorgiaseventfake serves GET /api/events and GET /api/tickets/{id}. It stamps created_datetime from the real
 * clock unless a test passes an explicit time.
 */
export class GorgiasEventsFake {
  /** The base URL to pass as apiBaseURL. */ readonly baseURL = 'https://acme.gorgias.com/api';
  /** The ID before the next recorded event's ID. */ nextEventId = 9000;
  /** The X-Gorgias-Account-Api-Call-Limit value of every response. */ apiCallLimit = '1/40';
  readonly #authorization = `Basic ${Buffer.from(`${fakeEmail}:${fakeAPIKey}`).toString('base64')}`;
  readonly #events: StoredEvent[] = [];
  readonly #tickets = new Map<number, StoredTicket>();
  readonly #failures: InjectedFailure[] = [];
  #eventRequestCount = 0;

  /** Serve one request; pass as GorgiasClientOptions.send. */
  readonly send = async (request: ProviderRequest): Promise<ProviderResponse> => this.#serve(request);

  /** Store the ticket GET /api/tickets/{id} returns, without recording an event. */
  setTicket(ticketId: number, subject: string, status: string, ...tags: string[]): void { this.#tickets.set(ticketId, { id: ticketId, subject, status, tags }); }

  /** Append an event about ticketId created now by userId (0 for a rule) and return its ID. */
  recordTicketEvent(eventType: string, ticketId: number, userId = 0): number { return this.recordEventAt(eventType, 'Ticket', ticketId, userId, Date.now()); }

  /** Append an event with an explicit object and created_datetime in milliseconds and return its ID. */
  recordEventAt(eventType: string, objectType: string, objectId: number, userId: number, createdAtMs: number): number {
    this.nextEventId++;
    this.#events.push({ id: this.nextEventId, eventType, objectType, objectId, userId, createdAtMs });
    return this.nextEventId;
  }

  /** Answer the next GET /api/events with status, an optional Retry-After value, and body. */
  failNextEvents(status: number, retryAfter: string, body: string): void { this.#failures.push({ status, retryAfter, body }); }

  /** Return how many GET /api/events requests arrived. */
  eventRequestCount(): number { return this.#eventRequestCount; }

  #serve(request: ProviderRequest): ProviderResponse {
    const headers: Record<string, string> = { date: new Date().toUTCString(), 'x-gorgias-account-api-call-limit': this.apiCallLimit };
    if (request.headers?.Authorization !== this.#authorization) return json(401, { error: { msg: 'invalid_credentials' } }, headers);
    if (request.method !== 'GET') return json(405, { error: { msg: 'method_not_allowed' } }, headers);
    const url = new URL(request.url);
    const path = url.pathname.replace(/^\/api/, '');
    if (path === '/events') { this.#eventRequestCount++; return this.#listEvents(url.searchParams, headers); }
    const ticketText = path.replace(/^\/tickets\//, '');
    const ticket = /^\d+$/.test(ticketText) ? this.#tickets.get(Number(ticketText)) : undefined;
    if (!ticket) return json(404, { error: { msg: 'not_found' } }, headers);
    return json(200, ticketWire(ticket), headers);
  }

  #listEvents(query: URLSearchParams, headers: Record<string, string>): ProviderResponse {
    const failure = this.#failures.shift();
    if (failure) {
      if (failure.retryAfter) headers['retry-after'] = failure.retryAfter;
      return { status: failure.status, headers: { ...headers, 'content-type': 'application/json' }, body: Buffer.from(failure.body) };
    }
    const types = query.getAll('types');
    const since = Date.parse(query.get('created_datetime[gte]') ?? '');
    const limit = Number(query.get('limit'));
    if (!Number.isFinite(since) || !Number.isInteger(limit) || limit < 1 || limit > 100 || query.get('order_by') !== 'created_datetime:asc' || types.length === 0) {
      return json(400, { error: { msg: 'invalid_query', data: { created_datetime: ['bad'] } } }, headers);
    }
    const matched = this.#events.filter(event => event.createdAtMs >= since && types.includes(event.eventType)).sort((left, right) => left.createdAtMs - right.createdAtMs);
    return json(200, matched.slice(0, limit).map(event => ({
      id: event.id, type: event.eventType, object_type: event.objectType, object_id: event.objectId, user_id: event.userId > 0 ? event.userId : null,
      created_datetime: formatEventTime(event.createdAtMs), data: null, context: 'dd4ff312-69df-494a-be96-1a58b3d8b8e0', uri: `/api/events/${event.id}/`,
    })), headers);
  }
}

function ticketWire(ticket: StoredTicket): Record<string, unknown> {
  return {
    id: ticket.id, subject: ticket.subject, status: ticket.status, priority: 'normal', channel: 'email',
    customer: { id: 501, email: 'ada@example.com', name: 'Ada Lovelace' }, tags: ticket.tags.map(name => ({ name })),
    created_datetime: created, updated_datetime: created,
    messages: [{
      id: ticket.id * 10 + 1, ticket_id: ticket.id, channel: 'email', via: 'email', public: true, from_agent: false, sender: { id: 501 },
      subject: ticket.subject, body_text: `Customer message for ${ticket.subject}`, created_datetime: created,
    }],
  };
}

function json(status: number, value: unknown, headers: Record<string, string>): ProviderResponse {
  return { status, headers: { ...headers, 'content-type': 'application/json' }, body: Buffer.from(JSON.stringify(value)) };
}
