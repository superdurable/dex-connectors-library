// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListAccessibleCustomersStep, type Connection, type ListAccessibleCustomersInput, type AccessibleCustomers } from '@superdurable/connector-google-ads';

/** Static Dex Web connection for the Customers example. */
export const ConnectionName = 'google-ads';
const resultAttribute = new Attribute<AccessibleCustomers>('google-ads-typescript-page', jsonCodec<AccessibleCustomers>());

/** List customers visible to the authorized Google identity. */
export class CustomersFlow implements Flow<ListAccessibleCustomersInput> {
  private readonly complete = new CompleteCustomers();
  private readonly read;
  /** Bind the required static connection. */ constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Customers connection name differs');
    this.read = newListAccessibleCustomersStep<ListAccessibleCustomersInput>({
      stepType: 'CallListAccessibleCustomers',
      annotations: { groupId: 'provider', groupLabel: 'Google Ads', explanation: 'List customers directly accessible to the authorized Google identity.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, listed: this.complete,
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'AdsCustomersListTypeScript'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<ListAccessibleCustomersInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the customers and complete the Flow."
class CompleteCustomers implements Step<QueryResult<AccessibleCustomers>> {
  readonly inputCodec = jsonCodec<QueryResult<AccessibleCustomers>>();
  getStepType(): string { return 'CompleteCustomers'; }
  execute(context: Context, result: QueryResult<AccessibleCustomers>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
