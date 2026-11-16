// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';

const invoicesPath = '/api.xro/2.0/Invoices';
const contactId = '025867f1-d741-4d6b-b1af-9ac774b59ba7';
/** The fake organisation's clock at start, 2026-10-01T09:00:00Z, in Unix milliseconds. */
export const BaseTimeMs = Date.UTC(2026, 9, 1, 9, 0, 0);
// Spaces changes by more than a second, so most changes fall in distinct seconds.
const defaultChangeStepMs = 1250;

/** One recorded request. */
export interface FakeRequest { path: string; query: URLSearchParams; modifiedSince: string; status: number }

interface StoredInvoice { id: string; number: string; kind: string; status: string; total: string; updatedAtMs: number }

// A JSON number written as its exact text, as Go's json.Number is.
class RawNumber { constructor(readonly text: string) {} }

// Encode as Go's json.Marshal does for these maps: sorted keys, raw numbers.
function encode(value: unknown): string {
  if (value instanceof RawNumber) return value.text;
  if (Array.isArray(value)) return `[${value.map(encode).join(',')}]`;
  if (value && typeof value === 'object') return `{${Object.keys(value).sort().map(key => `${JSON.stringify(key)}:${encode((value as Record<string, unknown>)[key])}`).join(',')}}`;
  return JSON.stringify(value);
}

const dotNetDate = (milliseconds: number): string => `/Date(${milliseconds}+0000)/`;
const atLeastOne = (value: string | null): number => { const parsed = /^-?[0-9]+$/.test(value ?? '') ? Number(value) : 1; return Math.max(parsed, 1); };

function invoiceJSON(stored: StoredInvoice): Record<string, unknown> {
  const [amountDue, amountPaid] = stored.status === 'PAID' ? ['0.00', stored.total] : [stored.total, '0.00'];
  return {
    InvoiceID: stored.id, InvoiceNumber: stored.number, Type: stored.kind, Status: stored.status,
    Contact: { ContactID: contactId, Name: 'City Agency' },
    DateString: '2026-10-01T00:00:00', CurrencyCode: 'USD', LineAmountTypes: 'NoTax',
    LineItems: [{ Description: 'Spring bouquet', Quantity: new RawNumber('1.0000'), UnitAmount: new RawNumber(stored.total), AccountCode: '200', LineAmount: new RawNumber(stored.total) }],
    SubTotal: new RawNumber(stored.total), Total: new RawNumber(stored.total), AmountDue: new RawNumber(amountDue), AmountPaid: new RawNumber(amountPaid),
    UpdatedDateUTC: dotNetDate(stored.updatedAtMs),
  };
}

/**
 * One Xero organisation's GET /Invoices list and GET /Invoices/{id} read, served in process through
 * XeroClientOptions.send. It mirrors the Go connector's internal/xeroinvoicefake: it honours If-Modified-Since to the
 * second, the Statuses filter, a Type where filter, order by UpdatedDateUTC, page and pageSize, and answers with
 * DateTimeUTC and a pagination object, as Xero's Accounting API does.
 */
export class XeroInvoiceFake {
  #now = BaseTimeMs;
  readonly #invoices = new Map<string, StoredInvoice>();
  #rateLimited = 0;
  #rateLimitKind = '';
  #retryAfter = '';
  #failures = 0;
  #failureStatus = 0;
  #maximumPerPage = 100;
  readonly #requests: FakeRequest[] = [];

  /** Serve one request; pass as XeroClientOptions.send. */
  readonly send = async (request: ProviderRequest): Promise<ProviderResponse> => this.serve(request);

  /**
   * Create or change an invoice stamped with the clock advanced by stepMs, and return its instant. invoiceType is
   * ACCREC or ACCPAY and total a decimal string such as 49.95; a zero step stamps the same instant as the last change.
   */
  upsert(invoiceId: string, invoiceNumber: string, invoiceType: string, status: string, total: string, stepMs = defaultChangeStepMs): number {
    this.#now += stepMs;
    const id = invoiceId.toLowerCase();
    this.#invoices.set(id, { id, number: invoiceNumber, kind: invoiceType, status, total, updatedAtMs: this.#now });
    return this.#now;
  }

  /** Change an existing invoice's status and stamp it as upsert does. */
  setStatus(invoiceId: string, status: string): number {
    const stored = this.#invoices.get(invoiceId.toLowerCase())!;
    return this.upsert(stored.id, stored.number, stored.kind, status, stored.total);
  }

