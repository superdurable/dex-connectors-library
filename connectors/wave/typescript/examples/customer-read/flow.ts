// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newGetCustomerStep, type Connection, type CustomerProfile, type GetCustomerInput,
} from '@superdurable/connector-wave';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'wave';
const usersAttribute = new Attribute<CustomerProfile>('wave-customer-read', jsonCodec<CustomerProfile>());

/** Read customer profile through a typed Wave Query. */
export class CustomerReadFlow implements Flow<GetCustomerInput> {
  private readonly complete = new CompleteCustomerRead();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Wave connection name differs');
    this.read = newGetCustomerStep<GetCustomerInput>({
      stepType: 'CallGetCustomer',
      annotations: { groupId: 'provider', groupLabel: 'Wave', explanation: 'Read customer profile from Wave.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteWave'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'WaveCustomerReadTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<GetCustomerInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the customer profile result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [usersAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the customer profile and complete the Flow."
class CompleteCustomerRead implements Step<QueryResult<CustomerProfile>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteWave'; }
  /** Persist and return the customer profile. */
  execute(context: Context, result: QueryResult<CustomerProfile>): StepDecision {
    usersAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
