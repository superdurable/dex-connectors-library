// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { MutationResult, QueryResult } from '@superdurable/connectors-sdk';
import { newCreateTaskStep, newDeleteTaskStep, newGetTaskStep, newListTasksStep, newUpdateTaskStep, type Connection, type CreateTaskInput, type DeleteTaskOutcome, type Task, type TaskPage } from '@superdurable/connector-doist-todoist';

/** Static Dex Web connection used by every provider Step. */
export const ConnectionName = 'todoist';
/** Input for one disposable task lifecycle. */
export interface TaskLifecycleInput extends CreateTaskInput { updateContent: string; updateDescription?: string | undefined }
/** Nonsecret result of all five provider operations. */
export interface TaskLifecycleOutcome { created: Task; read: Task; updated: Task; page: TaskPage; deletion: DeleteTaskOutcome }

/** Durable, nonsecret request retained for later Steps. */
export const lifecycleRequest = new Attribute<TaskLifecycleInput>('todoist-ts-lifecycle-request', jsonCodec<TaskLifecycleInput>());
/** Durable created task, including its immutable ID. */
export const createdTask = new Attribute<Task>('todoist-ts-created-task', jsonCodec<Task>());
/** Durable read-back of the created task. */
export const readTask = new Attribute<Task>('todoist-ts-read-task', jsonCodec<Task>());
/** Durable task after the absolute update. */
export const updatedTask = new Attribute<Task>('todoist-ts-updated-task', jsonCodec<Task>());
/** One project page returned by listTasks. */
export const listedPage = new Attribute<TaskPage>('todoist-ts-listed-page', jsonCodec<TaskPage>());

/** Create, read, update, list, and delete one disposable task. */
export class TaskLifecycleFlow implements Flow<TaskLifecycleInput> {
  private readonly prepare = new PrepareTaskLifecycle();
  private readonly recordCreation = new RecordCreation();
  private readonly recordRead = new RecordRead();
  private readonly recordUpdate = new RecordUpdate();
  private readonly recordList = new RecordList();
  private readonly complete = new CompleteTaskLifecycle();
  /** Bind one named Todoist connection at Worker registration. */
  constructor(private readonly connection: Connection) { if (!connection) throw new TypeError('Todoist connection is required'); }
  /** Stable Flow identity. */ getFlowType(): string { return 'TodoistTypeScriptTaskLifecycleFlow'; }
  /** Register five connector calls and their local transitions. */
  getSteps(): StepList<TaskLifecycleInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newCreateTaskStep({ stepType: 'CreateTodoistTask', annotations: { groupId: 'todoist-ts', groupLabel: 'Todoist Task', explanation: 'Create one disposable task with a stable request ID.' },
        connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => ({ content: input.content, description: input.description, projectId: input.projectId }), created: this.recordCreation }),
      this.recordCreation,
      newGetTaskStep({ stepType: 'ReadTodoistTask', annotations: { groupId: 'todoist-ts', groupLabel: 'Todoist Task', explanation: 'Read the task created by this Flow.' },
        connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => input, found: this.recordRead }),
      this.recordRead,
      newUpdateTaskStep({ stepType: 'UpdateTodoistTask', annotations: { groupId: 'todoist-ts', groupLabel: 'Todoist Task', explanation: 'Replace the task title and optional description.' },
        connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => input, updated: this.recordUpdate }),
      this.recordUpdate,
      newListTasksStep({ stepType: 'ListTodoistProjectTasks', annotations: { groupId: 'todoist-ts', groupLabel: 'Todoist Task', explanation: 'Read one page of the created task project.' },
        connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => input, listed: this.recordList }),
      this.recordList,
      newDeleteTaskStep({ stepType: 'DeleteTodoistTask', annotations: { groupId: 'todoist-ts', groupLabel: 'Todoist Task', explanation: 'Delete only the task created by this Flow.' },
        connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => input, deleted: this.complete }),
      this.complete,
    );
  }
  /** Register every persisted nonsecret value. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [lifecycleRequest, createdTask, readTask, updatedTask, listedPage] }; }
}

/** Validate a disposable task request. */
// dex:group group-id:todoist-ts group-label:"Todoist Task"
// dex:explanation text:"Validate a disposable task request before creation."
export class PrepareTaskLifecycle implements Step<TaskLifecycleInput> {
  /** Stable Step identity. */ getStepType(): string { return 'PrepareTodoistTaskLifecycle'; }
  /** Persist safe input and start creation. */
  execute(context: Context, input: TaskLifecycleInput): StepDecision {
    if (!input.content?.trim() || !input.updateContent?.trim()) return forceFail('content and updateContent are required');
    lifecycleRequest.set(context, input);
    return goTo(new StepRef('CreateTodoistTask'), input);
  }
}

