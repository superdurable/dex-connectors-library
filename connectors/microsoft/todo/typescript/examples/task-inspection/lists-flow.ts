// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newListTaskListsStep, type Connection, type ListTaskListsInput, type TaskListsPage,
} from '@superdurable/connector-microsoft-todo';

/** Static Dex Web connection shared by both example Flows. */
export const ConnectionName = 'microsoft-todo';
const listsAttribute = new Attribute<TaskListsPage>('microsoft-todo-lists', jsonCodec<TaskListsPage>());

/** Read all bounded To Do task lists through a typed Query. */
export class TaskListsInspectionFlow implements Flow<ListTaskListsInput> {
  private readonly complete = new CompleteLists();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Microsoft To Do connection name differs');
    this.read = newListTaskListsStep<ListTaskListsInput>({
      stepType: 'CallListTaskLists',
      annotations: { groupId: 'provider', groupLabel: 'Microsoft To Do', explanation: 'Read task lists from Microsoft To Do.' },
      connection, connectionName: ConnectionName, mapToOperationInput: input => input,
      listed: new StepRef('CompleteListTaskLists'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'MicrosoftTodoListsInspectionTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<ListTaskListsInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected list collection. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [listsAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the task lists and complete the Flow."
class CompleteLists implements Step<QueryResult<TaskListsPage>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteListTaskLists'; }
  /** Persist and return the task lists. */
  execute(context: Context, result: QueryResult<TaskListsPage>): StepDecision {
    listsAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
