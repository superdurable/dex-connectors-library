// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, newQueryRetry, type Call, type Failure, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import type { ShopifyClient, ShopifyExchangeFailure } from './client.js';
import { GetOrderDefinition, ListOrdersDefinition } from './generated/definitions.js';
import type { GetOrderInput, ListOrdersInput, Order, OrderPage } from './generated/models.js';

const emptyOrder: Order = { id: 0, name: '', financial_status: '', total_price: '', currency: '' };
const emptyPage: OrderPage = { orders: [] };

/** Read one Shopify order. */
export class GetOrderOperation implements Query<GetOrderInput, Order> {
  /** Bind the validated client. */ constructor(private readonly client: ShopifyClient) {}
  /** Return the manifest operation definition. */ definition() { return GetOrderDefinition; }
  /** Validate and read one order. */
  async invoke(call: Call, input: GetOrderInput): Promise<QueryAttempt<Order>> {
    if (!Number.isSafeInteger(input.orderId) || input.orderId <= 0) return failed('defect', 'getOrder', 'VALIDATION', 'orderId must be positive', emptyOrder);
    const result = await this.client.read(call, 'getOrder', `/orders/${input.orderId}.json`);
    if ('outcome' in result) return classify(result.outcome, emptyOrder);
    try {
      const body = parseObject(result.body);
      const order = decodeOrder(body.order);
      if (order.id <= 0) throw new TypeError('missing order');
      return newQueryBranch('found', order);
    } catch { return failed('invalidResponse', 'getOrder', 'PROTOCOL', 'provider returned invalid JSON', emptyOrder); }
  }
}

/** Read one bounded Shopify order page. */
export class ListOrdersOperation implements Query<ListOrdersInput, OrderPage> {
  /** Bind the validated client. */ constructor(private readonly client: ShopifyClient) {}
  /** Return the manifest operation definition. */ definition() { return ListOrdersDefinition; }
  /** Validate and read one page. */
  async invoke(call: Call, input: ListOrdersInput): Promise<QueryAttempt<OrderPage>> {
    const limit = input.limit ?? 0;
    if (!Number.isSafeInteger(limit) || limit < 0 || limit > 250) return failed('defect', 'listOrders', 'VALIDATION', 'limit must be 1 through 250, or zero for provider default', emptyPage);
    const query = new URLSearchParams({ status: 'any' });
    if (limit > 0) query.set('limit', String(limit));
    const result = await this.client.read(call, 'listOrders', '/orders.json', query);
    if ('outcome' in result) return classify(result.outcome, emptyPage);
    try {
      const body = parseObject(result.body);
      if (!Array.isArray(body.orders)) throw new TypeError('missing orders');
      return newQueryBranch('listed', { orders: body.orders.map(decodeOrder) });
    } catch { return failed('invalidResponse', 'listOrders', 'PROTOCOL', 'provider returned invalid JSON', emptyPage); }
  }
}

function parseObject(body: Uint8Array): Record<string, unknown> {
  const value: unknown = JSON.parse(Buffer.from(body).toString('utf8'));
  if (value === null || Array.isArray(value) || typeof value !== 'object') throw new TypeError('object expected');
  return value as Record<string, unknown>;
}

function decodeOrder(value: unknown): Order {
  if (value === null || Array.isArray(value) || typeof value !== 'object') throw new TypeError('order expected');
  const order = value as Record<string, unknown>;
  const numberField = order.id ?? 0;
  if (typeof numberField !== 'number' || !Number.isSafeInteger(numberField)) throw new TypeError('id must be an integer');
  const text = (key: string): string => {
    const field = order[key] ?? '';
    if (typeof field !== 'string') throw new TypeError('order text field expected');
    return field;
  };
  return { id: numberField, name: text('name'), financial_status: text('financial_status'), total_price: text('total_price'), currency: text('currency') };
}

function classify<Output>(outcome: ShopifyExchangeFailure, empty: Output): QueryAttempt<Output> {
  if (outcome.branch === 'retry') return newQueryRetry(outcome.failure, outcome.retryAfterMs);
  return newQueryBranch(outcome.branch, empty, {}, outcome.failure);
}

function failed<Output>(branch: string, operation: string, kind: Failure['kind'], message: string, empty: Output): QueryAttempt<Output> {
  return newQueryBranch(branch, empty, {}, { kind, provider: 'shopify', operation, message });
}
