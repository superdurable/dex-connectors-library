// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newGetTransactionStep, type Connection, type TransactionStatus, type GetTransactionInput,
} from '@superdurable/connector-paddle';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'paddle';
const transactionAttribute = new Attribute<TransactionStatus>('paddle-transaction', jsonCodec<TransactionStatus>());

/** Read one transaction through a typed Paddle Query. */
export class TransactionInspectionFlow implements Flow<GetTransactionInput> {
  private readonly complete = new CompleteTransactionInspection();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Paddle connection name differs');
    this.read = newGetTransactionStep<GetTransactionInput>({
      stepType: 'CallGetTransaction',
      annotations: { groupId: 'provider', groupLabel: 'Paddle', explanation: 'Read one transaction from Paddle.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompletePaddle'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'PaddleTransactionInspectionTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<GetTransactionInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the selected transaction result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [transactionAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected transaction and complete the Flow."
class CompleteTransactionInspection implements Step<QueryResult<TransactionStatus>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompletePaddle'; }
  /** Persist and return the selected transaction. */
  execute(context: Context, result: QueryResult<TransactionStatus>): StepDecision {
    transactionAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
