// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { BodyTooLargeError, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';

/** The only email the fake accepts, as {email}/token over HTTP Basic. */ export const fakeEmail = 'admin@acme.example.com';
/** The only API token the fake accepts. */ export const fakeAPIToken = 'fake-zendesk-api-token';
/** The API base URL to pass as apiBaseURL. */ export const fakeAPIBaseURL = 'https://acme.zendesk.com/api/v2';

interface StoredTicket { id: number; subject: string; status: string; createdAtSeconds: number; updatedAtSeconds: number; sequence: number }
/** One request the fake answered. */
export interface RecordedRequest { /** The path below the API base URL. */ path: string; /** The query parameters. */ query: URLSearchParams; /** The answered status. */ status: number }

const formatTime = (seconds: number): string => `${new Date(seconds * 1000).toISOString().slice(0, 19)}Z`;

/**
 * A stateful Zendesk account served through ZendeskClientOptions.send, mirroring the Go connector's
 * internal/zendeskexportfake: the cursor-based incremental ticket export, ticket reads, and comment reads. Every
 * create, update, or delete is one change with the next sequence number; the export lists each ticket once, at
 * its latest change. Changes are stamped one minute before now, as Zendesk serves a change only once it is a
 * minute old, and at least two seconds apart; tieChanges stamps a burst in one second instead.
 */
export class ZendeskExportFake {
  readonly #authorization = `Basic ${Buffer.from(`${fakeEmail}/token:${fakeAPIToken}`).toString('base64')}`;
  readonly #tickets = new Map<number, StoredTicket>();
  readonly #requests: RecordedRequest[] = [];
  #sequence = 0;
  #clockSeconds: number | undefined;
  #cursorEpoch = 0;
  #rateLimited = 0;
  #retryAfter = '';
  #isAgentOnly = false;
  #maximumPerPage = 1000;
  #isTyingChanges = false;

  /** Serve one request; pass as ZendeskClientOptions.send. */
  readonly send = async (request: ProviderRequest): Promise<ProviderResponse> => {
    const response = this.#serve(request);
    // The SDK transport reads at most maxResponseBytes, as the Go client's bounded reader does.
    if (response.body.length > request.maxResponseBytes) throw new BodyTooLargeError(response.status, response.headers);
    return response;
  };

  /** Add a new ticket. */
  create(ticketId: number, subject: string): void {
    const now = this.#advance();
    this.#tickets.set(ticketId, { id: ticketId, subject, status: 'new', createdAtSeconds: now, updatedAtSeconds: now, sequence: this.#sequence });
  }

  /** Change an existing ticket's status. */
  update(ticketId: number, status: string): void {
    const now = this.#advance();
    const ticket = this.#tickets.get(ticketId)!;
    ticket.status = status; ticket.updatedAtSeconds = now; ticket.sequence = this.#sequence;
  }

