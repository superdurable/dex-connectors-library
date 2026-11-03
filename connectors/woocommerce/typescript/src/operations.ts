// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, newQueryRetry, type Call, type Failure, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import type { WooCommerceClient, WooCommerceExchange } from './client.js';
import { GetOrderDefinition, ListOrdersDefinition } from './generated/definitions.js';
import type { GetOrderInput, ListOrdersInput, Order, OrderPage } from './generated/models.js';

const emptyOrder: Order = { id: 0, status: '', total: '', currency: '' };
const emptyPage: OrderPage = { orders: [] };

/** Read one page of WooCommerce orders. */
export class ListOrdersOperation implements Query<ListOrdersInput, OrderPage> {
  /** Bind an authenticated client. */ constructor(private readonly client: WooCommerceClient) {}
  /** Return the manifest definition. */ definition() { return ListOrdersDefinition; }
  /** Validate pagination and read the order page. */
  async invoke(call: Call, input: ListOrdersInput): Promise<QueryAttempt<OrderPage>> {
    const page = input.page ?? 0;
    const perPage = input.perPage ?? 0;
    if (!Number.isSafeInteger(page) || page < 0 || !Number.isSafeInteger(perPage) || perPage < 0 || perPage > 100) return failure('listOrders', 'defect', 'VALIDATION', emptyPage, 'page must be positive and perPage at most 100');
    const query = [page > 0 ? `page=${page}` : '', perPage > 0 ? `per_page=${perPage}` : ''].filter(Boolean).join('&');
    const exchange = await this.client.exchange(call, 'listOrders', '/orders', query);
    if (!('response' in exchange)) return classify(exchange, emptyPage);
    try {
      const document: unknown = JSON.parse(Buffer.from(exchange.response.body).toString('utf8'));
      if (!Array.isArray(document)) throw new TypeError('missing orders');
      return newQueryBranch('listed', { orders: document.map(decodeOrder) }, {});
    } catch { return failure('listOrders', 'invalidResponse', 'PROTOCOL', emptyPage, 'provider returned invalid JSON'); }
  }
}

/** Read one WooCommerce order. */
export class GetOrderOperation implements Query<GetOrderInput, Order> {
  /** Bind an authenticated client. */ constructor(private readonly client: WooCommerceClient) {}
  /** Return the manifest definition. */ definition() { return GetOrderDefinition; }
  /** Validate the order ID and read the order. */
  async invoke(call: Call, input: GetOrderInput): Promise<QueryAttempt<Order>> {
    if (!Number.isSafeInteger(input.orderId) || input.orderId <= 0) return failure('getOrder', 'defect', 'VALIDATION', emptyOrder, 'orderId must be positive');
    const exchange = await this.client.exchange(call, 'getOrder', `/orders/${input.orderId}`);
    if (!('response' in exchange)) return classify(exchange, emptyOrder);
    try {
      const order = decodeOrder(JSON.parse(Buffer.from(exchange.response.body).toString('utf8')) as unknown);
      if (order.id <= 0) throw new TypeError('missing order');
      return newQueryBranch('found', order, {});
    } catch { return failure('getOrder', 'invalidResponse', 'PROTOCOL', emptyOrder, 'provider returned invalid JSON'); }
  }
}

function decodeOrder(value: unknown): Order {
  if (value === null) return { ...emptyOrder };
  if (typeof value !== 'object' || Array.isArray(value)) throw new TypeError('invalid order');
  const fields = value as Record<string, unknown>;
  return { id: goInteger(fields.id), status: goString(fields.status), total: goString(fields.total), currency: goString(fields.currency) };
}
function goInteger(value: unknown): number {
  if (value === undefined || value === null) return 0;
  if (typeof value !== 'number' || !Number.isSafeInteger(value)) throw new TypeError('invalid integer');
  return value;
}
function goString(value: unknown): string {
  if (value === undefined || value === null) return '';
  if (typeof value !== 'string') throw new TypeError('invalid string');
  return value;
}
function classify<Output>(exchange: Exclude<WooCommerceExchange, { response: unknown }>, empty: Output): QueryAttempt<Output> {
  if ('retry' in exchange) return newQueryRetry(exchange.failure, exchange.retryAfterMs);
  return newQueryBranch(exchange.branch, empty, {}, exchange.failure);
}
function failure<Output>(operation: string, branch: string, kind: Failure['kind'], empty: Output, message: string): QueryAttempt<Output> {
  return newQueryBranch(branch, empty, {}, { kind, provider: 'woocommerce', operation, message });
}