  /** Move the organisation's clock without changing an invoice. */ advanceClock(milliseconds: number): void { this.#now += milliseconds; }
  /** Answer the next count list requests with 429, X-Rate-Limit-Problem, and Retry-After. */
  rateLimitNext(count: number, problem: string, retryAfter: string): void { [this.#rateLimited, this.#rateLimitKind, this.#retryAfter] = [count, problem, retryAfter]; }
  /** Answer the next count list requests with status, such as 500 or 403. */ failNext(count: number, status: number): void { [this.#failures, this.#failureStatus] = [count, status]; }
  /** Answer at most pageSize invoices per page. */ limitPageSize(pageSize: number): void { this.#maximumPerPage = pageSize; }
  /** Every recorded request. */ requests(): FakeRequest[] { return [...this.#requests]; }
  /** The recorded GET /Invoices list requests. */ listRequests(): FakeRequest[] { return this.#requests.filter(request => request.path === invoicesPath); }

  /** Answer one request and record it. */
  serve(request: ProviderRequest): ProviderResponse {
    const url = new URL(request.url);
    const recorded: FakeRequest = { path: url.pathname, query: url.searchParams, modifiedSince: request.headers?.['If-Modified-Since'] ?? '', status: 0 };
    const headers: Record<string, string> = { 'content-type': 'application/json; charset=utf-8' };
    const [status, text] = this.#answer(request, recorded, headers);
    this.#requests.push({ ...recorded, status });
    return { status, headers, body: Buffer.from(text) };
  }

  #answer(request: ProviderRequest, recorded: FakeRequest, headers: Record<string, string>): [number, string] {
    if (request.method !== 'GET' || !(request.headers?.Authorization ?? '').startsWith('Bearer ')) return [401, '{"Type":"AuthenticationUnsuccessful"}'];
    if (recorded.path === invoicesPath) {
      if (this.#rateLimited > 0) {
        this.#rateLimited--;
        headers['x-rate-limit-problem'] = this.#rateLimitKind;
        headers['retry-after'] = this.#retryAfter;
        return [429, '{"Type":"RateLimitExceeded"}'];
      }
      if (this.#failures > 0) { this.#failures--; return [this.#failureStatus, '{"Type":"InjectedFailure"}']; }
      return this.#listInvoices(recorded);
    }
    if (recorded.path.startsWith(`${invoicesPath}/`)) {
      const stored = this.#invoices.get(recorded.path.slice(invoicesPath.length + 1).toLowerCase());
      return stored ? [200, encode({ Invoices: [invoiceJSON(stored)] })] : [404, '{"Type":"NotFound"}'];
    }
    return [404, '{"Type":"NotFound"}'];
  }

  #listInvoices(recorded: FakeRequest): [number, string] {
    let since = Number.NEGATIVE_INFINITY;
    if (recorded.modifiedSince) {
      if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}$/.test(recorded.modifiedSince)) return [400, '{"Type":"ValidationException"}'];
      since = Date.parse(`${recorded.modifiedSince}Z`);
    }
    const statuses = new Set((recorded.query.get('Statuses') ?? '').split(',').filter(Boolean));
    let invoiceType = '';
    const where = recorded.query.get('where') ?? '';
    if (where) {
      if (!where.startsWith('Type=="') || !where.endsWith('"')) return [400, '{"Type":"ValidationException"}'];
      invoiceType = where.slice('Type=="'.length, -1);
    }
    // Xero compares If-Modified-Since to the second, inclusively.
    const matching = [...this.#invoices.values()].filter(stored => Math.floor(stored.updatedAtMs / 1000) * 1000 >= since
      && (statuses.size === 0 || statuses.has(stored.status)) && (!invoiceType || stored.kind === invoiceType));
    const isDescending = (recorded.query.get('order') ?? '').endsWith(' DESC');
    matching.sort((left, right) => left.updatedAtMs !== right.updatedAtMs
      ? ((left.updatedAtMs < right.updatedAtMs) !== isDescending ? -1 : 1)
      : (left.id < right.id ? -1 : left.id > right.id ? 1 : 0));
    const page = atLeastOne(recorded.query.get('page'));
    const pageSize = Math.min(atLeastOne(recorded.query.get('pageSize')), this.#maximumPerPage);
    const start = Math.min((page - 1) * pageSize, matching.length);
    const end = Math.min(start + pageSize, matching.length);
    return [200, encode({
      DateTimeUTC: dotNetDate(this.#now),
      pagination: { page, pageSize, pageCount: Math.ceil(matching.length / pageSize), itemCount: matching.length },
      Invoices: matching.slice(start, end).map(invoiceJSON),
    })];
  }
}
