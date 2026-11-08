// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newGetTransactionStep, type Connection, type TransactionProfile, type GetTransactionInput,
} from '@superdurable/connector-braintree';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'braintree';
const usersAttribute = new Attribute<TransactionProfile>('braintree-transaction-read', jsonCodec<TransactionProfile>());

/** Read transaction status through a typed Braintree Query. */
export class TransactionReadFlow implements Flow<GetTransactionInput> {
  private readonly complete = new CompleteTransactionRead();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Braintree connection name differs');
    this.read = newGetTransactionStep<GetTransactionInput>({
      stepType: 'CallGetTransaction',
      annotations: { groupId: 'provider', groupLabel: 'Braintree', explanation: 'Read transaction status from Braintree.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteBraintree'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'BraintreeTransactionReadTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<GetTransactionInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the transaction status result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [usersAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the transaction status and complete the Flow."
class CompleteTransactionRead implements Step<QueryResult<TransactionProfile>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteBraintree'; }
  /** Persist and return the transaction status. */
  execute(context: Context, result: QueryResult<TransactionProfile>): StepDecision {
    usersAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