/** Persist the created task ID before another provider call. */
// dex:group group-id:todoist-ts group-label:"Todoist Task"
// dex:explanation text:"Retain the created task ID and read it back."
export class RecordCreation implements Step<MutationResult<Task>> {
  /** Stable Step identity. */ getStepType(): string { return 'RecordTodoistCreation'; }
  /** Continue to a task read. */
  execute(context: Context, result: MutationResult<Task>): StepDecision {
    createdTask.set(context, result.value);
    return goTo(new StepRef('ReadTodoistTask'), { taskId: result.value.id });
  }
}

/** Retain the read and submit an absolute update. */
// dex:group group-id:todoist-ts group-label:"Todoist Task"
// dex:explanation text:"Retain the task read before replacing its fields."
export class RecordRead implements Step<QueryResult<Task>> {
  /** Stable Step identity. */ getStepType(): string { return 'RecordTodoistRead'; }
  /** Continue with the requested replacement. */
  execute(context: Context, result: QueryResult<Task>): StepDecision {
    readTask.set(context, result.value);
    const request = lifecycleRequest.get(context);
    return goTo(new StepRef('UpdateTodoistTask'), { taskId: result.value.id, content: request.updateContent, ...(request.updateDescription !== undefined ? { description: request.updateDescription } : {}) });
  }
}

/** Retain the updated task and list its project. */
// dex:group group-id:todoist-ts group-label:"Todoist Task"
// dex:explanation text:"Retain the update and list one page from its project."
export class RecordUpdate implements Step<MutationResult<Task>> {
  /** Stable Step identity. */ getStepType(): string { return 'RecordTodoistUpdate'; }
  /** Continue with a bounded project list. */
  execute(context: Context, result: MutationResult<Task>): StepDecision {
    updatedTask.set(context, result.value);
    return goTo(new StepRef('ListTodoistProjectTasks'), { projectId: result.value.projectId, limit: 50 });
  }
}

/** Retain a page before deleting the disposable task. */
// dex:group group-id:todoist-ts group-label:"Todoist Task"
// dex:explanation text:"Retain one project page and delete the disposable task."
export class RecordList implements Step<QueryResult<TaskPage>> {
  /** Stable Step identity. */ getStepType(): string { return 'RecordTodoistList'; }
  /** Continue with deletion by the stored immutable ID. */
  execute(context: Context, result: QueryResult<TaskPage>): StepDecision {
    listedPage.set(context, result.value);
    return goTo(new StepRef('DeleteTodoistTask'), { taskId: createdTask.get(context).id });
  }
}

/** Complete after Todoist acknowledges deletion. */
// dex:group group-id:todoist-ts group-label:"Todoist Task"
// dex:explanation text:"Return the five operation results after deletion."
export class CompleteTaskLifecycle implements Step<MutationResult<DeleteTaskOutcome>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteTodoistTaskLifecycle'; }
  /** Return only safe provider projections. */
  execute(context: Context, result: MutationResult<DeleteTaskOutcome>): StepDecision {
    return gracefulComplete({ created: createdTask.get(context), read: readTask.get(context), updated: updatedTask.get(context), page: listedPage.get(context), deletion: result.value } satisfies TaskLifecycleOutcome);
  }
}
