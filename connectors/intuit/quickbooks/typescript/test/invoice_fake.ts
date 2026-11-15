// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';

// QuickBooks writes MetaData times in company time; the fake company is at UTC-07:00.
const companyOffsetMs = -7 * 3_600_000;
const companyPathPattern = /^\/v3\/company\/([0-9]+)\/(query|invoice\/([0-9]+))$/;
const queryPattern = /^select \* from Invoice(?: where MetaData\.LastUpdatedTime >= '([^']+)')? ORDERBY MetaData\.LastUpdatedTime( DESC)? STARTPOSITION ([0-9]+) MAXRESULTS ([0-9]+)$/;

interface StoredInvoice { id: string; docNumber: string; customerId: string; amount: string; balance: string; syncToken: number; createdAtMs: number; updatedAtMs: number }

/** Format a time in company time as Go's Format(time.RFC3339) does, such as 2026-11-02T09:00:01-07:00. */
const formatCompanyTime = (milliseconds: number): string => `${new Date(milliseconds + companyOffsetMs).toISOString().slice(0, 19)}-07:00`;

/**
 * An in-process stand-in for the QuickBooks Online invoice query and read endpoints, served through
 * QuickBooksClientOptions.send. It mirrors the Go connector's internal/quickbooksinvoicefake: a company clock, a new
 * SyncToken and LastUpdatedTime on every write, the watermark query answered in LastUpdatedTime order, and throttling.
 */
export class QuickBooksInvoiceFake {
  readonly #invoices = new Map<string, StoredInvoice>();
  readonly #queries: string[] = [];
  #clockMs = Date.UTC(2026, 10, 2, 16, 0, 0);
  #nextID = 130;
  #throttles = 0;
  #retryAfter = '';
  #rejectsWatermarks = false;
  #isClockHeld = false;

  /** Start an empty company whose clock reads 2026-11-02T09:00:00-07:00. */
  constructor(readonly realmId: string) {}

  /** Serve one request; pass as QuickBooksClientOptions.send. */
  readonly send = async (request: ProviderRequest): Promise<ProviderResponse> => this.#serve(request);

