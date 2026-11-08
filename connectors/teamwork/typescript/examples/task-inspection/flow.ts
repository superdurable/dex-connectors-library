// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newGetTaskStep, type Connection, type TaskSummary, type GetTaskInput,
} from '@superdurable/connector-teamwork';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'teamwork';
const taskAttribute = new Attribute<TaskSummary>('teamwork-task', jsonCodec<TaskSummary>());

/** Read one task through a typed Teamwork Query. */
export class TaskInspectionFlow implements Flow<GetTaskInput> {
  private readonly complete = new CompleteTaskInspection();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Teamwork connection name differs');
    this.read = newGetTaskStep<GetTaskInput>({
      stepType: 'CallGetTask',
      annotations: { groupId: 'provider', groupLabel: 'Teamwork', explanation: 'Read one task from Teamwork.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteTeamwork'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'TeamworkTaskInspectionTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<GetTaskInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the selected task result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [taskAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected task and complete the Flow."
class CompleteTaskInspection implements Step<QueryResult<TaskSummary>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteTeamwork'; }
  /** Persist and return the selected task. */
  execute(context: Context, result: QueryResult<TaskSummary>): StepDecision {
    taskAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
