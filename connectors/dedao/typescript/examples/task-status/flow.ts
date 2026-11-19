// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetNoteTaskStatusStep, type Connection, type GetNoteTaskStatusInput, type GetNoteTaskStatusOutput } from '@superdurable/connector-dedao';

/** Static Dex Web connection used by the example. */ export const ConnectionName = 'dedao';
/** Persisted note task status. */ export const taskStatusAttribute = new Attribute<GetNoteTaskStatusOutput>('dedao-task-status', jsonCodec<GetNoteTaskStatusOutput>());

/** Read an existing Get Note task status. */
export class DedaoTaskStatusFlow implements Flow<GetNoteTaskStatusInput> {
  private readonly complete = new CompleteTaskStatus();
  private readonly read;
  /** Bind one static Dedao connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Dedao connection name differs');
    this.read = newGetNoteTaskStatusStep<GetNoteTaskStatusInput>({ stepType: 'CallGetNoteTaskStatus',
      annotations: { groupId: 'provider', groupLabel: 'Dedao', explanation: 'Read task status and resulting note ID.' },
      connection, connectionName: ConnectionName, mapToOperationInput: input => input,
      found: new StepRef('CompleteDedaoTaskStatus'),
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'DedaoTaskStatusTypeScript'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<GetNoteTaskStatusInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Register the persisted task status. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [taskStatusAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the task status and complete the Flow."
class CompleteTaskStatus implements Step<QueryResult<GetNoteTaskStatusOutput>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteDedaoTaskStatus'; }
  /** Persist the provider result without credentials. */
  execute(context: Context, result: QueryResult<GetNoteTaskStatusOutput>): StepDecision { taskStatusAttribute.set(context, result.value); return gracefulComplete(result.value); }
}
