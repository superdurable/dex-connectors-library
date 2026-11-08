// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newListTasksStep, type Connection, type ListTasksInput, type TasksPage,
} from '@superdurable/connector-microsoft-todo';

/** Static Dex Web connection shared by both example Flows. */
export const ConnectionName = 'microsoft-todo';
const tasksAttribute = new Attribute<TasksPage>('microsoft-todo-tasks', jsonCodec<TasksPage>());

/** Read all bounded tasks in one To Do list through a typed Query. */
export class TasksInspectionFlow implements Flow<ListTasksInput> {
  private readonly complete = new CompleteTasks();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Microsoft To Do connection name differs');
    this.read = newListTasksStep<ListTasksInput>({
      stepType: 'CallListTasks',
      annotations: { groupId: 'provider', groupLabel: 'Microsoft To Do', explanation: 'Read tasks in the selected To Do list.' },
      connection, connectionName: ConnectionName, mapToOperationInput: input => input,
      listed: new StepRef('CompleteListTasks'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'MicrosoftTodoTasksInspectionTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<ListTasksInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected task collection. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [tasksAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the tasks and complete the Flow."
class CompleteTasks implements Step<QueryResult<TasksPage>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteListTasks'; }
  /** Persist and return the tasks. */
  execute(context: Context, result: QueryResult<TasksPage>): StepDecision {
    tasksAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
