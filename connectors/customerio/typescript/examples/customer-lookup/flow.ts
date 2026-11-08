// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newGetCustomersByEmailStep, type Connection, type CustomerLookupPage, type GetCustomersByEmailInput,
} from '@superdurable/connector-customerio';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'customerio';
const usersAttribute = new Attribute<CustomerLookupPage>('customerio-customer-lookup', jsonCodec<CustomerLookupPage>());

/** Read customer IDs through a typed Customer.io Query. */
export class CustomerLookupFlow implements Flow<GetCustomersByEmailInput> {
  private readonly complete = new CompleteCustomerLookup();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Customerio connection name differs');
    this.read = newGetCustomersByEmailStep<GetCustomersByEmailInput>({
      stepType: 'CallGetCustomersByEmail',
      annotations: { groupId: 'provider', groupLabel: 'Customer.io', explanation: 'Read customer IDs from Customer.io.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteCustomerio'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'CustomerioCustomerLookupTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<GetCustomersByEmailInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the customer lookup results result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [usersAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the customer lookup results and complete the Flow."
class CompleteCustomerLookup implements Step<QueryResult<CustomerLookupPage>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteCustomerio'; }
  /** Persist and return the customer lookup results. */
  execute(context: Context, result: QueryResult<CustomerLookupPage>): StepDecision {
    usersAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
