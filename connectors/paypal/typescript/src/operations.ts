// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, newQueryRetry, type Call, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import type { PayPalClient, PayPalFailure } from './client.js';
import { GetOrderDefinition, ListTransactionsDefinition } from './generated/definitions.js';
import type { GetOrderInput, ListTransactionsInput, Order, TransactionPage } from './generated/models.js';

const zeroOrder: Order = { id: '', status: '' };
const zeroPage: TransactionPage = { transaction_details: [], page: 0, total_pages: 0 };

/** Read one PayPal checkout order. */
export class GetOrderOperation implements Query<GetOrderInput, Order> {
  /** Bind a PayPal client. */ constructor(private readonly client: PayPalClient) {}
  /** Return the manifest definition. */ definition() { return GetOrderDefinition; }
  /** Validate and read one order. */
  async invoke(call: Call, input: GetOrderInput): Promise<QueryAttempt<Order>> {
    if (!validPathSegment(input.orderId)) return outcome(this.client.failure('getOrder', 'defect', 'VALIDATION', 'orderId must be a path segment'), zeroOrder);
    const path = `/v2/checkout/orders/${goPathEscape(input.orderId)}`;
    const result = await this.client.read(call, 'getOrder', path);
    if ('outcome' in result) return outcome(result.outcome, zeroOrder);
    try {
      const value: unknown = JSON.parse(Buffer.from(result.response.body).toString('utf8'));
      if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('invalid order');
      const order = value as Record<string, unknown>;
      if (typeof order.id !== 'string' || !order.id) throw new Error('missing order');
      return newQueryBranch('found', { id: order.id, status: typeof order.status === 'string' ? order.status : '' });
    } catch { return outcome(this.client.failure('getOrder', 'invalidResponse', 'PROTOCOL', 'provider returned invalid JSON'), zeroOrder); }
  }
}

/** List one PayPal transaction page. */
export class ListTransactionsOperation implements Query<ListTransactionsInput, TransactionPage> {
  /** Bind a PayPal client. */ constructor(private readonly client: PayPalClient) {}
  /** Return the manifest definition. */ definition() { return ListTransactionsDefinition; }
  /** Validate a date window and read one page. */
  async invoke(call: Call, input: ListTransactionsInput): Promise<QueryAttempt<TransactionPage>> {
    const start = parseRFC3339(input.startDate);
    const end = parseRFC3339(input.endDate);
    if (start === undefined || end === undefined || start > end || end - start > 31 * 86_400_000 || (input.page ?? 0) < 0) {
      return outcome(this.client.failure('listTransactions', 'defect', 'VALIDATION', 'startDate and endDate must be RFC 3339, ordered within 31 days, and page nonnegative'), zeroPage);
    }
    const query = new URLSearchParams({ start_date: input.startDate, end_date: input.endDate, fields: 'transaction_info' });
    if ((input.page ?? 0) > 0) query.set('page', String(input.page));
    query.sort();
    const result = await this.client.read(call, 'listTransactions', `/v1/reporting/transactions?${query}`);
    if ('outcome' in result) return outcome(result.outcome, zeroPage);
    try {
      const value: unknown = JSON.parse(Buffer.from(result.response.body).toString('utf8'));
      if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('invalid page');
      const page = value as Record<string, unknown>;
      return newQueryBranch('listed', {
        transaction_details: Array.isArray(page.transaction_details) ? page.transaction_details.map(decodeDetail) : [],
        page: typeof page.page === 'number' ? page.page : 0,
        total_pages: typeof page.total_pages === 'number' ? page.total_pages : 0,
      });
    } catch { return outcome(this.client.failure('listTransactions', 'invalidResponse', 'PROTOCOL', 'provider returned invalid JSON'), zeroPage); }
  }
}

function outcome<T>(result: PayPalFailure, zero: T): QueryAttempt<T> {
  return result.branch === 'retry' ? newQueryRetry(result.failure, result.retryAfterMs)
    : newQueryBranch(result.branch, zero, {}, result.failure);
}
function validPathSegment(value: string): boolean {
  return !!value && value !== '.' && value !== '..' && !value.includes('/') && !value.includes('?') && !value.includes('#') && !Array.from(value).some(character => character.codePointAt(0)! < 32);
}
function goPathEscape(value: string): string {
  return encodeURIComponent(value).replace(/[!'()*]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`).replace(/%(?:2B|3D|3A|40|24|26)/g, escape => String.fromCharCode(parseInt(escape.slice(1), 16)));
}
function parseRFC3339(value: string): number | undefined {
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/.test(value)) return undefined;
  const timestamp = Date.parse(value);
  return Number.isFinite(timestamp) ? timestamp : undefined;
}
function decodeDetail(value: unknown): NonNullable<TransactionPage['transaction_details']>[number] {
  const detail = object(value);
  const info = object(detail.transaction_info);
  const amount = object(info.transaction_amount);
  return { transaction_info: {
    transaction_id: stringValue(info.transaction_id), transaction_status: stringValue(info.transaction_status),
    transaction_amount: { currency_code: stringValue(amount.currency_code), value: stringValue(amount.value) },
  } };
}
function object(value: unknown): Record<string, unknown> { return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {}; }
function stringValue(value: unknown): string { return typeof value === 'string' ? value : ''; }
