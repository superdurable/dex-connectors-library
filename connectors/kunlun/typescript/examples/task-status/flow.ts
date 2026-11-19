// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetTaskStatusStep, type Connection, type GetTaskStatusOutput, type GetTaskStatusInput } from '../../src/index.js';

/** Static Dex Web connection for the Mureka example. */
export const ConnectionName = 'kunlun';
const taskResult = new Attribute<GetTaskStatusOutput>('kunlun-task-status-result', jsonCodec<GetTaskStatusOutput>());

/** Read one existing song task through Mureka. */
export class TaskStatusFlow implements Flow<GetTaskStatusInput> {
  private readonly start = new StartTask();
  private readonly complete = new CompleteTask();
  /** Bind the static project connection. */
  constructor(private readonly connection: Connection) { if (connection.reference.name !== ConnectionName) throw new TypeError('Mureka connection name differs'); }
  /** Preserve the Flow identity. */ getFlowType(): string { return 'MurekaTaskStatus'; }
  /** Register the provider and terminal Steps. */
  getSteps(): StepList<GetTaskStatusInput> {
    return StepList.startStep(this.start).otherSteps(newGetTaskStatusStep({
      stepType: 'CallMurekaTask',
      annotations: { groupId: 'kunlun', groupLabel: 'Mureka', explanation: 'Read the current state of one existing song task.' },
      connection: this.connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, found: new StepRef('CompleteMurekaTask'),
    }), this.complete);
  }
  /** Register the durable task observation. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [taskResult] }; }
}

// dex:group group-id:kunlun group-label:"Mureka"
// dex:explanation text:"Start the task-status query."
class StartTask implements Step<GetTaskStatusInput> {
  getStepType(): string { return 'StartMurekaTask'; }
  execute(_context: Context, input: GetTaskStatusInput): StepDecision { return goTo(new StepRef('CallMurekaTask'), input); }
}

// dex:group group-id:kunlun group-label:"Mureka"
// dex:explanation text:"Persist the observation and complete the Flow."
class CompleteTask implements Step<QueryResult<GetTaskStatusOutput>> {
  getStepType(): string { return 'CompleteMurekaTask'; }
  execute(context: Context, result: QueryResult<GetTaskStatusOutput>): StepDecision { taskResult.set(context, result.value); return gracefulComplete(result.value); }
}
