// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetTaskStep, type Connection, type GetTaskInput, type Task } from '@superdurable/connector-clickup';

/** Static Dex Web connection name used by the Worker and starter. */
export const ConnectionName = 'clickup-workspace';
/** Nonsecret task request retained for Flow inspection. */
export const taskRequest = new Attribute<GetTaskInput>('clickup-task-request', jsonCodec<GetTaskInput>());
/** Safe task result retained for Flow inspection. */
export const taskResult = new Attribute<Task>('clickup-task-result', jsonCodec<Task>());

/** One ClickUp task read through a named connection. */
export class GetTaskFlow implements Flow<GetTaskInput> {
  private readonly prepare = new PrepareGetTask();
  private readonly complete = new CompleteGetTask();

  /** Bind the named connection at Worker registration. */
  constructor(private readonly connection: Connection) { if (!connection) throw new TypeError('ClickUp connection is required'); }
  /** Stable Flow type. */ getFlowType(): string { return 'ClickUpGetTaskFlow'; }
  /** Register each task read Step. */
  getSteps(): StepList<GetTaskInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newGetTaskStep({ stepType: 'ReadClickUpTask', annotations: { groupId: 'clickup-task', groupLabel: 'ClickUp Task', explanation: 'Read a task with its custom fields.' },
        connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => input, found: this.complete }),
      this.complete,
    );
  }
  /** Register each persisted Attribute. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [taskRequest, taskResult] }; }
}

/** Validate and retain the task request. */
// dex:group group-id:clickup-task group-label:"ClickUp Task"
// dex:explanation text:"Validate the task identifier before reading ClickUp."
export class PrepareGetTask implements Step<GetTaskInput> {
  /** Stable Step type. */ getStepType(): string { return 'PrepareClickUpGetTask'; }
  /** Start the provider Step. */
  execute(context: Context, input: GetTaskInput): StepDecision {
    if (!/^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$/.test(input.taskId)) return forceFail('ClickUp taskId is required');
    taskRequest.set(context, input);
    return goTo(new StepRef('ReadClickUpTask'), input);
  }
}

/** Persist the returned task and finish. */
// dex:group group-id:clickup-task group-label:"ClickUp Task"
// dex:explanation text:"Persist the returned task and complete the Flow."
export class CompleteGetTask implements Step<QueryResult<Task>> {
  /** Stable Step type. */ getStepType(): string { return 'CompleteClickUpGetTask'; }
  /** Complete with the provider task. */
  execute(context: Context, result: QueryResult<Task>): StepDecision {
    taskResult.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
