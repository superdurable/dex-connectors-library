// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StepList, gracefulComplete, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListOrdersStep, type Connection, type ListOrdersInput, type OrderPage } from '@superdurable/connector-woocommerce';

/** Static Dex Web connection name. */
export const ConnectionName = 'woocommerce';

/** Read one page of WooCommerce orders through Dex. */
export class ResourceReadFlow implements Flow<ListOrdersInput> {
  private readonly complete = new CompleteResourceRead();
  /** Bind the connection at Worker registration. */
  constructor(private readonly connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('WooCommerce connection name differs');
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'WooCommerceTypeScriptResourceRead'; }
  /** Route the happy branch to completion. */
  getSteps(): StepList<ListOrdersInput> {
    return StepList.startStep(newListOrdersStep<ListOrdersInput>({
      stepType: 'ReadWooCommerceOrderPage',
      annotations: { groupId: 'woocommerce', groupLabel: 'WooCommerce', explanation: 'Read one page of store orders.' },
      connection: this.connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, listed: this.complete,
    })).otherSteps(this.complete);
  }
  /** No Attributes or Streams are needed. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [] }; }
}

/** Complete with the provider order page. */
// dex:group group-id:woocommerce group-label:"WooCommerce"
// dex:explanation text:"Return the bounded order page from the provider read."
export class CompleteResourceRead implements Step<QueryResult<OrderPage>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteWooCommerceResourceRead'; }
  /** Return the query value. */ execute(_context: unknown, result: QueryResult<OrderPage>): StepDecision { return gracefulComplete(result.value); }
}
