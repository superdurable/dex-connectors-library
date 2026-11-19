// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListCustomerFieldsStep, type Connection, type ListCustomerFieldsInput, type CustomerFields } from '@superdurable/connector-okki';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'okki';
const resultAttribute = new Attribute<CustomerFields>('okki-result', jsonCodec<CustomerFields>());

/** Query Okki through a typed connector Step. */
export class OkkiInspectionFlow implements Flow<ListCustomerFieldsInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Okki connection name differs');
    this.read = newListCustomerFieldsStep<ListCustomerFieldsInput>({
      stepType: 'CallListCustomerFields',
      annotations: { groupId: 'provider', groupLabel: 'Okki', explanation: 'Read provider data through Okki.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompleteOkki'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'OkkiInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<ListCustomerFieldsInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<CustomerFields>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteOkki'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<CustomerFields>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
