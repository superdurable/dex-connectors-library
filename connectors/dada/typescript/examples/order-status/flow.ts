// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newQueryOrderStatusStep, type Connection, type QueryOrderStatusInput, type OrderStatus } from '@superdurable/connector-dada';

/** Static Dex Web connection used by the example. */ export const ConnectionName = 'dada';
/** Persisted delivery status. */ export const orderStatusAttribute = new Attribute<OrderStatus>('dada-order-status', jsonCodec<OrderStatus>());

/** Read the delivery status for a third-party order. */
export class DadaOrderStatusFlow implements Flow<QueryOrderStatusInput> {
  private readonly complete = new CompleteOrderStatus();
  private readonly read;
  /** Bind one static Dada connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Dada connection name differs');
    this.read = newQueryOrderStatusStep<QueryOrderStatusInput>({ stepType: 'CallQueryOrderStatus',
      annotations: { groupId: 'provider', groupLabel: 'Dada', explanation: 'Query the current delivery status by order ID.' },
      connection, connectionName: ConnectionName, mapToOperationInput: input => input,
      found: new StepRef('CompleteDadaOrderStatus'),
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'DadaOrderStatusTypeScript'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<QueryOrderStatusInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Register the persisted status. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [orderStatusAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the delivery status and complete the Flow."
class CompleteOrderStatus implements Step<QueryResult<OrderStatus>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteDadaOrderStatus'; }
  /** Persist the provider result without credentials. */
  execute(context: Context, result: QueryResult<OrderStatus>): StepDecision { orderStatusAttribute.set(context, result.value); return gracefulComplete(result.value); }
}
