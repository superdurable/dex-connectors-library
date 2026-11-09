// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetCustomerStep, type Connection, type GetCustomerInput, type CustomerSummary } from '@superdurable/connector-oracle-netsuite';

/** Static Dex Web connection for the customer example. */
export const ConnectionName = 'oracle-netsuite';
const resultAttribute = new Attribute<CustomerSummary>('oracle-netsuite-typescript-customer', jsonCodec<CustomerSummary>());

/** Read one customer visible to the authorized Oracle NetSuite user. */
export class CustomerFlow implements Flow<GetCustomerInput> {
  private readonly complete = new CompleteCustomer();
  private readonly read;
  /** Bind the required static connection. */ constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Oracle NetSuite connection name differs');
    this.read = newGetCustomerStep<GetCustomerInput>({
      stepType: 'CallGetCustomer',
      annotations: { groupId: 'provider', groupLabel: 'Oracle NetSuite', explanation: 'Read the selected customer record.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, found: this.complete,
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'OracleNetSuiteCustomerInspectionTypeScript'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<GetCustomerInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the customer and complete the Flow."
class CompleteCustomer implements Step<QueryResult<CustomerSummary>> {
  readonly inputCodec = jsonCodec<QueryResult<CustomerSummary>>();
  getStepType(): string { return 'CompleteCustomer'; }
  execute(context: Context, result: QueryResult<CustomerSummary>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