  /** Add an invoice at SyncToken 0, one company second after the previous write, and return its Id. */
  create(docNumber: string, customerId: string, amount: string): string { return this.#add(docNumber, customerId, amount, this.#tick()); }

  /** Add an invoice stamped ageMs before the company clock, as a write QuickBooks made visible late. */
  createBackdated(docNumber: string, customerId: string, amount: string, ageMs: number): string { return this.#add(docNumber, customerId, amount, this.#clockMs - ageMs); }

  /** Zero the balance as an applied payment does, bumping the SyncToken. */
  pay(id: string): void {
    const changed = this.#invoices.get(id)!;
    changed.balance = '0'; changed.syncToken += 1; changed.updatedAtMs = this.#tick();
  }

  /** Make later writes share the next company second until releaseClock, as a burst does. */
  holdClock(): void { this.#clockMs += 1000; this.#isClockHeld = true; }

  /** Let every later write advance the company clock by one second again. */
  releaseClock(): void { this.#isClockHeld = false; }

  /** Answer the next count queries with HTTP 429; a blank retryAfter sends no Retry-After header. */
  throttle(count: number, retryAfter: string): void { this.#throttles = count; this.#retryAfter = retryAfter; }

  /** Answer every later watermark query with HTTP 400 ValidationFault; the baseline query still succeeds. */
  rejectWatermarkQueries(rejects: boolean): void { this.#rejectsWatermarks = rejects; }

  /** Return every query statement received, in order. */
  queries(): string[] { return [...this.#queries]; }

  #serve(request: ProviderRequest): ProviderResponse {
    const url = new URL(request.url);
    const match = companyPathPattern.exec(url.pathname);
    if (!request.headers?.Authorization) return fault(401, 'AuthenticationFault', '3200');
    if (request.method !== 'GET' || !match) return fault(400, 'ValidationFault', '4000');
    if (match[1] !== this.realmId) return fault(403, 'AuthorizationFault', '3100');
    if (match[3]) {
      const found = this.#invoices.get(match[3]);
      if (!found) return fault(400, 'ValidationFault', '610');
      return body(200, `{"Invoice":${wire(found)},"time":"${formatCompanyTime(this.#clockMs)}"}`);
    }
    return this.#serveQuery(url.searchParams.get('query') ?? '');
  }

  #serveQuery(statement: string): ProviderResponse {
    this.#queries.push(statement);
    if (this.#throttles > 0) {
      this.#throttles -= 1;
      return fault(429, 'ThrottleExceeded', '003001', this.#retryAfter ? { 'retry-after': this.#retryAfter } : {});
    }
    const match = queryPattern.exec(statement);
    if (!match || (this.#rejectsWatermarks && match[1])) return fault(400, 'ValidationFault', '4000');
    const sinceMs = match[1] ? Date.parse(match[1]) : Number.NEGATIVE_INFINITY;
    if (Number.isNaN(sinceMs)) return fault(400, 'ValidationFault', '4000');
    const isDescending = !!match[2];
    // Ties keep Id order, as QuickBooks keeps equal timestamps in a stable order.
    const selected = [...this.#invoices.values()].filter(stored => stored.updatedAtMs >= sinceMs)
      .sort((left, right) => (left.updatedAtMs !== right.updatedAtMs ? (left.updatedAtMs - right.updatedAtMs) * (isDescending ? -1 : 1) : Number(left.id) - Number(right.id)));
    const start = Number(match[3]) - 1;
    const page = selected.slice(Math.max(start, 0), Math.max(start, 0) + Number(match[4]));
    const members = [`"startPosition":"${match[3]}"`, `"maxResults":${page.length}`];
    if (page.length) members.unshift(`"Invoice":[${page.map(wire).join(',')}]`);
    return body(200, `{"QueryResponse":{${members.join(',')}},"time":"${formatCompanyTime(Date.now())}"}`);
  }

  #add(docNumber: string, customerId: string, amount: string, stampedMs: number): string {
    this.#nextID += 1;
    const id = String(this.#nextID);
    this.#invoices.set(id, { id, docNumber, customerId, amount, balance: amount, syncToken: 0, createdAtMs: stampedMs, updatedAtMs: stampedMs });
    return id;
  }

  #tick(): number {
    if (!this.#isClockHeld) this.#clockMs += 1000;
    return this.#clockMs;
  }
}

// Encode the invoice as QuickBooks does, keeping exact decimal amounts as JSON numbers.
function wire(stored: StoredInvoice): string {
  const created = formatCompanyTime(stored.createdAtMs);
  return `{"Balance":${stored.balance},"CustomerRef":{"value":${JSON.stringify(stored.customerId)}},"DocNumber":${JSON.stringify(stored.docNumber)},"EmailStatus":"NotSet",`
    + `"Id":${JSON.stringify(stored.id)},"Line":[{"Amount":${stored.amount},"DetailType":"SalesItemLineDetail","Id":"1","SalesItemLineDetail":{"ItemRef":{"value":"1"},"Qty":1,"UnitPrice":${stored.amount}}},`
    + `{"Amount":${stored.amount},"DetailType":"SubTotalLineDetail","SubTotalLineDetail":{}}],`
    + `"MetaData":{"CreateTime":"${created}","LastUpdatedTime":"${formatCompanyTime(stored.updatedAtMs)}"},"SyncToken":"${stored.syncToken}","TotalAmt":${stored.amount},"TxnDate":"${created.slice(0, 10)}"}`;
}

function fault(status: number, type: string, code: string, headers: Record<string, string> = {}): ProviderResponse {
  return body(status, JSON.stringify({ Fault: { Error: [{ Message: 'fake QuickBooks fault', code }], type } }), headers);
}

function body(status: number, text: string, headers: Record<string, string> = {}): ProviderResponse {
  return { status, headers: { 'content-type': 'application/json', ...headers }, body: Buffer.from(text) };
}
