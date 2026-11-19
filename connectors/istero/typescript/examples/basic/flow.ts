// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetAccountStatusStep, type Connection, type GetAccountStatusInput, type GetAccountStatusOutput } from '@superdurable/connector-istero';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'istero';
const resultAttribute = new Attribute<GetAccountStatusOutput>('istero-result', jsonCodec<GetAccountStatusOutput>());

/** Query IsTero through a typed connector Step. */
export class IsTeroInspectionFlow implements Flow<GetAccountStatusInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('IsTero connection name differs');
    this.read = newGetAccountStatusStep<GetAccountStatusInput>({
      stepType: 'CallGetAccountStatus',
      annotations: { groupId: 'provider', groupLabel: 'IsTero', explanation: 'Read provider data through IsTero.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      retrieved: new StepRef('CompleteIsTero'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'IsTeroInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<GetAccountStatusInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<GetAccountStatusOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteIsTero'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<GetAccountStatusOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
