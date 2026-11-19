// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetOrderStep, type Connection, type GetOrderInput, type Order } from '@superdurable/connector-paypal';

/** Static Dex Web connection name. */
export const ConnectionName = 'paypal';
/** Safe order ID retained in the Flow. */
export const orderRequest = new Attribute<GetOrderInput>('paypal-order-request', jsonCodec<GetOrderInput>());
/** Safe order summary retained in the Flow. */
export const orderResult = new Attribute<Order>('paypal-order-result', jsonCodec<Order>());

/** Read one PayPal checkout order. */
export class ResourceReadFlow implements Flow<GetOrderInput> {
  private readonly prepare = new PrepareOrderRead();
  private readonly complete = new CompleteOrderRead();
  /** Bind a named PayPal connection. */
  constructor(private readonly connection: Connection) { if (!connection) throw new TypeError('PayPal connection is required'); }
  /** Stable Dex Flow type. */ getFlowType(): string { return 'PayPalResourceReadTypeScript'; }
  /** Register validation, provider, and completion Steps. */
  getSteps(): StepList<GetOrderInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newGetOrderStep({
        stepType: 'ReadPayPalOrder',
        annotations: { groupId: 'paypal-order', groupLabel: 'PayPal Order', explanation: 'Read one checkout order from PayPal.' },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: input => input,
        found: this.complete,
      }),
      this.complete,
    );
  }
  /** Persist every Flow Attribute. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [orderRequest, orderResult] }; }
}

/** Validate and retain the order request. */
// dex:group group-id:paypal-order group-label:"PayPal Order"
// dex:explanation text:"Validate the order ID before reading it from PayPal."
export class PrepareOrderRead implements Step<GetOrderInput> {
  /** Stable Dex Step type. */ getStepType(): string { return 'PreparePayPalOrderRead'; }
  /** Start the provider Step with the safe order ID. */
  execute(context: Context, input: GetOrderInput): StepDecision {
    if (!input.orderId) return forceFail('PayPal orderId is required');
    orderRequest.set(context, input);
    return goTo(new StepRef('ReadPayPalOrder'), input);
  }
}

/** Persist the safe order summary and complete. */
// dex:group group-id:paypal-order group-label:"PayPal Order"
// dex:explanation text:"Persist the PayPal order summary and complete the Flow."
export class CompleteOrderRead implements Step<QueryResult<Order>> {
  /** Stable Dex Step type. */ getStepType(): string { return 'CompletePayPalOrderRead'; }
  /** Complete with the provider's order summary. */
  execute(context: Context, result: QueryResult<Order>): StepDecision {
    orderResult.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
