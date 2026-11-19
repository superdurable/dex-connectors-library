// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListDepartmentsStep, type Connection, type ListDepartmentsInput, type ListDepartmentsOutput } from '../../src/index.js';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'qince';
const resultAttribute = new Attribute<ListDepartmentsOutput>('qince-result', jsonCodec<ListDepartmentsOutput>());

/** Query Qince through a typed connector Step. */
export class QinceInspectionFlow implements Flow<ListDepartmentsInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Qince connection name differs');
    this.read = newListDepartmentsStep<ListDepartmentsInput>({
      stepType: 'CallListDepartments',
      annotations: { groupId: 'provider', groupLabel: 'Qince', explanation: 'Read provider data through Qince.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompleteQince'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'QinceInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<ListDepartmentsInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<ListDepartmentsOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteQince'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<ListDepartmentsOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
