// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetTaskStatusStep, type Connection, type GetTaskStatusInput, type GetTaskStatusOutput } from '../../src/index.js';

/** Static Dex Web connection for the Shengshu example. */
export const ConnectionName = 'shengshu';
const tasksResult = new Attribute<GetTaskStatusOutput>('shengshu-model-list-result', jsonCodec<GetTaskStatusOutput>());

/** Read the task status through Shengshu. */
export class TaskStatusFlow implements Flow<GetTaskStatusInput> {
  private readonly read;
  private readonly complete = new CompleteTaskStatus();
  /** Bind the static project connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Shengshu connection name differs');
    this.read = newGetTaskStatusStep<GetTaskStatusInput>({
      stepType: 'CallShengshu',
      annotations: { groupId: 'shengshu', groupLabel: 'Shengshu', explanation: 'Read the task status with an API Key.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, found: new StepRef('CompleteShengshuTasks'),
    });
  }
  /** Preserve the Flow identity. */ getFlowType(): string { return 'ShengshuTaskStatus'; }
  /** Register the provider and terminal Steps. */
  getSteps(): StepList<GetTaskStatusInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Register the durable task status. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [tasksResult] }; }
}

// dex:group group-id:shengshu group-label:"Shengshu"
// dex:explanation text:"Persist the task status and complete the Flow."
class CompleteTaskStatus implements Step<QueryResult<GetTaskStatusOutput>> {
  getStepType(): string { return 'CompleteShengshuTasks'; }
  execute(context: Context, result: QueryResult<GetTaskStatusOutput>): StepDecision { tasksResult.set(context, result.value); return gracefulComplete(result.value); }
}
