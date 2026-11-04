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
      return newQueryBranch('found', { id: order.id, status: goString(order.status) });
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
    if (start === undefined || end === undefined || start > end || end - start > 31n * 86_400_000_000_000n || (input.page ?? 0) < 0) {
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
        transaction_details: goArray(page.transaction_details)?.map(decodeDetail) ?? null,
        page: goInteger(page.page),
        total_pages: goInteger(page.total_pages),
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
function parseRFC3339(value: string): bigint | undefined {
  const parts = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:[.,](\d+))?(Z|([+-])(\d{2}):(\d{2}))$/.exec(value);
  if (!parts) return undefined;
  const [, yearText, monthText, dayText, hourText, minuteText, secondText, fractionText, , offsetSign, offsetHourText, offsetMinuteText] = parts;
  const year = Number(yearText);
  const month = Number(monthText);
  const day = Number(dayText);
  const hour = Number(hourText);
  const minute = Number(minuteText);
  const second = Number(secondText);
  const offsetHour = Number(offsetHourText ?? 0);
  const offsetMinute = Number(offsetMinuteText ?? 0);
  const daysInMonth = [31, year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0) ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31][month - 1] ?? 0;
  if (month < 1 || month > 12 || day < 1 || day > daysInMonth || hour > 23 || minute > 59 || second > 59 || offsetHour > 24 || offsetMinute > 60) return undefined;
  const instant = new Date(0);
  instant.setUTCFullYear(year, month - 1, day);
  instant.setUTCHours(hour, minute, second, 0);
  const fractionNanoseconds = fractionText ? BigInt(fractionText.slice(0, 9).padEnd(9, '0')) : 0n;
  const offsetMilliseconds = (offsetHour * 60 + offsetMinute) * 60_000 * (offsetSign === '+' ? 1 : -1);
  return BigInt(instant.getTime() - offsetMilliseconds) * 1_000_000n + fractionNanoseconds;
}
function decodeDetail(value: unknown): NonNullable<TransactionPage['transaction_details']>[number] {
  const detail = object(value);
  const info = object(detail.transaction_info);
  const amount = object(info.transaction_amount);
  return { transaction_info: {
    transaction_id: goString(info.transaction_id), transaction_status: goString(info.transaction_status),
    transaction_amount: { currency_code: goString(amount.currency_code), value: goString(amount.value) },
  } };
}
function object(value: unknown): Record<string, unknown> {
  if (value === undefined || value === null) return {};
  if (typeof value !== 'object' || Array.isArray(value)) throw new TypeError('expected JSON object');
  return value as Record<string, unknown>;
}
function goArray(value: unknown): unknown[] | null {
  if (value === undefined || value === null) return null;
  if (!Array.isArray(value)) throw new TypeError('expected JSON array');
  return value;
}
function goString(value: unknown): string {
  if (value === undefined || value === null) return '';
  if (typeof value !== 'string') throw new TypeError('expected JSON string');
  return value;
}
function goInteger(value: unknown): number {
  if (value === undefined || value === null) return 0;
  if (typeof value !== 'number' || !Number.isSafeInteger(value)) throw new TypeError('expected JSON integer');
  return value;
}