  /** Delete a ticket; the export reports it with status deleted, and reads answer 404. */ delete(ticketId: number): void { this.update(ticketId, 'deleted'); }
  /** Make every cursor issued so far answer HTTP 422. */ invalidateCursors(): void { this.#cursorEpoch++; }
  /** Answer the next count export requests with 429 and the given Retry-After value. */ rateLimitNextRequests(count: number, retryAfter: string): void { this.#rateLimited = count; this.#retryAfter = retryAfter; }
  /** Answer the export with 403, as Zendesk does for a user who is not an administrator. */ rejectAsAgent(isAgentOnly: boolean): void { this.#isAgentOnly = isAgentOnly; }
  /** Cap per_page, as a smaller provider page would. */ limitPerPage(maximumPerPage: number): void { this.#maximumPerPage = maximumPerPage; }
  /** Stamp every following change in the previous change's second while isTying is true. */ tieChanges(isTying: boolean): void { this.#isTyingChanges = isTying; }
  /** The after_cursor a caught-up export reports. */ cursor(): string { return this.#epochCursor(this.#sequence); }
  /** The recorded requests in arrival order. */ requests(): RecordedRequest[] { return [...this.#requests]; }

  #advance(): number {
    this.#sequence++;
    const servedAt = Math.floor(Date.now() / 1000) - 60;
    if (this.#isTyingChanges && this.#clockSeconds !== undefined) return this.#clockSeconds;
    this.#clockSeconds = Math.max(servedAt, (this.#clockSeconds ?? 0) + 2);
    return this.#clockSeconds;
  }

  #epochCursor(sequence: number): string { return `c-${this.#cursorEpoch}-${sequence}`; }

  #serve(request: ProviderRequest): ProviderResponse {
    const url = new URL(request.url);
    const path = url.pathname.replace(/^\/api\/v2/, '');
    const authorization = request.headers?.Authorization ?? request.headers?.authorization;
    if (authorization !== this.#authorization) return this.#answer(path, url.searchParams, 401, { error: "Couldn't authenticate you" });
    if (path === '/incremental/tickets/cursor') return this.#serveExport(path, url.searchParams);
    const match = /^\/tickets\/(\d+)(\/comments)?$/.exec(path);
    const ticket = match ? this.#tickets.get(Number(match[1])) : undefined;
    if (!ticket || ticket.status === 'deleted') return this.#answer(path, url.searchParams, 404, { error: 'RecordNotFound' });
    if (!match![2]) return this.#answer(path, url.searchParams, 200, { ticket: ticketJSON(ticket), users: [{ id: 7701, name: 'Jane Smith', email: 'jane@acme.example.com', role: 'end-user' }] });
    return this.#answer(path, url.searchParams, 200, {
      comments: [{ id: ticket.id * 10, author_id: 7701, public: true, plain_body: `Opened: ${ticket.subject}`, created_at: formatTime(ticket.createdAtSeconds) }],
      meta: { has_more: false },
    });
  }

  #serveExport(path: string, query: URLSearchParams): ProviderResponse {
    if (this.#rateLimited > 0) {
      this.#rateLimited--;
      return this.#answer(path, query, 429, { error: 'APIRateLimitExceeded' }, this.#retryAfter ? { 'retry-after': this.#retryAfter } : {});
    }
    if (this.#isAgentOnly) return this.#answer(path, query, 403, { error: 'Forbidden' });
    let perPage = 1000;
    if (query.has('per_page')) {
      perPage = Number(query.get('per_page'));
      if (!Number.isInteger(perPage) || perPage < 1 || perPage > 1000) return this.#answer(path, query, 400, { error: 'InvalidPaginationParameter' });
    }
    perPage = Math.min(perPage, this.#maximumPerPage);
    const cursors = query.getAll('cursor');
    const isCursor = cursors.length === 1;
    let after: number;
    if (isCursor) {
      const match = /^c-(\d+)-(\d+)$/.exec(cursors[0]!);
      if (!match || Number(match[1]) !== this.#cursorEpoch) return this.#answer(path, query, 422, { error: 'InvalidPaginationParameter' });
      after = Number(match[2]);
    } else {
      const startTime = Number(query.get('start_time'));
      // Zendesk serves no data for the most recent minute.
      if (!query.has('start_time') || !Number.isInteger(startTime) || startTime > Math.floor(Date.now() / 1000) - 60) return this.#answer(path, query, 400, { error: 'InvalidPaginationParameter' });
      after = startTime;
    }
    const isExcludingDeleted = query.get('exclude_deleted') === 'true';
    let matches = [...this.#tickets.values()]
      .filter(ticket => (isCursor ? ticket.sequence > after : ticket.updatedAtSeconds >= after) && !(isExcludingDeleted && ticket.status === 'deleted'))
      .sort((left, right) => left.sequence - right.sequence);
    let nextSequence = this.#sequence;
    const isEnd = matches.length <= perPage;
    if (!isEnd) { matches = matches.slice(0, perPage); nextSequence = matches.at(-1)!.sequence; }
    return this.#answer(path, query, 200, { tickets: matches.map(ticketJSON), after_cursor: this.#epochCursor(nextSequence), before_cursor: null, end_of_stream: isEnd });
  }

  #answer(path: string, query: URLSearchParams, status: number, body: unknown, headers: Record<string, string> = {}): ProviderResponse {
    this.#requests.push({ path, query, status });
    return { status, headers: { 'content-type': 'application/json', ...headers }, body: Buffer.from(JSON.stringify(body)) };
  }
}

function ticketJSON(ticket: StoredTicket): Record<string, unknown> {
  return {
    id: ticket.id, subject: ticket.subject, description: `Opened: ${ticket.subject}`, status: ticket.status, requester_id: 7701,
    tags: ['dex-fake'], via: { channel: 'api' }, created_at: formatTime(ticket.createdAtSeconds), updated_at: formatTime(ticket.updatedAtSeconds),
    generated_timestamp: ticket.updatedAtSeconds,
  };
}
