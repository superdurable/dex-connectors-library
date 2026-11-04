// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetOrderStep, type Connection, type GetOrderInput, type Order } from '@superdurable/connector-shopify';

/** Static Dex Web connection name for the Worker and starter. */
export const ConnectionName = 'shopify-orders';

/** Nonsecret order lookup retained for inspection. */
export const orderRequest = new Attribute<GetOrderInput>('shopify-order-request', jsonCodec<GetOrderInput>());
/** Safe order summary retained for inspection. */
export const foundOrder = new Attribute<Order>('shopify-found-order', jsonCodec<Order>());

/** Read one Shopify order through a named connection. */
export class GetOrderFlow implements Flow<GetOrderInput> {
  private readonly prepare = new PrepareOrderRead();
  private readonly complete = new CompleteOrderRead();

  /** Bind the named Shopify connection. */
  constructor(private readonly connection: Connection) {
    if (!connection) throw new TypeError('Shopify connection is required');
  }

  /** Stable Flow type. */ getFlowType(): string { return 'ShopifyGetOrderFlow'; }
  /** Register preparation, provider read, and completion Steps. */
  getSteps(): StepList<GetOrderInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newGetOrderStep({
        stepType: 'ReadShopifyOrder',
        annotations: { groupId: 'shopify-order', groupLabel: 'Shopify Order', explanation: 'Read the requested Shopify order.' },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: input => input,
        found: this.complete,
      }),
      this.complete,
    );
  }
  /** Persist only nonsecret request and result data. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [orderRequest, foundOrder] }; }
}

/** Validate the order ID before the provider Step. */
// dex:group group-id:shopify-order group-label:"Shopify Order"
// dex:explanation text:"Validate the requested order ID before reading Shopify."
export class PrepareOrderRead implements Step<GetOrderInput> {
  /** Stable Step type. */ getStepType(): string { return 'PrepareShopifyOrderRead'; }
  /** Start the provider read with nonsecret input. */
  execute(context: Context, input: GetOrderInput): StepDecision {
    if (!Number.isSafeInteger(input.orderId) || input.orderId <= 0) return forceFail('Shopify orderId must be positive');
    orderRequest.set(context, input);
    return goTo(new StepRef('ReadShopifyOrder'), input);
  }
}

/** Persist the safe order summary and complete. */
// dex:group group-id:shopify-order group-label:"Shopify Order"
// dex:explanation text:"Persist the returned order summary and complete the Flow."
export class CompleteOrderRead implements Step<QueryResult<Order>> {
  /** Stable Step type. */ getStepType(): string { return 'CompleteShopifyOrderRead'; }
  /** Complete with the provider result. */
  execute(context: Context, result: QueryResult<Order>): StepDecision {
    foundOrder.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
