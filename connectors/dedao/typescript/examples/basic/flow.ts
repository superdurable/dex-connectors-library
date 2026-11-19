// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetNoteTaskStatusStep, type Connection, type GetNoteTaskStatusInput, type GetNoteTaskStatusOutput } from '@superdurable/connector-dedao';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'dedao';
const resultAttribute = new Attribute<GetNoteTaskStatusOutput>('dedao-result', jsonCodec<GetNoteTaskStatusOutput>());

/** Query Dedao through a typed connector Step. */
export class DedaoInspectionFlow implements Flow<GetNoteTaskStatusInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Dedao connection name differs');
    this.read = newGetNoteTaskStatusStep<GetNoteTaskStatusInput>({
      stepType: 'CallGetNoteTaskStatus',
      annotations: { groupId: 'provider', groupLabel: 'Dedao', explanation: 'Read provider data through Dedao.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteDedao'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'DedaoInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<GetNoteTaskStatusInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<GetNoteTaskStatusOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteDedao'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<GetNoteTaskStatusOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
