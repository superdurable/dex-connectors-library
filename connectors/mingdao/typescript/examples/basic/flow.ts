// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListWorksheetsStep, type Connection, type ListWorksheetsInput, type ListWorksheetsOutput } from '../../src/index.js';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'mingdao';
const resultAttribute = new Attribute<ListWorksheetsOutput>('mingdao-result', jsonCodec<ListWorksheetsOutput>());

/** Query Mingdao through a typed connector Step. */
export class MingdaoInspectionFlow implements Flow<ListWorksheetsInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Mingdao connection name differs');
    this.read = newListWorksheetsStep<ListWorksheetsInput>({
      stepType: 'CallListWorksheets',
      annotations: { groupId: 'provider', groupLabel: 'Mingdao', explanation: 'Read provider data through Mingdao.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompleteMingdao'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'MingdaoInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<ListWorksheetsInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<ListWorksheetsOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteMingdao'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<ListWorksheetsOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
